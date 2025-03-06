package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Result struct {
	StatusCode int
	Duration   time.Duration
	Error      error
}

type ReportExporter interface {
	Export(Report) string
}

type JSONExporter struct{}
type CSVExporter struct{}

func (j JSONExporter) Export(r Report) string {
	data, _ := json.MarshalIndent(r, "", "  ")
	return string(data)
}

func (c CSVExporter) Export(r Report) string {
	var sb strings.Builder

	// Cabeçalho
	sb.WriteString("Total Time (s),Total Requests,RPS,Min Duration (ms),Max Duration (ms),Avg Duration (ms),Errors\n")

	// Dados principais
	sb.WriteString(fmt.Sprintf("%.2f,%d,%.2f,%.2f,%.2f,%.2f,%d\n",
		r.TotalTime.Seconds(),
		r.TotalRequests,
		r.RPS,
		float64(r.MinDuration.Milliseconds()),
		float64(r.MaxDuration.Milliseconds()),
		float64(r.AvgDuration.Milliseconds()),
		r.Errors))

	// Status Codes
	sb.WriteString("\nStatus Code Distribution\n")
	sb.WriteString("Code,Count,Percentage\n")
	for code, count := range r.StatusCodes {
		percentage := float64(count) / float64(r.TotalRequests) * 100
		sb.WriteString(fmt.Sprintf("%d,%d,%.2f\n", code, count, percentage))
	}

	return sb.String()
}

type Config struct {
	URL         string
	Requests    int
	Concurrency int
	Timeout     time.Duration
	Method      string
	Headers     map[string]string
	Body        string
	Format      string // "plain", "json", "csv"
}

type Report struct {
	TotalTime     time.Duration
	TotalRequests int
	StatusCodes   map[int]int
	Errors        int
	Durations     []time.Duration
	MinDuration   time.Duration
	MaxDuration   time.Duration
	AvgDuration   time.Duration
	RPS           float64
	StdDeviation  time.Duration
	ErrorDetails  map[string]ErrorDetail
}

type ErrorDetail struct {
	Count   int
	Message string
}

func main() {
	urlFlag := flag.String("url", "", "URL to test")
	requestsFlag := flag.Int("requests", 0, "Number of requests to make")
	concurrencyFlag := flag.Int("concurrency", 1, "Number of concurrent requests")
	timeoutFlag := flag.Duration("timeout", 10*time.Second, "Timeout for each request")
	methodFlag := flag.String("method", "GET", "HTTP method to use")
	formatFlag := flag.String("format", "plain", "Output format (plain, json, csv)")

	flag.Parse()

	config := Config{
		URL:         *urlFlag,
		Requests:    *requestsFlag,
		Concurrency: *concurrencyFlag,
		Timeout:     *timeoutFlag,
		Method:      *methodFlag,
		Format:      *formatFlag,
	}

	if config.URL == "" || config.Requests == 0 {
		fmt.Println("URL and number of requests are required")
		return
	}

	report := executeLoadTest(config)
	printReport(report)
	printErrorDetails(report)
}

func executeLoadTest(config Config) Report {
	results := make(chan Result, config.Requests)
	start := time.Now()

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, config.Concurrency)

	// Mostrar progresso
	progress := make(chan int, config.Requests)
	go showProgress(config.Requests, progress)

	for i := 0; i < config.Requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			makeRequest(config, results)
			progress <- 1
			<-semaphore
		}()
	}

	go func() {
		wg.Wait()
		close(results)
		close(progress)
	}()

	return collectResults(results, start)
}

func showProgress(total int, progress chan int) {
	current := 0
	start := time.Now()

	for range progress {
		current++
		percent := float64(current) / float64(total) * 100
		elapsed := time.Since(start)
		rate := float64(current) / elapsed.Seconds()

		fmt.Printf("\rProgress: %.1f%% (%d/%d) | Rate: %.2f req/s",
			percent, current, total, rate)
	}
	fmt.Println()
}

func makeRequest(config Config, results chan<- Result) {
	client := &http.Client{
		Timeout: config.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
		},
	}

	req, err := http.NewRequest(config.Method, config.URL, strings.NewReader(config.Body))
	if err != nil {
		results <- Result{Error: err}
		return
	}

	// Adicionar headers
	for k, v := range config.Headers {
		req.Header.Add(k, v)
	}

	start := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(start)

	if err != nil {
		results <- Result{
			Error:    err,
			Duration: duration,
		}
		return
	}
	defer resp.Body.Close()

	results <- Result{
		StatusCode: resp.StatusCode,
		Duration:   duration,
	}
}

