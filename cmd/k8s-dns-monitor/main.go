/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/bwagner5/k8s-dns-monitor/pkg/monitor"
	"github.com/imdario/mergo"
	"github.com/jaypipes/envutil"
	"github.com/olekukonko/tablewriter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	OutputYAML       = "yaml"
	OutputTableShort = "short"
	OutputTableWide  = "wide"
)

var (
	version = ""
	// handle graceful shutdown
	exit = make(chan os.Signal, 1)
)

type GlobalOptions struct {
	Verbose    bool
	Version    bool
	Output     string
	ConfigFile string
	Kubeconfig string
}

type RootOptions struct {
	Attribution     bool
	MetricsPort     int
	MetricsPrefix   string
	ConcurrentTests int
}

var (
	globalOpts = GlobalOptions{}
	rootOpts   = RootOptions{}
	rootCmd    = &cobra.Command{
		Use:     "k8s-dns-monitor",
		Version: version,
		Run: func(cmd *cobra.Command, args []string) {
			signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
			if rootOpts.Attribution {
				fmt.Println(attribution)
				os.Exit(0)
			}
			cfg, err := config.LoadDefaultConfig(cmd.Context())
			if err != nil {
				log.Fatalf("unable to load AWS SDK config, %s", err)
			}
			cwAPI := cloudwatch.NewFromConfig(cfg)
			registry := prometheus.NewRegistry()
			lo.Must0(monitor.RegisterMetrics(registry, rootOpts.MetricsPrefix))
			MustStartPromMetrics(registry, rootOpts.MetricsPort)
			dnsTest := monitor.DNSTest{
				CW:              cwAPI,
				Clientset:       MustGetClientset(),
				MetricsRegistry: registry,
				MetricsPrefix:   rootOpts.MetricsPrefix,
			}
			jobs := make(chan struct{}, rootOpts.ConcurrentTests)
			for {
				go func() {
					if err := dnsTest.Run(cmd.Context()); err != nil {
						log.Printf("Test FAIL: %s", err)
					} else {
						log.Println("Test SUCCEEDED")
					}
					<-jobs
				}()
				jobs <- struct{}{}
				time.Sleep(10 * time.Millisecond)
				if isShuttingDown() {
					time.Sleep(15 * time.Second)
					log.Println("Shutting down!")
					return
				}
			}
		},
	}
)

//go:generate cp -r ../../ATTRIBUTION.md ./
//go:embed ATTRIBUTION.md
var attribution string

func main() {
	rootCmd.Flags().BoolVar(&rootOpts.Attribution, "attribution", false, "show attributions")
	rootCmd.Flags().IntVar(&rootOpts.ConcurrentTests, "concurrent-tests", envutil.WithDefaultInt("CONCURRENT_TESTS", 10), "number of test services to perform concurrently")
	rootCmd.Flags().StringVar(&rootOpts.MetricsPrefix, "metrics-prefix", envutil.WithDefault("METRICS_PREFIX", ""), "metrics name prefix")
	rootCmd.Flags().IntVar(&rootOpts.MetricsPort, "metrics-port", envutil.WithDefaultInt("METRICS_PORT", 8000), "port to expose prometheus /metrics")
	rootCmd.PersistentFlags().BoolVar(&globalOpts.Verbose, "verbose", false, "Verbose output")
	rootCmd.PersistentFlags().BoolVar(&globalOpts.Version, "version", false, "version")
	rootCmd.PersistentFlags().StringVarP(&globalOpts.Output, "output", "o", OutputTableShort,
		fmt.Sprintf("Output mode: %v", []string{OutputTableShort, OutputTableWide, OutputYAML}))
	rootCmd.PersistentFlags().StringVarP(&globalOpts.ConfigFile, "file", "f", "", "YAML Config File")

	rootCmd.AddCommand(&cobra.Command{Use: "completion", Hidden: true})
	cobra.EnableCommandSorting = false

	lo.Must0(rootCmd.Execute())
}

