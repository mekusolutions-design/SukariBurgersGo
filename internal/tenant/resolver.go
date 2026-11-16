package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Resolver loads membership and shop grants from Phase 1A tables.
type Resolver struct {
	pool *pgxpool.Pool
}

func NewResolver(pool *pgxpool.Pool) *Resolver {
	return &Resolver{pool: pool}
}

// Resolve validates that userID may operate on claimedLegacyShopID.
func (r *Resolver) Resolve(ctx context.Context, userID, claimedLegacyShopID string) (*Context, error) {
	if userID == "" {
		return nil, errors.New("user id required")
	}
	if claimedLegacyShopID == "" {
		claimedLegacyShopID = "1"
	}

	tablesPresent := r.TablesPresent(ctx)

	const q = `
SELECT m.id, m.tenant_id, m.role::text, m.is_active,
       t.status::text,
       s.id, s.legacy_shop_id
FROM memberships m
JOIN tenants t ON t.id = m.tenant_id
JOIN membership_shops ms ON ms.membership_id = m.id
JOIN shops s ON s.id = ms.shop_id
WHERE m.user_id = $1
  AND s.legacy_shop_id = $2
  AND m.is_active = true
  AND s.is_active = true
LIMIT 1`

	var tc Context
	err := r.pool.QueryRow(ctx, q, userID, claimedLegacyShopID).Scan(
		&tc.MembershipID, &tc.TenantID, &tc.Role, &tc.IsActive,
		&tc.TenantStatus,
		&tc.ShopID, &tc.LegacyShopID,
	)
	if err == nil {
		tc.UserID = userID
		if tc.TenantStatus != "ACTIVE" {
			return nil, fmt.Errorf("tenant status %s", tc.TenantStatus)
		}
		return &tc, nil
	}

	// No matching membership/shop grant
	if tablesPresent {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("no membership grant for shop %s", claimedLegacyShopID)
		}
		// Unexpected DB error while hierarchy exists — do not soft-fallback
		return nil, fmt.Errorf("membership resolve failed: %w", err)
	}

	// Phase 1A tables absent: legacy JWT shop only
	return &Context{
		UserID:       userID,
		LegacyShopID: claimedLegacyShopID,
		Role:         "",
		IsActive:     true,
		TenantStatus: "ACTIVE",
	}, nil
}

// TablesPresent reports whether Phase 1A hierarchy tables exist.
func (r *Resolver) TablesPresent(ctx context.Context) bool {
	var n int
	err := r.pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema = 'public' AND table_name IN ('tenants','shops','memberships','membership_shops')
`).Scan(&n)
	return err == nil && n == 4
}