func collectResults(results chan Result, startTime time.Time) Report {
	report := Report{
		StatusCodes:  make(map[int]int),
		Durations:    make([]time.Duration, 0),
		MinDuration:  time.Hour,
		ErrorDetails: make(map[string]ErrorDetail),
	}

	for result := range results {
		report.TotalRequests++

		if result.Error != nil {
			report.Errors++
			errMsg := result.Error.Error()
			detail := report.ErrorDetails[errMsg]
			detail.Count++
			detail.Message = errMsg
			report.ErrorDetails[errMsg] = detail
		}

		report.StatusCodes[result.StatusCode]++
		report.Durations = append(report.Durations, result.Duration)

		if result.Duration < report.MinDuration {
			report.MinDuration = result.Duration
		}
		if result.Duration > report.MaxDuration {
			report.MaxDuration = result.Duration
		}
	}

	report.TotalTime = time.Since(startTime)

	// Calcular média
	var total time.Duration
	for _, d := range report.Durations {
		total += d
	}
	if len(report.Durations) > 0 {
		report.AvgDuration = total / time.Duration(len(report.Durations))
	}

	// Calcular RPS
	report.RPS = float64(report.TotalRequests) / report.TotalTime.Seconds()

	// Calcular desvio padrão
	report.StdDeviation = calculateStdDeviation(report.Durations, report.AvgDuration)

	return report
}

func calculatePercentile(durations []time.Duration, percentile float64) time.Duration {
	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	index := int(float64(len(durations)) * percentile / 100)
	if index >= len(durations) {
		index = len(durations) - 1
	}
	return durations[index]
}

func calculateStdDeviation(durations []time.Duration, avg time.Duration) time.Duration {
	var sumSquares float64
	for _, d := range durations {
		diff := d.Seconds() - avg.Seconds()
		sumSquares += diff * diff
	}
	variance := sumSquares / float64(len(durations))
	return time.Duration(math.Sqrt(variance) * float64(time.Second))
}

func printReport(report Report) {
	fmt.Printf("\n📊 Test Results Summary\n")
	fmt.Printf("----------------------------------------\n")
	fmt.Printf("Total Time: %.2f seconds\n", report.TotalTime.Seconds())
	fmt.Printf("Total Requests: %d\n", report.TotalRequests)
	fmt.Printf("Requests per Second: %.2f\n", report.RPS)
	fmt.Printf("----------------------------------------\n\n")

	fmt.Printf("⚡ Response Time Stats\n")
	fmt.Printf("----------------------------------------\n")
	fmt.Printf("Minimum: %v\n", report.MinDuration)
	fmt.Printf("Maximum: %v\n", report.MaxDuration)
	fmt.Printf("Average: %v\n", report.AvgDuration)
	if len(report.Durations) > 0 {
		fmt.Printf("P50: %v\n", calculatePercentile(report.Durations, 50))
		fmt.Printf("P90: %v\n", calculatePercentile(report.Durations, 90))
		fmt.Printf("P95: %v\n", calculatePercentile(report.Durations, 95))
		fmt.Printf("P99: %v\n", calculatePercentile(report.Durations, 99))
	}
	fmt.Printf("----------------------------------------\n\n")

	fmt.Printf("📈 Status Code Distribution\n")
	fmt.Printf("----------------------------------------\n")
	for code, count := range report.StatusCodes {
		percentage := float64(count) / float64(report.TotalRequests) * 100
		fmt.Printf("Status %d: %d requests (%.1f%%)\n", code, count, percentage)
	}
	fmt.Printf("----------------------------------------\n")

	if report.Errors > 0 {
		fmt.Printf("\n❌ Errors: %d (%.1f%%)\n",
			report.Errors,
			float64(report.Errors)/float64(report.TotalRequests)*100)
	}
}

func printErrorDetails(report Report) {
	if report.Errors > 0 {
		fmt.Printf("\n❌ Error Details:\n")
		fmt.Printf("----------------------------------------\n")
		for errType, detail := range report.ErrorDetails {
			fmt.Printf("%s: %d occurrences (%.1f%%)\n",
				errType,
				detail.Count,
				float64(detail.Count)/float64(report.TotalRequests)*100)
		}
	}
}
