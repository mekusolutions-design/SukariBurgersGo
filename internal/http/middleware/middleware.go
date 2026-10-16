package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/restflow/api-go/internal/auth"
	apperrors "github.com/restflow/api-go/pkg/errors"
	"github.com/restflow/api-go/internal/observability"
	"github.com/restflow/api-go/pkg/logging"
	"log/slog"
)

// RequestID injects a request ID into context and response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}
		ctx := logging.WithRequestID(r.Context(), id)
		ctx = context.WithValue(ctx, apperrors.RequestIDKey(), id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Logger logs request method, path, status, duration with structured fields.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(rw, r)
			l := logging.FromContext(r.Context(), log)
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if rw.status >= 500 {
				l.Error("request", attrs...)
			} else if rw.status >= 400 {
				l.Warn("request", attrs...)
			} else {
				l.Info("request", attrs...)
			}
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Recover catches panics and returns a clean 500.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					l := logging.FromContext(r.Context(), log)
					l.Error("panic", "recover", rec, "path", r.URL.Path, "method", r.Method)
					observability.ReportPanic(rec, r.URL.Path)
					apperrors.WriteJSON(w, r, apperrors.Internal("Internal server error"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORS applies configurable CORS. Empty origins = reflect request origin (dev).
func CORS(origins []string) func(http.Handler) http.Handler {
	allowAll := len(origins) == 0
	originSet := map[string]bool{}
	for _, o := range origins {
		originSet[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if allowAll {
				if origin != "" {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
			} else if originSet[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, Origin, X-Requested-With, Idempotency-Key")
			w.Header().Set("Access-Control-Expose-Headers", "Authorization, X-Request-ID")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Authenticate validates JWT and injects principal into context.
// Public paths must be registered before this middleware or excluded.
func Authenticate(authSvc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				apperrors.WriteJSON(w, r, apperrors.Unauthorized("Missing or invalid authorization"))
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")
			claims, err := authSvc.ValidateToken(tokenStr)
			if err != nil {
				apperrors.WriteJSON(w, r, err)
				return
			}
			ctx := auth.WithPrincipal(r.Context(), claims)
			ctx = logging.WithUserID(ctx, claims.Sub)
			ctx = logging.WithShopID(ctx, claims.ShopID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRoles returns middleware that enforces one of the allowed roles.
func RequireRoles(roles ...auth.Role) func(http.Handler) http.Handler {
	allowed := map[auth.Role]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := auth.RoleFromContext(r.Context())
			if !allowed[role] {
				apperrors.WriteJSON(w, r, apperrors.Forbidden("Insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBody limits request body size.
func MaxBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}
