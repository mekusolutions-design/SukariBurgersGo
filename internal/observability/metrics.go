package observability

import (
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "restflow_http_requests_total",
		Help: "Total HTTP requests by method, route template, and status class",
	}, []string{"method", "route", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "restflow_http_request_duration_seconds",
		Help:    "HTTP request latency",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"method", "route", "status"})

	httpInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "restflow_http_in_flight",
		Help: "In-flight HTTP requests",
	})

	EventAppends = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "restflow_event_appends_total",
		Help: "Event store appends by type and result",
	}, []string{"event_type", "result"})

	ProjectionErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "restflow_projection_errors_total",
		Help: "Projection failures by event type",
	}, []string{"event_type"})

	WSConnections = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "restflow_ws_connections",
		Help: "Active realtime connections by transport",
	}, []string{"transport"})
)

var (
	uuidRe = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	hexRe  = regexp.MustCompile(`(?i)\b[0-9a-f]{16,}\b`)
	numSeg = regexp.MustCompile(`/\d+(/|$)`)
	prodID = regexp.MustCompile(`(?i)/PROD-[^/]+`)
)

// NormalizeRoute reduces cardinality for Prometheus labels.
func NormalizeRoute(path string) string {
	if path == "" {
		return "/"
	}
	p := uuidRe.ReplaceAllString(path, "{id}")
	p = prodID.ReplaceAllString(p, "/{productionId}")
	p = hexRe.ReplaceAllString(p, "{id}")
	p = numSeg.ReplaceAllString(p, "/{id}$1")
	if len(p) > 80 {
		p = p[:80]
	}
	return p
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// MetricsMiddleware records request counts and latency.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never instrument the scrape endpoint itself with high-cardinality noise
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		httpInFlight.Inc()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		defer func() {
			httpInFlight.Dec()
			route := NormalizeRoute(r.URL.Path)
			status := strconv.Itoa(rec.status)
			httpRequests.WithLabelValues(r.Method, route, status).Inc()
			httpDuration.WithLabelValues(r.Method, route, status).Observe(time.Since(start).Seconds())
		}()
		next.ServeHTTP(rec, r)
	})
}

// MetricsHandler returns the Prometheus scrape handler.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// MetricsAuth wraps /metrics with optional bearer token.
func MetricsAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		if authz != "Bearer "+token && r.URL.Query().Get("token") != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
