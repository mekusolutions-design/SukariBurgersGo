package fefo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/restflow/api-go/pkg/money"
)

// ConsumeResult mirrors Nest fefo-stock.consumeStock return shape.
type ConsumeResult struct {
	ItemID      string
	ShopID      string
	Quantity    float64
	UnitCostWAC float64
	Value       float64
}

func tableMissing(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "does not exist") || strings.Contains(s, "42P01")
}

// ConsumeStock deducts qty from InventoryBatch FEFO (earliest expiry first, nulls last).
// If InventoryBatch relation is missing, returns zero consume (projection still deducts).
func ConsumeStock(ctx context.Context, tx pgx.Tx, shopID, itemID string, qty float64) (*ConsumeResult, error) {
	if qty <= 0 || itemID == "" {
		return &ConsumeResult{ItemID: itemID, ShopID: shopID}, nil
	}
	need := qty
	if err := seedSyntheticLotIfNeeded(ctx, tx, shopID, itemID); err != nil {
		if tableMissing(err) {
			return &ConsumeResult{ItemID: itemID, ShopID: shopID}, nil
		}
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		SELECT id, qty_remaining, unit_cost::text, expiry_date
		FROM "InventoryBatch"
		WHERE shop_id = $1 AND item_id = $2 AND qty_remaining > 0
		ORDER BY expiry_date ASC NULLS LAST, received_at ASC NULLS LAST, created_at ASC
		FOR UPDATE`, shopID, itemID)
	if err != nil {
		if tableMissing(err) {
			return &ConsumeResult{ItemID: itemID, ShopID: shopID}, nil
		}
		return nil, fmt.Errorf("fefo list batches: %w", err)
	}
	type lot struct {
		id   string
		qty  float64
		cost money.Money
	}
	var lots []lot
	for rows.Next() {
		var id string
		var q float64
		var costStr *string
		var exp *time.Time
		if err := rows.Scan(&id, &q, &costStr, &exp); err != nil {
			rows.Close()
			return nil, err
		}
		c := money.Zero()
		if costStr != nil {
			if m, e := money.New(*costStr); e == nil {
				c = m
			}
		}
		lots = append(lots, lot{id: id, qty: q, cost: c})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	consumed := 0.0
	value := money.Zero()
	for _, l := range lots {
		if need <= 0 {
			break
		}
		take := l.qty
		if take > need {
			take = need
		}
		if take <= 0 {
			continue
		}
		_, err := tx.Exec(ctx, `
			UPDATE "InventoryBatch"
			SET qty_remaining = qty_remaining - $1, updated_at = $2
			WHERE id = $3 AND qty_remaining >= $1`,
			take, time.Now().UTC(), l.id)
		if err != nil {
			if tableMissing(err) {
				break
			}
			return nil, fmt.Errorf("fefo update batch: %w", err)
		}
		need -= take
		consumed += take
		value = value.Add(l.cost.MulQty(take))
	}

	wac := 0.0
	if consumed > 0 {
		if u, err := value.DivQty(consumed); err == nil {
			wac = u.Float64()
		}
	}
	if wac <= 0 && consumed > 0 {
		var avgStr *string
		_ = tx.QueryRow(ctx, `
			SELECT avg_unit_cost::text FROM "InventoryProjection"
			WHERE shop_id = $1 AND item_id = $2`, shopID, itemID).Scan(&avgStr)
		if avgStr != nil {
			if m, e := money.New(*avgStr); e == nil {
				wac = m.Float64()
				value = m.MulQty(consumed)
			}
		}
	}

	return &ConsumeResult{
		ItemID: itemID, ShopID: shopID,
		Quantity: consumed, UnitCostWAC: wac, Value: value.Float64(),
	}, nil
}

func seedSyntheticLotIfNeeded(ctx context.Context, tx pgx.Tx, shopID, itemID string) error {
	var open float64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(qty_remaining),0) FROM "InventoryBatch"
		WHERE shop_id = $1 AND item_id = $2 AND qty_remaining > 0`, shopID, itemID).Scan(&open)
	if err != nil {
		if tableMissing(err) {
			return err // caller treats as soft
		}
		return nil // no rows / other — try projection path only if query worked
	}
	if open > 0 {
		return nil
	}
	var stock float64
	var avgStr *string
	err = tx.QueryRow(ctx, `
		SELECT available_stock, avg_unit_cost::text FROM "InventoryProjection"
		WHERE shop_id = $1 AND item_id = $2`, shopID, itemID).Scan(&stock, &avgStr)
	if err != nil || stock <= 0 {
		return nil
	}
	uc := "0"
	if avgStr != nil && *avgStr != "" {
		uc = *avgStr
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO "InventoryBatch" (
			id, shop_id, item_id, batch_number, qty_received, qty_remaining,
			unit_cost, received_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$5,$6::numeric,$7,$7,$7)`,
		uuid.New().String(), shopID, itemID, fmt.Sprintf("SYN-%d", now.Unix()),
		stock, uc, now)
	if tableMissing(err) {
		return err
	}
	return err
}