func ParseConfig[T any](globalOpts GlobalOptions, opts T) (T, error) {
	if globalOpts.ConfigFile == "" {
		return opts, nil
	}
	configBytes, err := os.ReadFile(globalOpts.ConfigFile)
	if err != nil {
		return opts, err
	}
	var parsedCreateOpts T
	if err := yaml.Unmarshal(configBytes, &parsedCreateOpts); err != nil {
		return opts, err
	}
	if err := mergo.Merge(&opts, parsedCreateOpts, mergo.WithOverride); err != nil {
		return opts, err
	}
	return opts, nil
}

func PrettyEncode(data any) string {
	var buffer bytes.Buffer
	enc := json.NewEncoder(&buffer)
	enc.SetIndent("", "    ")
	if err := enc.Encode(data); err != nil {
		panic(err)
	}
	return buffer.String()
}

func PrettyTable[T any](data []T, wide bool) string {
	var headers []string
	var rows [][]string
	for _, dataRow := range data {
		var row []string
		// clear headers each time so we only keep one set
		headers = []string{}
		reflectStruct := reflect.Indirect(reflect.ValueOf(dataRow))
		for i := 0; i < reflectStruct.NumField(); i++ {
			typeField := reflectStruct.Type().Field(i)
			tag := typeField.Tag.Get("table")
			if tag == "" {
				continue
			}
			subtags := strings.Split(tag, ",")
			if len(subtags) > 1 && subtags[1] == "wide" && !wide {
				continue
			}
			headers = append(headers, subtags[0])
			row = append(row, reflect.ValueOf(dataRow).Field(i).String())
		}
		rows = append(rows, row)
	}
	out := bytes.Buffer{}
	table := tablewriter.NewWriter(&out)
	table.SetHeader(headers)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetTablePadding("\t") // pad with tabs
	table.SetNoWhiteSpace(true)
	table.AppendBulk(rows) // Add Bulk Data
	table.Render()
	return out.String()
}

func MustGetClientset() *kubernetes.Clientset {
	// Setup K8s clientset
	var k8sConfig *rest.Config
	var err error
	if globalOpts.Kubeconfig != "" {
		k8sConfig, err = clientcmd.BuildConfigFromFlags("", globalOpts.Kubeconfig)
		if err != nil {
			log.Fatalf("Unable to create K8s clientset from kubeconfig: %s", err)
		}
	} else {
		k8sConfig, err = rest.InClusterConfig()
		if err != nil {
			log.Fatalf("Unable to find in-cluster K8s config: %s\n", err)
		}
	}
	k8sConfig.QPS = 1_000
	k8sConfig.Burst = 10_000
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		log.Fatalf("Unable to create K8s clientset: %s", err)
	}
	return clientset
}

// Chaos does a bunch of unique dns queries to clear the cache
func Chaos(region string, concurrency int) {
	log.Printf("Starting chaos queries to disrupt the cache in %s with a concurrency of %d", region, concurrency)
	for i := 0; i < concurrency; i++ {
		go func(prefix int) {
			for {
				net.LookupIP(fmt.Sprintf("%d-%d.%s.compute.internal", prefix, rand.Int63(), region))
			}
		}(i)
		log.Printf("Chaos %d started", i)
	}
	log.Printf("Chaos queries initialized")
}

func MustStartPromMetrics(registry *prometheus.Registry, port int) {
	http.Handle("/metrics", promhttp.HandlerFor(
		registry,
		promhttp.HandlerOpts{EnableOpenMetrics: false},
	))
	srv := &http.Server{
		ReadTimeout:       1 * time.Second,
		WriteTimeout:      1 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		Addr:              fmt.Sprintf(":%d", port),
	}
	log.Printf("Serving prometheus metrics at http://:%d/metrics", port)
	go func() {
		lo.Must0(srv.ListenAndServe())
	}()
}

func isShuttingDown() bool {
	for {
		select {
		case <-exit:
			return true
		default:
			return false
		}
	}
}
