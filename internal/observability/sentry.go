package observability

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/restflow/api-go/pkg/logging"
)

// InitSentry configures Sentry when DSN is non-empty. Returns a flush function.
func InitSentry(dsn, environment, release string, log *slog.Logger) (flush func()) {
	flush = func() {}
	if dsn == "" {
		if log != nil {
			log.Info("sentry disabled (SENTRY_DSN not set)")
		}
		return flush
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      environment,
		Release:          release,
		TracesSampleRate: 0.1, // 10% performance traces — adjust via ops
		EnableTracing:    true,
		AttachStacktrace: true,
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			// Strip Authorization headers if present on request
			if event.Request != nil && event.Request.Headers != nil {
				delete(event.Request.Headers, "Authorization")
				delete(event.Request.Headers, "authorization")
				delete(event.Request.Headers, "Cookie")
			}
			return event
		},
	})
	if err != nil {
		if log != nil {
			log.Error("sentry init failed", "error", err)
		}
		return flush
	}
	if log != nil {
		log.Info("sentry enabled", "environment", environment, "release", release)
	}
	return func() {
		sentry.Flush(3 * time.Second)
	}
}

// SentryMiddleware reports panics and attaches request hub. No-op if Sentry not inited.
func SentryMiddleware(next http.Handler) http.Handler {
	handler := sentryhttp.New(sentryhttp.Options{
		Repanic: true, // let our Recover still run after capture
	}).Handle(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub := sentry.GetHubFromContext(r.Context())
		if hub == nil {
			hub = sentry.CurrentHub().Clone()
			r = r.WithContext(sentry.SetHubOnContext(r.Context(), hub))
		}
		if rid := logging.RequestID(r.Context()); rid != "" {
			hub.Scope().SetTag("request_id", rid)
		}
		if uid := logging.UserID(r.Context()); uid != "" {
			hub.Scope().SetUser(sentry.User{ID: uid})
		}
		if sid := logging.ShopID(r.Context()); sid != "" {
			hub.Scope().SetTag("shop_id", sid)
		}
		handler.ServeHTTP(w, r)
	})
}

// CaptureError sends an error to Sentry with optional tags (safe if disabled).
func CaptureError(err error, tags map[string]string) {
	if err == nil || sentry.CurrentHub().Client() == nil {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		for k, v := range tags {
			scope.SetTag(k, v)
		}
		sentry.CaptureException(err)
	})
}

// CaptureMessage sends a message event (warnings, business anomalies).
func CaptureMessage(msg string, tags map[string]string) {
	if sentry.CurrentHub().Client() == nil {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		for k, v := range tags {
			scope.SetTag(k, v)
		}
		sentry.CaptureMessage(msg)
	})
}

// ReportPanic helper for Recover middleware.
func ReportPanic(rec interface{}, path string) {
	if sentry.CurrentHub().Client() == nil {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("path", path)
		scope.SetLevel(sentry.LevelFatal)
		sentry.CaptureException(fmt.Errorf("panic: %v", rec))
	})
}
