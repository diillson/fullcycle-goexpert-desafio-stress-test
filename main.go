package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net"
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
	data, _ := json.MarshalIndent(r, "", " ")
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
	Code    int // Código HTTP associado ao erro, se aplicável
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
		fmt.Printf("\rProgress: %.1f%% (%d/%d) | Rate: %.2f req/s", percent, current, total, rate)
	}
	fmt.Println()
}

func classifyErrorToHTTPStatus(err error) int {
	if err == nil {
		return 200 // OK (não deveria acontecer)
	}

	// Verifica se o erro é um erro de timeout
	if netErr, ok := err.(net.Error); ok {
		if netErr.Timeout() {
			return 408 // Request Timeout
		} else if netErr.Temporary() {
			return 503 // Service Unavailable (temporário)
		}
	}

	// Análise baseada no texto da mensagem de erro
	errMsg := err.Error()

	// Connection refused
	if strings.Contains(errMsg, "connection refused") {
		return 503 // Service Unavailable
	}

	// DNS/Host não encontrado
	if strings.Contains(errMsg, "no such host") ||
		strings.Contains(errMsg, "lookup") && strings.Contains(errMsg, "no such host") {
		return 404 // Not Found
	}

	// Erros de certificado SSL
	if strings.Contains(errMsg, "certificate") ||
		strings.Contains(errMsg, "x509") {
		return 495 // SSL Certificate Error (não padrão)
	}

	// Connection reset
	if strings.Contains(errMsg, "connection reset") {
		return 500 // Internal Server Error
	}

	// TLS handshake timeout
	if strings.Contains(errMsg, "TLS handshake timeout") {
		return 408 // Request Timeout
	}

	// Too many redirects
	if strings.Contains(errMsg, "stopped after") && strings.Contains(errMsg, "redirects") {
		return 310 // Too many redirects
	}

	// EOF
	if strings.Contains(errMsg, "EOF") {
		return 500 // Internal Server Error
	}

	// Connection closed
	if strings.Contains(errMsg, "connection closed") {
		return 500 // Internal Server Error
	}

	// Request canceled (context deadline exceeded)
	if strings.Contains(errMsg, "context deadline exceeded") {
		return 408 // Request Timeout
	}

	// Dial TCP especificamente
	if strings.Contains(errMsg, "dial tcp") {
		// Se contiver "connection refused"
		if strings.Contains(errMsg, "connection refused") {
			return 503 // Service Unavailable
		}
		// Se contiver "i/o timeout"
		if strings.Contains(errMsg, "i/o timeout") {
			return 408 // Request Timeout
		}
	}

	// Erro genérico de rede
	if strings.Contains(errMsg, "net/http") {
		return 500 // Internal Server Error
	}

	// Fallback para qualquer outro erro
	return 500 // Internal Server Error genérico
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
		statusCode := classifyErrorToHTTPStatus(err)
		results <- Result{
			StatusCode: statusCode,
			Error:      err,
			Duration:   0,
		}
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
		statusCode := classifyErrorToHTTPStatus(err)
		results <- Result{
			StatusCode: statusCode,
			Error:      err,
			Duration:   duration,
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

		// Incrementar contagem do código de status
		report.StatusCodes[result.StatusCode]++

		// Registrar erro se existir
		if result.Error != nil {
			report.Errors++
			errMsg := result.Error.Error()
			detail, exists := report.ErrorDetails[errMsg]
			if !exists {
				detail = ErrorDetail{
					Message: errMsg,
					Code:    result.StatusCode,
				}
			}
			detail.Count++
			report.ErrorDetails[errMsg] = detail
		}

		// Processar duração
		if result.Duration > 0 {
			report.Durations = append(report.Durations, result.Duration)
			if result.Duration < report.MinDuration {
				report.MinDuration = result.Duration
			}
			if result.Duration > report.MaxDuration {
				report.MaxDuration = result.Duration
			}
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
	if len(durations) == 0 {
		return 0
	}

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
	if len(durations) == 0 {
		return 0
	}

	var sumSquares float64
	for _, d := range durations {
		diff := d.Seconds() - avg.Seconds()
		sumSquares += diff * diff
	}
	variance := sumSquares / float64(len(durations))
	return time.Duration(math.Sqrt(variance) * float64(time.Second))
}

func getStatusCodeDescription(code int) string {
	switch code {
	case 0:
		return "Erro não identificado"
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 204:
		return "No Content"
	case 310:
		return "Too Many Redirects"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 408:
		return "Request Timeout"
	case 429:
		return "Too Many Requests"
	case 495:
		return "SSL Certificate Error"
	case 500:
		return "Internal Server Error"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	case 504:
		return "Gateway Timeout"
	default:
		return "Status Code " + fmt.Sprintf("%d", code)
	}
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
	if len(report.Durations) > 0 {
		fmt.Printf("Minimum: %v\n", report.MinDuration)
		fmt.Printf("Maximum: %v\n", report.MaxDuration)
		fmt.Printf("Average: %v\n", report.AvgDuration)
		fmt.Printf("P50: %v\n", calculatePercentile(report.Durations, 50))
		fmt.Printf("P90: %v\n", calculatePercentile(report.Durations, 90))
		fmt.Printf("P95: %v\n", calculatePercentile(report.Durations, 95))
		fmt.Printf("P99: %v\n", calculatePercentile(report.Durations, 99))
	} else {
		fmt.Printf("No successful requests to measure response time\n")
	}
	fmt.Printf("----------------------------------------\n\n")

	fmt.Printf("📈 Status Code Distribution\n")
	fmt.Printf("----------------------------------------\n")

	// Ordenar códigos para exibição
	var codes []int
	for code := range report.StatusCodes {
		codes = append(codes, code)
	}
	sort.Ints(codes)

	// Destacar sucesso e falhas
	successCount := report.StatusCodes[200]
	successRate := float64(successCount) / float64(report.TotalRequests) * 100

	fmt.Printf("✅ Status 200 (Success): %d requests (%.1f%%)\n", successCount, successRate)

	for _, code := range codes {
		if code == 200 {
			continue // Já exibimos o 200 acima
		}

		count := report.StatusCodes[code]
		percentage := float64(count) / float64(report.TotalRequests) * 100

		if code >= 400 || code == 0 {
			// Erro
			fmt.Printf("❌ Status %d (%s): %d requests (%.1f%%)\n",
				code, getStatusCodeDescription(code), count, percentage)
		} else if code >= 300 {
			// Redirecionamento
			fmt.Printf("↪️ Status %d (%s): %d requests (%.1f%%)\n",
				code, getStatusCodeDescription(code), count, percentage)
		} else {
			// Outros códigos de sucesso
			fmt.Printf("✅ Status %d (%s): %d requests (%.1f%%)\n",
				code, getStatusCodeDescription(code), count, percentage)
		}
	}
	fmt.Printf("----------------------------------------\n")

	if report.Errors > 0 {
		errorRate := float64(report.Errors) / float64(report.TotalRequests) * 100
		fmt.Printf("\n❌ Total Errors: %d (%.1f%%)\n", report.Errors, errorRate)
	}
}

func printErrorDetails(report Report) {
	if report.Errors > 0 {
		fmt.Printf("\n❌ Detalhes dos Erros:\n")
		fmt.Printf("----------------------------------------\n")
		fmt.Printf("| %-8s | %-50s | %-8s | %-10s |\n",
			"Status", "Mensagem de Erro", "Count", "Percentual")
		fmt.Printf("----------------------------------------\n")

		// Ordenar erros por contagem
		type ErrEntry struct {
			Type   string
			Detail ErrorDetail
		}

		entries := make([]ErrEntry, 0, len(report.ErrorDetails))
		for errType, detail := range report.ErrorDetails {
			entries = append(entries, ErrEntry{errType, detail})
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Detail.Count > entries[j].Detail.Count
		})

		for _, entry := range entries {
			percent := float64(entry.Detail.Count) / float64(report.TotalRequests) * 100

			shortErrType := entry.Type
			if len(shortErrType) > 50 {
				shortErrType = shortErrType[:47] + "..."
			}

			fmt.Printf("| %-8d | %-50s | %-8d | %-9.1f%% |\n",
				entry.Detail.Code,
				shortErrType,
				entry.Detail.Count,
				percent)
		}
		fmt.Printf("----------------------------------------\n")
	}
}
