package monitor

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

var (
	dnsPropagationLatencyMetricName = "dns_propagation_latency_seconds"
	dnsPropagationLatency           prometheus.Histogram
	loopBackIP                      = net.ParseIP("127.0.0.1")
)

func RegisterMetrics(registry prometheus.Registerer, metricPrefix string) error {
	dnsPropagationLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: getDNSPropagationLatencyMetricName(metricPrefix),
		Help: "Latency of DNS Propagation from Service Creation to Queryable (seconds)",
		Buckets: []float64{
			0.05, 0.10, 0.15, 0.20, 0.25, 0.50, 0.75,
			1, 1.25, 1.50, 1.75,
			2, 2.25, 2.50, 2.75,
			3, 3.25, 3.50, 3.75,
			4, 4.25, 4.50, 4.75,
			5, 5.25, 5.50, 5.75,
			6, 6.25, 6.50, 6.75,
			7, 7.25, 7.50, 7.75,
			8, 8.25, 8.50, 8.75,
			9, 9.25, 9.50, 9.75,
			10, 10.25, 10.50, 10.75,
			11, 11.25, 11.50, 11.75,
			12, 13, 14, 15, 16, 17,
			18, 19, 20, 21, 22, 23,
			24, 25, 26, 27, 28, 29,
		},
	})
	for _, c := range []prometheus.Collector{dnsPropagationLatency} {
		if err := registry.Register(c); err != nil {
			return err
		}
	}
	return nil
}

func getDNSPropagationLatencyMetricName(metricPrefix string) string {
	if metricPrefix != "" && !strings.HasSuffix(metricPrefix, "_") {
		metricPrefix = fmt.Sprintf("%s_", metricPrefix)
	}
	return fmt.Sprintf("%s%s", metricPrefix, dnsPropagationLatencyMetricName)
}

type DNSTest struct {
	CW              *cloudwatch.Client
	Clientset       *kubernetes.Clientset
	MetricsRegistry *prometheus.Registry
	MetricsPrefix   string
}

// 1. Use Inflater to launch a service
// 2. Send DNS queries for the service
// 3. When the queries succeed, emit a metric else timeout after 30 sec
// 4. Delete service
// 5. Send DNS Queries until NXDOMAIN
// 6. Emit a metric

func (d DNSTest) Run(ctx context.Context) error {
	testID := fmt.Sprintf("k8s-dns-monitor-inflate-%d", rand.Int())
	namespace := "default"
	mockService, err := d.Clientset.CoreV1().Services(namespace).Create(ctx, getService(testID, namespace), metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("creating a mock service: %v", err)
		return err
	}
	defer func() { d.Clientset.CoreV1().Services(namespace).Delete(ctx, mockService.Name, metav1.DeleteOptions{}) }()
	// inflate := inflater.New(d.Clientset)
	// inflateCollection, err := inflate.Inflate(ctx, inflater.Options{
	// 	RandomSuffix: true,
	// 	Service:      true,
	// 	Image:        "602401143452.dkr.ecr.sa-east-1.amazonaws.com/eks/pause:3.5",
	// })
	// if err != nil {
	// 	return err
	// }
	// defer inflate.Delete(ctx, inflater.DeleteFilters{
	// 	Namespace: inflateCollection.Deployment.Namespace,
	// 	Name:      inflateCollection.Deployment.Name,
	// })

	if err := testUntil(300*time.Second, func() error {
		dnsQuery := fmt.Sprintf("%s.%s.svc.cluster.local", mockService.Name, mockService.Namespace)
		ips, err := net.LookupIP(dnsQuery)
		if err != nil {
			return err
		} else if len(ips) == 0 {
			emptyErr := fmt.Errorf("returned empty list of IPs")
			return emptyErr
		} else if len(ips) == 1 && ips[0].Equal(loopBackIP) {
			wildErr := fmt.Errorf("returned wildcard IP")
			return wildErr
		}
		latency := time.Since(mockService.CreationTimestamp.Time).Milliseconds()
		log.Printf("Successfully Received IPs for %s: %v in %dms", dnsQuery, lo.Map(ips, func(ip net.IP, _ int) string { return ip.String() }), latency)
		return nil
	}); err != nil {
		if _, ok := lo.ErrorsAs[*net.DNSError](err); ok {
			if err := d.emitMetricPropagationDelayMetric(ctx, time.Since(mockService.CreationTimestamp.Time)); err != nil {
				log.Fatalf("Unable to emit propagation delay metric to CloudWatch on a failure: %s", err)
			}
		}
		return err
	}

	// Emit propagation metric
	if err := d.emitMetricPropagationDelayMetric(ctx, time.Since(mockService.CreationTimestamp.Time)); err != nil {
		log.Fatalf("Unable to emit propagation delay metric to CloudWatch: %s", err)
	}

	return nil
}

func (d DNSTest) emitMetricPropagationDelayMetric(ctx context.Context, latency time.Duration) error {
	// Prometheus Metric
	dnsPropagationLatency.Observe(float64(latency.Seconds()))
	// CloudWatch Metric
	_, err := d.CW.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace: aws.String("K8s-DNS-Monitor"),
		MetricData: []types.MetricDatum{
			{
				MetricName: aws.String(getDNSPropagationLatencyMetricName(d.MetricsPrefix)),
				Value:      aws.Float64(float64(latency.Seconds())),
				Unit:       types.StandardUnitSeconds,
			},
		},
	})
	return err
}

func testUntil(timeout time.Duration, testFN func() error) error {
	startTime := time.Now().UTC()
	for {
		err := testFN()
		if err == nil {
			return nil
		}
		if time.Since(startTime) >= timeout {
			return err
		}
		time.Sleep(time.Millisecond * 100)
	}
}

func getService(name string, namespace string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":        name,
				"managed-by": "k8s-dns-monitor",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": name,
			},
			Ports: []corev1.ServicePort{
				{
					Port: 8080,
				},
			},
		},
	}
}
