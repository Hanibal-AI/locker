// Command loadtest drives concurrent load against a real Locker process
// (over real sockets, not in-memory ServeHTTP calls) and reports latency
// percentiles, throughput, and memory use. It complements the
// `go test -bench` benchmarks in internal/proxy/bench_test.go, which
// measure the pipeline's CPU/alloc cost in isolation — this script
// exercises actual concurrent connection handling and goroutine
// scheduling under load. See Docs/roadmap.md Phase 5.2.
//
// Usage:
//
//	go run ./scripts/loadtest [-concurrency 50] [-requests 2000] [-stream]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Hanibal-AI/locker/internal/config"
	"github.com/Hanibal-AI/locker/internal/pii"
	"github.com/Hanibal-AI/locker/internal/proxy"
)

// stubProvider points the proxy at the fake upstream started by this
// script, mirroring internal/proxy's own test double.
type stubProvider struct{ baseURL string }

func (p *stubProvider) Name() string { return "loadtest-stub" }
func (p *stubProvider) Target(path string) string {
	return p.baseURL + strings.TrimPrefix(path, "/v1")
}
func (p *stubProvider) Authenticate(*http.Request) {}

const nonStreamingBody = `{"model":"gpt-4o","messages":[{"role":"user","content":` +
	`"Hi, I work with Martin at Renault in Boulogne. Please email jean.dupont@example.com."}]}`

const streamingBody = `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":` +
	`"Hi, I work with Martin at Renault in Boulogne. Please email jean.dupont@example.com."}]}`

func main() {
	concurrency := flag.Int("concurrency", 50, "number of concurrent clients")
	totalRequests := flag.Int("requests", 5000, "total number of requests to send")
	stream := flag.Bool("stream", false, "send streaming requests instead of non-streaming")
	flag.Parse()

	upstreamAddr := startFakeUpstream()
	lockerAddr := startLocker(upstreamAddr)

	url := fmt.Sprintf("http://%s/v1/chat/completions", lockerAddr)
	body := nonStreamingBody
	if *stream {
		body = streamingBody
	}

	var memBefore runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	latencies := make([]time.Duration, *totalRequests)
	var next atomic.Int32
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 10 * time.Second}

	start := time.Now()
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := next.Add(1) - 1
				if i >= int32(*totalRequests) {
					return
				}
				t0 := time.Now()
				if err := doRequest(client, url, body); err != nil {
					fmt.Fprintf(os.Stderr, "request %d failed: %v\n", i, err)
					continue
				}
				latencies[i] = time.Since(t0)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	report(*totalRequests, *concurrency, *stream, elapsed, latencies, memBefore, memAfter)
}

func doRequest(client *http.Client, url, body string) error {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}

func report(total, concurrency int, streaming bool, elapsed time.Duration, latencies []time.Duration, before, after runtime.MemStats) {
	sorted := append([]time.Duration(nil), latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	pct := func(p float64) time.Duration {
		if len(sorted) == 0 {
			return 0
		}
		idx := int(p * float64(len(sorted)-1))
		return sorted[idx]
	}

	mode := "non-streaming"
	if streaming {
		mode = "streaming"
	}

	fmt.Printf("mode:          %s\n", mode)
	fmt.Printf("requests:      %d\n", total)
	fmt.Printf("concurrency:   %d\n", concurrency)
	fmt.Printf("elapsed:       %s\n", elapsed)
	fmt.Printf("req/s:         %.1f\n", float64(total)/elapsed.Seconds())
	fmt.Printf("latency p50:   %s\n", pct(0.50))
	fmt.Printf("latency p95:   %s\n", pct(0.95))
	fmt.Printf("latency p99:   %s\n", pct(0.99))
	fmt.Printf("latency max:   %s\n", sorted[len(sorted)-1])
	fmt.Printf("heap delta:    %.2f MB\n", float64(after.HeapAlloc-before.HeapAlloc)/(1<<20))
	fmt.Printf("total allocs:  %d\n", after.Mallocs-before.Mallocs)
}

// startFakeUpstream runs a minimal OpenAI-compatible server standing in
// for a real LLM provider, so this script measures Locker's own added
// latency, not a real provider's.
func startFakeUpstream() string {
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)

		if payload.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)
			for _, chunk := range []string{
				`data: {"choices":[{"index":0,"delta":{"content":"Sure, I will email [EMAIL_1] and loop in [PERSON_1]."}}]}` + "\n\n",
				"data: [DONE]\n\n",
			} {
				_, _ = w.Write([]byte(chunk))
				flusher.Flush()
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"Sure, I will email [EMAIL_1] and loop in [PERSON_1]."}}]}`))
	})
	return startServer(mux)
}

func startLocker(upstreamAddr string) string {
	piiEngine, err := pii.NewEngine(config.PIIConfig{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "pii.NewEngine: %v\n", err)
		os.Exit(1)
	}
	server := proxy.New(proxy.Options{
		Provider:       &stubProvider{baseURL: "http://" + upstreamAddr},
		RequestTimeout: 5 * time.Second,
		PII:            piiEngine,
	})
	return startServer(server.Handler())
}

func startServer(handler http.Handler) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String()
}
