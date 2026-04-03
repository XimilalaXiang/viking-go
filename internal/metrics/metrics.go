package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Collector aggregates metrics from all viking-go subsystems.
type Collector struct {
	counters   map[string]*int64
	gauges     map[string]*int64
	histograms map[string]*Histogram
	mu         sync.RWMutex
	startTime  time.Time
}

// Histogram tracks value distribution in predefined buckets.
type Histogram struct {
	buckets []float64
	counts  []int64
	sum     int64
	count   int64
	mu      sync.Mutex
}

var global *Collector

func init() {
	global = New()
}

// Global returns the global metrics collector.
func Global() *Collector { return global }

// New creates a new metrics collector.
func New() *Collector {
	return &Collector{
		counters:   make(map[string]*int64),
		gauges:     make(map[string]*int64),
		histograms: make(map[string]*Histogram),
		startTime:  time.Now(),
	}
}

// Counter returns (and lazily creates) a named counter.
func (c *Collector) Counter(name string) *int64 {
	c.mu.RLock()
	if p, ok := c.counters[name]; ok {
		c.mu.RUnlock()
		return p
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.counters[name]; ok {
		return p
	}
	var v int64
	c.counters[name] = &v
	return &v
}

// Gauge returns (and lazily creates) a named gauge.
func (c *Collector) Gauge(name string) *int64 {
	c.mu.RLock()
	if p, ok := c.gauges[name]; ok {
		c.mu.RUnlock()
		return p
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.gauges[name]; ok {
		return p
	}
	var v int64
	c.gauges[name] = &v
	return &v
}

// Inc increments a counter by 1.
func Inc(name string) {
	atomic.AddInt64(global.Counter(name), 1)
}

// Add adds delta to a counter.
func Add(name string, delta int64) {
	atomic.AddInt64(global.Counter(name), delta)
}

// Set sets a gauge to a specific value.
func Set(name string, val int64) {
	atomic.StoreInt64(global.Gauge(name), val)
}

// Observe records a duration in milliseconds to a histogram.
func Observe(name string, d time.Duration) {
	global.mu.RLock()
	h, ok := global.histograms[name]
	global.mu.RUnlock()

	if !ok {
		global.mu.Lock()
		h, ok = global.histograms[name]
		if !ok {
			h = newHistogram()
			global.histograms[name] = h
		}
		global.mu.Unlock()
	}

	h.observe(d.Milliseconds())
}

func newHistogram() *Histogram {
	buckets := []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000}
	return &Histogram{
		buckets: buckets,
		counts:  make([]int64, len(buckets)+1),
	}
}

func (h *Histogram) observe(ms int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	atomic.AddInt64(&h.sum, ms)
	atomic.AddInt64(&h.count, 1)
	for i, b := range h.buckets {
		if float64(ms) <= b {
			h.counts[i]++
			return
		}
	}
	h.counts[len(h.buckets)]++
}

// Handler returns an http.Handler that serves metrics in Prometheus exposition format.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(global.Render()))
	})
}

// Render produces Prometheus-format metrics text.
func (c *Collector) Render() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# viking_go_uptime_seconds %d\n", int(time.Since(c.startTime).Seconds())))
	sb.WriteString("\n")

	counterNames := sortedKeys(c.counters)
	for _, name := range counterNames {
		v := atomic.LoadInt64(c.counters[name])
		sb.WriteString(fmt.Sprintf("# TYPE %s counter\n%s %d\n", name, name, v))
	}

	if len(counterNames) > 0 {
		sb.WriteString("\n")
	}

	gaugeNames := sortedKeys(c.gauges)
	for _, name := range gaugeNames {
		v := atomic.LoadInt64(c.gauges[name])
		sb.WriteString(fmt.Sprintf("# TYPE %s gauge\n%s %d\n", name, name, v))
	}

	if len(gaugeNames) > 0 {
		sb.WriteString("\n")
	}

	histNames := sortedHistKeys(c.histograms)
	for _, name := range histNames {
		h := c.histograms[name]
		h.mu.Lock()
		sb.WriteString(fmt.Sprintf("# TYPE %s histogram\n", name))
		cumulative := int64(0)
		for i, b := range h.buckets {
			cumulative += h.counts[i]
			sb.WriteString(fmt.Sprintf("%s_bucket{le=\"%.0f\"} %d\n", name, b, cumulative))
		}
		cumulative += h.counts[len(h.buckets)]
		sb.WriteString(fmt.Sprintf("%s_bucket{le=\"+Inf\"} %d\n", name, cumulative))
		sb.WriteString(fmt.Sprintf("%s_sum %d\n", name, h.sum))
		sb.WriteString(fmt.Sprintf("%s_count %d\n", name, h.count))
		h.mu.Unlock()
	}

	return sb.String()
}

func sortedKeys(m map[string]*int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedHistKeys(m map[string]*Histogram) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
