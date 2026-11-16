package tenant

import (
	"context"
	"testing"
)

func TestWithFromContext(t *testing.T) {
	tc := &Context{TenantID: "t1", LegacyShopID: "1", UserID: "u1", IsActive: true, TenantStatus: "ACTIVE"}
	ctx := WithContext(context.Background(), tc)
	got, ok := FromContext(ctx)
	if !ok || got.TenantID != "t1" || LegacyShopID(ctx) != "1" {
		t.Fatalf("context roundtrip failed: %+v ok=%v", got, ok)
	}
}

func TestRequireInactive(t *testing.T) {
	tc := &Context{IsActive: false, TenantStatus: "ACTIVE"}
	ctx := WithContext(context.Background(), tc)
	if _, err := Require(ctx); err == nil {
		t.Fatal("expected error for inactive membership")
	}
}

func TestRequireSuspendedTenant(t *testing.T) {
	tc := &Context{IsActive: true, TenantStatus: "SUSPENDED"}
	ctx := WithContext(context.Background(), tc)
	if _, err := Require(ctx); err == nil {
		t.Fatal("expected error for suspended tenant")
	}
}
