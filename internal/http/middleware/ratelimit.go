package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

type visitor struct {
	tokens float64
	last   time.Time
}

// RateLimit is a simple per-IP token bucket. limits: max requests per window.
func RateLimit(maxPerMinute int) func(http.Handler) http.Handler {
	if maxPerMinute <= 0 {
		maxPerMinute = 120
	}
	var mu sync.Mutex
	visitors := map[string]*visitor{}
	rate := float64(maxPerMinute) / 60.0

	go func() {
		t := time.NewTicker(5 * time.Minute)
		for range t.C {
			mu.Lock()
			cutoff := time.Now().Add(-10 * time.Minute)
			for k, v := range visitors {
				if v.last.Before(cutoff) {
					delete(visitors, k)
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			mu.Lock()
			v, ok := visitors[ip]
			now := time.Now()
			if !ok {
				v = &visitor{tokens: float64(maxPerMinute), last: now}
				visitors[ip] = v
			}
			elapsed := now.Sub(v.last).Seconds()
			v.tokens += elapsed * rate
			if v.tokens > float64(maxPerMinute) {
				v.tokens = float64(maxPerMinute)
			}
			v.last = now
			if v.tokens < 1 {
				mu.Unlock()
				apperrors.WriteJSON(w, r, apperrors.New(http.StatusTooManyRequests, "Too many requests"))
				return
			}
			v.tokens--
			mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}

// AuthRateLimit stricter limit for login/register.
func AuthRateLimit(maxPerMinute int) func(http.Handler) http.Handler {
	if maxPerMinute <= 0 {
		maxPerMinute = 10
	}
	return RateLimit(maxPerMinute)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := len(xff); i > 0 {
			// first hop
			for j := 0; j < len(xff); j++ {
				if xff[j] == ',' {
					return xff[:j]
				}
			}
			return xff
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
