// Package tenant provides multi-tenant request context and membership checks.
// Phase 1B foundation: resolve tenant/shop from membership tables while
// operational queries continue to use legacy shop_id strings.
package tenant

import (
	"context"
	"errors"
)

type ctxKey int

const keyCtx ctxKey = 1

// Context is the validated server-side tenant/shop scope for a request.
type Context struct {
	TenantID      string
	TenantStatus  string // ACTIVE, SUSPENDED, CANCELLED
	ShopID        string // Shop UUID (internal)
	LegacyShopID  string // Operational shop_id used by Event/projections/JWT
	UserID        string
	MembershipID  string
	Role          string
	IsActive      bool
}

// WithContext stores tenant context on the request context.
func WithContext(ctx context.Context, tc *Context) context.Context {
	return context.WithValue(ctx, keyCtx, tc)
}

// FromContext returns tenant context if present.
func FromContext(ctx context.Context) (*Context, bool) {
	tc, ok := ctx.Value(keyCtx).(*Context)
	return tc, ok && tc != nil
}

// Require returns tenant context or error.
func Require(ctx context.Context) (*Context, error) {
	tc, ok := FromContext(ctx)
	if !ok {
		return nil, errors.New("tenant context required")
	}
	if !tc.IsActive {
		return nil, errors.New("membership inactive")
	}
	if tc.TenantStatus != "" && tc.TenantStatus != "ACTIVE" {
		return nil, errors.New("tenant not active")
	}
	return tc, nil
}

// LegacyShopID is the operational shop filter for SQL (empty if no context).
func LegacyShopID(ctx context.Context) string {
	if tc, ok := FromContext(ctx); ok {
		return tc.LegacyShopID
	}
	return ""
}
