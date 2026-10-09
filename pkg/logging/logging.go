// Package logging provides structured JSON logging with request/shop/user context.
package logging

import (
	"context"
	"log/slog"
	"os"
	"time"
)

type ctxKey string

const (
	ctxRequestID ctxKey = "request_id"
	ctxUserID    ctxKey = "user_id"
	ctxShopID    ctxKey = "shop_id"
)

func New(env string) *slog.Logger {
	level := slog.LevelInfo
	if env == "development" || env == "test" {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{Key: "timestamp", Value: slog.StringValue(a.Value.Time().UTC().Format(time.RFC3339Nano))}
			}
			return a
		},
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	return slog.New(handler)
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxRequestID, id)
}

func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxUserID, id)
}

func WithShopID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxShopID, id)
}

func FromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	attrs := []any{}
	if v, ok := ctx.Value(ctxRequestID).(string); ok && v != "" {
		attrs = append(attrs, "request_id", v)
	}
	if v, ok := ctx.Value(ctxUserID).(string); ok && v != "" {
		attrs = append(attrs, "user_id", v)
	}
	if v, ok := ctx.Value(ctxShopID).(string); ok && v != "" {
		attrs = append(attrs, "shop_id", v)
	}
	if len(attrs) == 0 {
		return base
	}
	return base.With(attrs...)
}

func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

func UserID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxUserID).(string); ok {
		return v
	}
	return ""
}

func ShopID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxShopID).(string); ok {
		return v
	}
	return ""
}
