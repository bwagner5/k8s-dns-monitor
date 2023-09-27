package monitor

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/bwagner5/inflate/pkg/inflater"
	"github.com/samber/lo"
	"k8s.io/client-go/kubernetes"
)

// 1. Use Inflater to launch a service
// 2. Send DNS queries for the service
// 3. When the queries succeed, emit a metric else timeout after 30 sec
// 4. Delete service
// 5. Send DNS Queries until NXDOMAIN
// 6. Emit a metric

func DNSTest(ctx context.Context, clientset *kubernetes.Clientset, cwAPI *cloudwatch.Client) error {
	inflate := inflater.New(clientset)
	inflateCollection, err := inflate.Inflate(ctx, inflater.Options{
		RandomSuffix: true,
		Service:      true,
		Image:        "public.ecr.aws/eks-distro/kubernetes/pause:3.2",
	})
	if err != nil {
		return err
	}
	defer inflate.Delete(ctx, inflater.DeleteFilters{
		Namespace: inflateCollection.Deployment.Namespace,
		Name:      inflateCollection.Deployment.Name,
	})
	if err := testUntil(60*time.Second, func() error {
		ips, err := net.LookupIP(fmt.Sprintf("%s.%s.svc.cluster.local.", inflateCollection.Service.Name, inflateCollection.Service.Namespace))
		if dnsErr, ok := lo.ErrorsAs[*net.DNSError](err); ok {
			log.Printf("DNS Lookup Error (%s/%s): %v", inflateCollection.Deployment.Namespace, inflateCollection.Deployment.Name, dnsErr.Err)
		}
		if err != nil {
			return err
		} else if len(ips) == 0 {
			emptyErr := fmt.Errorf("returned empty list of IPs")
			log.Printf("DNS Lookup Error (%s/%s): ", inflateCollection.Deployment.Namespace, inflateCollection.Deployment.Name)
			return emptyErr
		}
		latency := time.Since(inflateCollection.Service.CreationTimestamp.Time).Milliseconds()
		log.Printf("Successfully Received IPs: %v in %dms", lo.Map(ips, func(ip net.IP, _ int) string { return ip.String() }), latency)
		return nil
	}); err != nil {
		return err
	}

	// Emit propagation metric
	if _, err := cwAPI.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace: aws.String("K8s-DNS-Monitor"),
		MetricData: []types.MetricDatum{
			{
				MetricName: aws.String("propagation-delay"),
				Value:      aws.Float64(float64(time.Since(inflateCollection.Service.CreationTimestamp.Time).Milliseconds())),
				Unit:       types.StandardUnitMilliseconds,
			},
		},
	}); err != nil {
		log.Fatalf("Unable to emit propagation delay metric to CloudWatch: %s", err)
	}
	return nil
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
		time.Sleep(time.Millisecond * 5)
	}
}
