package middleware

import (
	"context"
	"net/http"

	"github.com/restflow/api-go/internal/auth"
	"github.com/restflow/api-go/internal/tenant"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

// AttachTenant resolves tenant/shop membership after JWT authentication.
// When hierarchy tables exist and strict is true, missing membership/shop grant is forbidden.
// When tables are absent, behaviour remains legacy JWT shop_id (Phase 1A not applied).
func AttachTenant(resolver *tenant.Resolver, strict bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if resolver == nil {
				next.ServeHTTP(w, r)
				return
			}
			userID := auth.UserIDFromContext(r.Context())
			if userID == "" {
				next.ServeHTTP(w, r)
				return
			}
			claimedShop := auth.ShopIDFromContext(r.Context())
			tc, err := resolver.Resolve(r.Context(), userID, claimedShop)
			if err != nil {
				if strict && resolver.TablesPresent(r.Context()) {
					apperrors.WriteJSON(w, r, apperrors.Forbidden("Shop access denied for membership"))
					return
				}
				tc = &tenant.Context{
					UserID:       userID,
					LegacyShopID: claimedShop,
					IsActive:     true,
					TenantStatus: "ACTIVE",
					Role:         string(auth.RoleFromContext(r.Context())),
				}
			}
			ctx := tenant.WithContext(r.Context(), tc)
			// Keep auth shop claim aligned with validated legacy shop id
			if tc.LegacyShopID != "" {
				ctx = context.WithValue(ctx, auth.CtxShopID, tc.LegacyShopID)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
