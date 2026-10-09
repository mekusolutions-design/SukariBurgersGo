package locking

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/jackc/pgx/v5"
)

const shopLockKey1 = 123456789

// AcquireShopLock acquires a transaction-scoped PostgreSQL advisory lock for a shop.
// The lock is automatically released when the transaction ends (commit or rollback).
// Mirrors NestJS AdvisoryLockService.acquireShopLock.
func AcquireShopLock(ctx context.Context, tx pgx.Tx, shopID string) error {
	key2 := hashStringToInt(shopID)
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, shopLockKey1, key2)
	if err != nil {
		return fmt.Errorf("acquire advisory lock for shop %s: %w", shopID, err)
	}
	return nil
}

func hashStringToInt(s string) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int32(h.Sum32() & 0x7fffffff) // positive
}

// ReserveStock atomically decrements available_stock if sufficient quantity exists.
// Returns true if the row was updated. Must run inside a transaction that also holds shop lock.
func ReserveStock(ctx context.Context, tx pgx.Tx, shopID, itemID string, qty float64) (bool, error) {
	if qty <= 0 {
		return false, fmt.Errorf("quantity must be positive")
	}
	tag, err := tx.Exec(ctx, `
		UPDATE "InventoryProjection"
		SET available_stock = available_stock - $3,
		    updated_at = NOW()
		WHERE shop_id = $1 AND item_id = $2 AND available_stock >= $3`,
		shopID, itemID, qty)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
