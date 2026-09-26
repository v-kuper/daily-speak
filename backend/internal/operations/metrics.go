package operations

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var durationBuckets = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type requestKey struct {
	method string
	route  string
	status int
}

type requestMeasurement struct {
	count   uint64
	sum     float64
	buckets [len(durationBuckets)]uint64
}

type Metrics struct {
	mu       sync.Mutex
	requests map[requestKey]requestMeasurement
	inFlight atomic.Int64
}

type DatabaseSnapshot struct {
	TotalConns    int32
	IdleConns     int32
	AcquiredConns int32
	MaxConns      int32
}

type QueueSnapshot struct {
	Kind             string
	State            string
	Count            int64
	OldestAgeSeconds float64
	RecentTerminal   bool
}

func NewMetrics() *Metrics {
	return &Metrics{requests: make(map[requestKey]requestMeasurement)}
}

func (m *Metrics) Begin() func(string, string, int, time.Duration) {
	if m == nil {
		return func(string, string, int, time.Duration) {}
	}
	m.inFlight.Add(1)
	return func(method string, route string, status int, duration time.Duration) {
		m.inFlight.Add(-1)
		m.Observe(method, route, status, duration)
	}
}

func (m *Metrics) Observe(method string, route string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	key := requestKey{method: method, route: route, status: status}
	seconds := duration.Seconds()
	m.mu.Lock()
	measurement := m.requests[key]
	measurement.count++
	measurement.sum += seconds
	for index, bucket := range durationBuckets {
		if seconds <= bucket {
			measurement.buckets[index]++
		}
	}
	m.requests[key] = measurement
	m.mu.Unlock()
}

func (m *Metrics) WritePrometheus(w io.Writer, database DatabaseSnapshot, queues []QueueSnapshot) {
	if m == nil {
		return
	}
	type item struct {
		key   requestKey
		value requestMeasurement
	}
	m.mu.Lock()
	items := make([]item, 0, len(m.requests))
	for key, value := range m.requests {
		items = append(items, item{key: key, value: value})
	}
	m.mu.Unlock()
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].key, items[j].key
		if left.route != right.route {
			return left.route < right.route
		}
		if left.method != right.method {
			return left.method < right.method
		}
		return left.status < right.status
	})
	_, _ = io.WriteString(w, "# HELP daily_speaking_http_requests_total Completed HTTP requests.\n")
	_, _ = io.WriteString(w, "# TYPE daily_speaking_http_requests_total counter\n")
	for _, item := range items {
		labels := requestLabels(item.key)
		_, _ = fmt.Fprintf(w, "daily_speaking_http_requests_total{%s} %d\n", labels, item.value.count)
	}
	_, _ = io.WriteString(w, "# HELP daily_speaking_http_request_duration_seconds HTTP request duration.\n")
	_, _ = io.WriteString(w, "# TYPE daily_speaking_http_request_duration_seconds histogram\n")
	for _, item := range items {
		labels := requestLabels(item.key)
		for index, bucket := range durationBuckets {
			_, _ = fmt.Fprintf(w, "daily_speaking_http_request_duration_seconds_bucket{%s,le=%q} %d\n", labels, strconv.FormatFloat(bucket, 'f', -1, 64), item.value.buckets[index])
		}
		_, _ = fmt.Fprintf(w, "daily_speaking_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels, item.value.count)
		_, _ = fmt.Fprintf(w, "daily_speaking_http_request_duration_seconds_sum{%s} %s\n", labels, strconv.FormatFloat(item.value.sum, 'f', 6, 64))
		_, _ = fmt.Fprintf(w, "daily_speaking_http_request_duration_seconds_count{%s} %d\n", labels, item.value.count)
	}
	_, _ = fmt.Fprintf(w, "# TYPE daily_speaking_http_requests_in_flight gauge\ndaily_speaking_http_requests_in_flight %d\n", m.inFlight.Load())
	writeGauge(w, "daily_speaking_db_pool_connections", "state", "total", float64(database.TotalConns))
	writeGauge(w, "daily_speaking_db_pool_connections", "state", "idle", float64(database.IdleConns))
	writeGauge(w, "daily_speaking_db_pool_connections", "state", "acquired", float64(database.AcquiredConns))
	writeGauge(w, "daily_speaking_db_pool_max_connections", "", "", float64(database.MaxConns))
	_, _ = io.WriteString(w, "# HELP daily_speaking_jobs Current active durable jobs.\n# TYPE daily_speaking_jobs gauge\n")
	_, _ = io.WriteString(w, "# HELP daily_speaking_job_oldest_age_seconds Oldest active durable job age.\n# TYPE daily_speaking_job_oldest_age_seconds gauge\n")
	_, _ = io.WriteString(w, "# HELP daily_speaking_jobs_completed_last_hour Terminal durable job outcomes in the last hour.\n# TYPE daily_speaking_jobs_completed_last_hour gauge\n")
	for _, queue := range queues {
		labels := "kind=\"" + escapeLabel(queue.Kind) + "\",state=\"" + escapeLabel(queue.State) + "\""
		if queue.RecentTerminal {
			_, _ = fmt.Fprintf(w, "daily_speaking_jobs_completed_last_hour{%s} %d\n", labels, queue.Count)
			continue
		}
		_, _ = fmt.Fprintf(w, "daily_speaking_jobs{%s} %d\n", labels, queue.Count)
		if queue.OldestAgeSeconds >= 0 {
			_, _ = fmt.Fprintf(w, "daily_speaking_job_oldest_age_seconds{%s} %s\n", labels, strconv.FormatFloat(queue.OldestAgeSeconds, 'f', 3, 64))
		}
	}
}

func writeGauge(w io.Writer, name, labelName, labelValue string, value float64) {
	if labelName == "" {
		_, _ = fmt.Fprintf(w, "%s %s\n", name, strconv.FormatFloat(value, 'f', -1, 64))
		return
	}
	_, _ = fmt.Fprintf(w, "%s{%s=\"%s\"} %s\n", name, labelName, escapeLabel(labelValue), strconv.FormatFloat(value, 'f', -1, 64))
}

func requestLabels(key requestKey) string {
	return "method=\"" + escapeLabel(key.method) + "\",route=\"" + escapeLabel(key.route) + "\",status=\"" + strconv.Itoa(key.status) + "\""
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return strings.ReplaceAll(value, "\"", "\\\"")
}
