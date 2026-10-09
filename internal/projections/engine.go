package projections

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/fefo"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/realtime"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/costing"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
	"github.com/shopspring/decimal"
)

// Engine implements eventstore.ProjectionRunner.
// Handlers run inside the same transaction as the event insert.
type Engine struct {
	log *slog.Logger
	rt  realtime.Broadcaster
}

func NewEngine(log *slog.Logger) *Engine {
	return &Engine{log: log}
}

// SetBroadcaster wires realtime after projections (append → project → emit).
// Optional: nil disables emits without changing projection math.
func (e *Engine) SetBroadcaster(rt realtime.Broadcaster) {
	e.rt = rt
}

func (e *Engine) emitStock(shopID, itemID, itemName string, available float64, extra map[string]interface{}) {
	if e.rt == nil || shopID == "" || itemID == "" {
		return
	}
	payload := map[string]interface{}{
		"item_id": itemID, "itemId": itemID,
		"item_name": itemName, "itemName": itemName,
		"available_stock": available, "availableStock": available,
		"shop_id": shopID, "shopId": shopID,
	}
	for k, v := range extra {
		payload[k] = v
	}
	e.rt.BroadcastNamespace("inventory", shopID, "stock_updated", payload)
	e.rt.BroadcastNamespace("inventory", shopID, "inventory_updated", payload)
}

func (e *Engine) emitLowStock(shopID, itemID, itemName string, current, threshold float64) {
	if e.rt == nil {
		return
	}
	payload := map[string]interface{}{
		"item_id": itemID, "item_name": itemName,
		"current_stock": current, "threshold": threshold,
		"shop_id": shopID,
	}
	e.rt.BroadcastNamespace("inventory", shopID, "low_stock_alert", payload)
}

func (e *Engine) UpdateProjections(ctx context.Context, tx pgx.Tx, event *eventstore.Event) error {
	var payload map[string]interface{}
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			e.log.Warn("projection: failed to unmarshal payload", "event_id", event.ID, "error", err)
			payload = map[string]interface{}{}
		}
	} else {
		payload = map[string]interface{}{}
	}

	shopID := event.ShopID
	if shopID == "" {
		shopID = asString(payload["shop_id"], asString(payload["shopId"], "1"))
	}

	switch event.EventType {
	case eventstore.EventReceived, eventstore.EventFinishedGoodAdded:
		return e.handleStockIncrease(ctx, tx, event, payload, shopID)
	case eventstore.EventWasteRecorded, "waste", "WASTE_RECORDED":
		return e.handleWaste(ctx, tx, event, payload, shopID)
	case eventstore.EventBatchExpired:
		return e.handleBatchExpired(ctx, tx, event, payload, shopID)
	case eventstore.EventFinishedGoodAdjusted:
		return e.handleAdjustment(ctx, tx, event, payload, shopID)
	case eventstore.EventRefillIssued:
		return e.handleRefillIssued(ctx, tx, event, payload, shopID)
	case eventstore.EventPosSale:
		// ONLY pos_sale mutates inventory + sales. Kitchen/status/payment must not.
		return e.handlePosSale(ctx, tx, event, payload, shopID)
	case eventstore.EventOrderSentToKitchen, eventstore.EventOrderStatusUpdated, eventstore.EventOrderPaymentUpdated:
		return nil
	case eventstore.EventClosingStockCounted:
		// Nest parity: stock_count_completed is list/query alias — do not apply twice
		return e.handleClosingStockCount(ctx, tx, event, payload, shopID)
	case eventstore.EventStockCountCompleted:
		return nil
	case eventstore.EventProductionFinished, eventstore.EventPrePrepCompleted:
		return e.handleProductionFinished(ctx, tx, event, payload, shopID)
	default:
		e.log.Debug("no projection handler", "event_type", event.EventType)
		return nil
	}
}

func (e *Engine) handleStockIncrease(ctx context.Context, tx pgx.Tx, event *eventstore.Event, payload map[string]interface{}, shopID string) error {
	itemID := firstNonEmpty(event.ItemID, strPtr(asString(payload["item_id"], asString(payload["itemId"], ""))))
	if itemID == nil || *itemID == "" {
		return nil
	}

	qty := asFloat(event.Quantity, asFloat(payload["quantity"], asFloat(payload["quantity_approved"], 0)))
	if qty <= 0 {
		return nil
	}

	unitCost := moneyFromEvent(event.UnitCost, payload, "unit_cost", "unitCost")
	totalCost := moneyFromEvent(event.TotalCost, payload, "total_cost", "totalCost", "line_total")
	if totalCost.IsZero() && !unitCost.IsZero() && qty > 0 {
		totalCost = unitCost.MulQty(qty)
	}
	if unitCost.IsZero() && !totalCost.IsZero() && qty > 0 {
		unitCost, _ = totalCost.Div(money.FromFloat(qty).Decimal())
	}

	var expiry *time.Time
	if event.ExpiryDate != nil {
		expiry = event.ExpiryDate
	} else if v := asString(payload["expiry_date"], ""); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			expiry = &t
		} else if t, err := time.Parse("2006-01-02", v); err == nil {
			expiry = &t
		}
	}

	var daysToExpiry *int
	if expiry != nil {
		d := int(math.Ceil(expiry.Sub(time.Now().UTC()).Hours() / 24))
		daysToExpiry = &d
	}

	// WAC: load current qty + avg, then rewrite stock + avg + value
	var prevQty float64
	var prevAvgStr *string
	_ = tx.QueryRow(ctx, `
		SELECT available_stock, avg_unit_cost::text
		FROM "InventoryProjection"
		WHERE shop_id = $1 AND item_id = $2
	`, shopID, *itemID).Scan(&prevQty, &prevAvgStr)

	prevAvg := 0.0
	if prevAvgStr != nil && *prevAvgStr != "" {
		prevAvg = asFloat(*prevAvgStr, 0)
	}
	receiptMoney := unitCost.AsUnitCost()
	if !receiptMoney.IsPositive() && !totalCost.IsZero() && qty > 0 {
		if u, err := totalCost.DivQty(qty); err == nil {
			receiptMoney = u.AsUnitCost()
		}
	}
	newAvgMoney := costing.WeightedAverageUnitCostDecimal(
		decimal.NewFromFloat(prevQty),
		decimal.NewFromFloat(qty),
		money.FromFloatUnitCost(prevAvg),
		receiptMoney,
	)
	newAvg := newAvgMoney.Float64()
	nextQty := prevQty + qty
	nextValue := newAvgMoney.MulQty(nextQty).AsTotal().Float64()

	const q = `
		INSERT INTO "InventoryProjection" (
			id, shop_id, item_id, available_stock, expired_stock, damaged_stock,
			total_value, avg_unit_cost, last_receipt_unit_cost,
			days_to_expiry_min, last_received_at, next_expiry_date, updated_at
		) VALUES (
			$1, $2, $3, $4, 0, 0,
			$5::numeric, $6::numeric, $7::numeric,
			$8, $9, $10, $11
		)
		ON CONFLICT (shop_id, item_id) DO UPDATE SET
			available_stock = $4,
			total_value = $5::numeric,
			avg_unit_cost = $6::numeric,
			last_receipt_unit_cost = COALESCE($7::numeric, "InventoryProjection".last_receipt_unit_cost),
			last_received_at = $9,
			next_expiry_date = COALESCE($10, "InventoryProjection".next_expiry_date),
			days_to_expiry_min = COALESCE($8, "InventoryProjection".days_to_expiry_min),
			updated_at = $11
	`
	now := time.Now().UTC()
	var receiptPtr interface{}
	if receiptMoney.IsPositive() {
		receiptPtr = receiptMoney.StringUnitCost()
	}
	_, err := tx.Exec(ctx, q,
		uuid.New().String(), shopID, *itemID, nextQty, fmt.Sprintf("%.4f", nextValue),
		fmt.Sprintf("%.6f", newAvg), receiptPtr,
		daysToExpiry, now, expiry, now,
	)
	if err != nil {
		return fmt.Errorf("stock increase projection: %w", err)
	}

	_ = ensureItemTx(ctx, tx, *itemID, payload)

	// Nest FEFO: record InventoryBatch so batches endpoint / FEFO consume work
	batchNumber := asString(payload["batch_number"], asString(payload["batchNumber"], ""))
	if batchNumber == "" {
		batchNumber = fmt.Sprintf("RCV-%d", now.UnixMilli())
	}
	supplierID := asString(payload["supplier_id"], asString(payload["supplierId"], ""))
	var supplierPtr interface{}
	if supplierID != "" {
		supplierPtr = supplierID
	}
	unitCostVal := receiptMoney.Float64()
	if unitCostVal < 0 {
		unitCostVal = 0
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO "InventoryBatch" (
			id, shop_id, item_id, batch_number, qty_received, qty_remaining,
			unit_cost, received_at, expiry_date, supplier_id, event_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $5,
			$6::numeric, $7, $8, $9, $10, $7, $7
		)`,
		uuid.New().String(), shopID, *itemID, batchNumber, qty,
		fmt.Sprintf("%.6f", unitCostVal), now, expiry, supplierPtr, event.ID,
	)
	if err != nil {
		// Nest: FEFO recordReceiveBatch is try/catch warn — stock projection must still commit
		if e.log != nil {
			e.log.Warn("inventory batch write skipped", "item_id", *itemID, "error", err)
		}
		// do not fail receive when InventoryBatch table missing or batch constraint fails
	}

	// Nest maybeSeedReorderPointFromReceive: ~20% of approved qty if unset or legacy 10
	_ = maybeSeedReorderPoint(ctx, tx, *itemID, qty, payload)

	name := asString(payload["item_name"], asString(payload["itemName"], *itemID))
	e.emitStock(shopID, *itemID, name, nextQty, map[string]interface{}{
		"total_value": nextValue, "avg_unit_cost": newAvg,
	})
	return nil
}

func (e *Engine) handleWaste(ctx context.Context, tx pgx.Tx, event *eventstore.Event, payload map[string]interface{}, shopID string) error {
	itemID := firstNonEmpty(event.ItemID, strPtr(asString(payload["item_id"], "")))
	if itemID == nil || *itemID == "" {
		return nil
	}
	qty := math.Abs(asFloat(event.Quantity, asFloat(payload["quantity_wasted"], asFloat(payload["quantity"], 0))))
	if qty <= 0 {
		return nil
	}
	totalValue := moneyFromEvent(event.TotalCost, payload, "total_waste_value", "total_cost")
	if totalValue.IsZero() {
		var avgStr *string
		_ = tx.QueryRow(ctx, `
			SELECT avg_unit_cost::text FROM "InventoryProjection"
			WHERE shop_id = $1 AND item_id = $2
		`, shopID, *itemID).Scan(&avgStr)
		if avgStr != nil {
			avg := asFloat(*avgStr, 0)
			totalValue = money.FromFloatUnitCost(avg).MulQty(qty).AsTotal()
		}
	}

	const q = `
		UPDATE "InventoryProjection"
		SET available_stock = GREATEST(0, available_stock - $1),
		    total_value = GREATEST(0, total_value - $2::numeric),
		    updated_at = $3
		WHERE shop_id = $4 AND item_id = $5
	`
	_, err := tx.Exec(ctx, q, qty, totalValue.String(), time.Now().UTC(), shopID, *itemID)
	if err != nil {
		return fmt.Errorf("waste projection: %w", err)
	}

	// Upsert WasteProjection
	const wq = `
		INSERT INTO "WasteProjection" (id, shop_id, item_id, total_wasted, total_value, last_wasted_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		ON CONFLICT (shop_id, item_id) DO UPDATE SET
			total_wasted = "WasteProjection".total_wasted + EXCLUDED.total_wasted,
			total_value = "WasteProjection".total_value + EXCLUDED.total_value,
			last_wasted_at = EXCLUDED.last_wasted_at,
			updated_at = EXCLUDED.updated_at
	`
	_, err = tx.Exec(ctx, wq, uuid.New().String(), shopID, *itemID, qty, totalValue.String(), time.Now().UTC())
	return err
}

func (e *Engine) handleBatchExpired(ctx context.Context, tx pgx.Tx, event *eventstore.Event, payload map[string]interface{}, shopID string) error {
	// Nest handleBatchExpired: available → expired, drop total_value, days_to_expiry_min=0
	itemID := firstNonEmpty(event.ItemID, strPtr(asString(payload["item_id"], "")))
	if itemID == nil || *itemID == "" {
		return nil
	}
	qty := math.Abs(asFloat(event.Quantity, asFloat(payload["quantity"], 0)))
	if qty <= 0 {
		return nil
	}
	totalValue := moneyFromEvent(event.TotalCost, payload, "total_cost", "total_waste_value")
	if totalValue.IsZero() {
		uc := moneyFromEvent(event.UnitCost, payload, "unit_cost")
		totalValue = uc.MulQty(qty)
	}
	now := time.Now().UTC()
	const q = `
		UPDATE "InventoryProjection"
		SET available_stock = GREATEST(0, available_stock - $1),
		    expired_stock = expired_stock + $1,
		    total_value = GREATEST(0, total_value - $2::numeric),
		    days_to_expiry_min = 0,
		    updated_at = $3
		WHERE shop_id = $4 AND item_id = $5
	`
	_, err := tx.Exec(ctx, q, qty, totalValue.String(), now, shopID, *itemID)
	if err != nil {
		return err
	}
	// Nest also upserts WasteProjection for expired
	_, _ = tx.Exec(ctx, `
		INSERT INTO "WasteProjection" (id, shop_id, item_id, total_wasted, total_value, last_wasted_at, updated_at)
		VALUES ($1, $2, $3, $4, $5::numeric, $6, $6)
		ON CONFLICT (shop_id, item_id) DO UPDATE SET
			total_wasted = "WasteProjection".total_wasted + $4,
			total_value = "WasteProjection".total_value + $5::numeric,
			last_wasted_at = $6,
			updated_at = $6`,
		uuid.New().String(), shopID, *itemID, qty, totalValue.String(), now)
	return nil
}

func (e *Engine) handleAdjustment(ctx context.Context, tx pgx.Tx, event *eventstore.Event, payload map[string]interface{}, shopID string) error {
	itemID := firstNonEmpty(event.ItemID, strPtr(asString(payload["item_id"], "")))
	if itemID == nil || *itemID == "" {
		return nil
	}
	// delta can be positive or negative
	delta := asFloat(event.Quantity, asFloat(payload["quantity"], asFloat(payload["adjustment"], 0)))
	const q = `
		UPDATE "InventoryProjection"
		SET available_stock = GREATEST(0, available_stock + $1),
		    updated_at = $2
		WHERE shop_id = $3 AND item_id = $4
	`
	_, err := tx.Exec(ctx, q, delta, time.Now().UTC(), shopID, *itemID)
	return err
}

func (e *Engine) handleRefillIssued(ctx context.Context, tx pgx.Tx, event *eventstore.Event, payload map[string]interface{}, shopID string) error {
	// Nest: refill_issued deducts issued_qty from store stock (kitchen draw).
	// Must NOT bump SalesProjection (that was the pos_sale mistake).
	itemID := firstNonEmpty(event.ItemID, strPtr(asString(payload["item_id"], asString(payload["itemId"], ""))))
	issued := asFloat(payload["issued_qty"], asFloat(payload["approved_qty"], asFloat(event.Quantity, asFloat(payload["quantity"], 0))))
	if itemID == nil || *itemID == "" || issued <= 0 {
		return nil
	}
	lineVal := money.Zero()
	if event.TotalCost != nil {
		lineVal = *event.TotalCost
	}
	if lineVal.IsZero() {
		lineVal = moneyFromMap(payload, "total_cost", "totalCost")
	}
	if err := e.deductStock(ctx, tx, shopID, *itemID, issued, lineVal); err != nil {
		// Nest floors at 0 rather than failing the whole refill when over-issued
		// Match soft floor: set to 0 if insufficient
		_, _ = tx.Exec(ctx, `
			UPDATE "InventoryProjection"
			SET available_stock = 0,
			    total_value = 0,
			    updated_at = $1
			WHERE shop_id = $2 AND item_id = $3`, time.Now().UTC(), shopID, *itemID)
		e.emitStock(shopID, *itemID, asString(payload["item_name"], *itemID), 0, nil)
		return nil
	}
	lost := asFloat(payload["lost_weight"], 0)
	if lost > 0 {
		// waste side-effect only; inventory already reflected issued path
		_ = lost
	}
	return nil
}

func (e *Engine) handlePosSale(ctx context.Context, tx pgx.Tx, event *eventstore.Event, payload map[string]interface{}, shopID string) error {
	if !AppliesSaleSideEffects(event.EventType) {
		return nil
	}
	// 1) Order-level stock_deductions ONLY (never also items[] — avoids double deduct).
	raw, ok := payload["stock_deductions"]
	if ok {
		arr, ok := raw.([]interface{})
		if !ok {
			// Sometimes JSON decodes as []map via intermediate — try remarshal
			b, _ := json.Marshal(raw)
			_ = json.Unmarshal(b, &arr)
		}
		for _, entry := range arr {
			m, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			itemID := asString(m["item_id"], asString(m["itemId"], ""))
			qty := asFloat(m["quantity"], 0)
			if itemID == "" || qty <= 0 {
				continue
			}
			unitCost := moneyFromMap(m, "unit_cost", "unitCost")
			lineVal := moneyFromMap(m, "total_cost", "totalCost")
			if lineVal.IsZero() && !unitCost.IsZero() {
				lineVal = unitCost.MulQty(qty)
			}
			if err := e.deductStock(ctx, tx, shopID, itemID, qty, lineVal); err != nil {
				return err
			}
		}
	} else {
		itemID := firstNonEmpty(event.ItemID, strPtr(asString(payload["item_id"], "")))
		qty := asFloat(event.Quantity, asFloat(payload["quantity"], 0))
		if itemID != nil && *itemID != "" && qty > 0 {
			if err := e.deductStock(ctx, tx, shopID, *itemID, qty, money.Zero()); err != nil {
				return err
			}
		}
	}

	// 2) SalesProjection — revenue must move when POS charges
	revenue := asFloat(payload["total_amount"], asFloat(payload["total"], asFloat(payload["revenue"], 0)))
	if revenue <= 0 && event.TotalCost != nil {
		revenue = event.TotalCost.Float64()
	}
	period := event.CreatedAt.UTC().Format("2006-01-02")
	if true {
		// Always bump order count when pos_sale is recorded
		ordersInc := 1
		if err := e.bumpSales(ctx, tx, shopID, period, revenue, ordersInc); err != nil {
			return fmt.Errorf("sales projection: %w", err)
		}
	}
	return nil
}

func (e *Engine) deductStock(ctx context.Context, tx pgx.Tx, shopID, itemID string, qty float64, value money.Money) error {
	now := time.Now().UTC()
	// Nest FEFO: consume open lots before projection qty (expiry-first)
	if _, err := fefo.ConsumeStock(ctx, tx, shopID, itemID, qty); err != nil {
		return fmt.Errorf("fefo consume: %w", err)
	}
	// Prefer live avg_unit_cost for value when caller did not supply line value
	val := value.Float64()
	if val <= 0 {
		var avgStr *string
		_ = tx.QueryRow(ctx, `
			SELECT avg_unit_cost::text FROM "InventoryProjection"
			WHERE shop_id = $1 AND item_id = $2
		`, shopID, itemID).Scan(&avgStr)
		if avgStr != nil {
			avg := asFloat(*avgStr, 0)
			val = costing.RoundMoney(qty * avg)
		}
	}
	const q = `
		UPDATE "InventoryProjection"
		SET available_stock = available_stock - $1,
		    total_value = GREATEST(0, total_value - $2::numeric),
		    updated_at = $3
		WHERE shop_id = $4 AND item_id = $5
		  AND available_stock >= $1
	`
	tag, err := tx.Exec(ctx, q, qty, fmt.Sprintf("%.4f", val), now, shopID, itemID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("insufficient stock for %s under concurrent sale (need %.4f)", itemID, qty)
	}
	var available float64
	_ = tx.QueryRow(ctx, `
		SELECT COALESCE(available_stock,0) FROM "InventoryProjection"
		WHERE shop_id = $1 AND item_id = $2`, shopID, itemID).Scan(&available)
	e.emitStock(shopID, itemID, itemID, available, nil)
	return nil
}

func (e *Engine) bumpSales(ctx context.Context, tx pgx.Tx, shopID, period string, revenue float64, orders int) error {
	const q = `
		INSERT INTO "SalesProjection" (
			id, shop_id, period, total_sales, total_orders, avg_order_value, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			CASE WHEN $5 > 0 THEN $4 / $5::float ELSE 0 END,
			$6
		)
		ON CONFLICT (shop_id, period) DO UPDATE SET
			total_sales = "SalesProjection".total_sales + EXCLUDED.total_sales,
			total_orders = "SalesProjection".total_orders + EXCLUDED.total_orders,
			avg_order_value = CASE
				WHEN ("SalesProjection".total_orders + EXCLUDED.total_orders) > 0
				THEN ("SalesProjection".total_sales + EXCLUDED.total_sales)
				     / ("SalesProjection".total_orders + EXCLUDED.total_orders)::float
				ELSE 0
			END,
			updated_at = EXCLUDED.updated_at
	`
	_, err := tx.Exec(ctx, q, uuid.New().String(), shopID, period, revenue, orders, time.Now().UTC())
	return err
}

func (e *Engine) handleClosingStockCount(ctx context.Context, tx pgx.Tx, event *eventstore.Event, payload map[string]interface{}, shopID string) error {
	resolvedShop := asString(payload["shop_id"], shopID)
	if resolvedShop == "" {
		resolvedShop = shopID
	}
	now := time.Now().UTC()

	applyOne := func(itemID string, countedQty, unitCost float64) error {
		if itemID == "" {
			return nil
		}
		newValue := 0.0
		if unitCost > 0 && countedQty > 0 {
			newValue = countedQty * unitCost
		}
		const q = `
			INSERT INTO "InventoryProjection" (
				id, shop_id, item_id, available_stock, total_value,
				expired_stock, damaged_stock, last_counted_at, last_counted_qty, updated_at
			) VALUES ($1, $2, $3, $4, $5, 0, 0, $6, $4, $6)
			ON CONFLICT (shop_id, item_id) DO UPDATE SET
				available_stock = EXCLUDED.available_stock,
				total_value = CASE WHEN $5 > 0 THEN EXCLUDED.total_value ELSE "InventoryProjection".total_value END,
				last_counted_at = EXCLUDED.last_counted_at,
				last_counted_qty = EXCLUDED.last_counted_qty,
				updated_at = EXCLUDED.updated_at
		`
		_, err := tx.Exec(ctx, q, uuid.New().String(), resolvedShop, itemID, countedQty, newValue, now)
		return err
	}

	// Nest multi-line payload.lines
	if raw, ok := payload["lines"].([]interface{}); ok && len(raw) > 0 {
		for _, entry := range raw {
			m, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			itemID := asString(m["item_id"], asString(m["itemId"], ""))
			counted := asFloat(m["counted_qty"], asFloat(m["counted_quantity"], asFloat(m["countedQuantity"], 0)))
			uc := asFloat(m["unit_cost"], asFloat(m["unitCost"], 0))
			if err := applyOne(itemID, counted, uc); err != nil {
				return err
			}
		}
		return nil
	}

	// Legacy single-item
	itemID := firstNonEmpty(event.ItemID, strPtr(asString(payload["item_id"], "")))
	if itemID == nil || *itemID == "" {
		return nil
	}
	actual := asFloat(event.Quantity, asFloat(payload["actual_qty"], asFloat(payload["quantity"], 0)))
	uc := asFloat(payload["unit_cost"], 0)
	return applyOne(*itemID, actual, uc)
}

func (e *Engine) handleProductionFinished(ctx context.Context, tx pgx.Tx, event *eventstore.Event, payload map[string]interface{}, shopID string) error {
	// 1) Deduct input ingredients at live avg (or line unit_cost)
	if raw, ok := payload["inputs"].([]interface{}); ok {
		for _, entry := range raw {
			m, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			itemID := asString(m["item_id"], asString(m["itemId"], ""))
			qty := asFloat(m["actual_quantity"], asFloat(m["quantity"], 0))
			if itemID == "" || qty <= 0 {
				continue
			}
			uc := moneyFromMap(m, "unit_cost", "unitCost")
			lineVal := uc.MulQty(qty)
			if err := e.deductStock(ctx, tx, shopID, itemID, qty, lineVal); err != nil {
				return err
			}
		}
	} else if raw, ok := payload["stock_deductions"].([]interface{}); ok {
		for _, entry := range raw {
			m, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			itemID := asString(m["item_id"], "")
			qty := asFloat(m["quantity"], 0)
			if itemID == "" || qty <= 0 {
				continue
			}
			uc := moneyFromMap(m, "unit_cost")
			if err := e.deductStock(ctx, tx, shopID, itemID, qty, uc.MulQty(qty)); err != nil {
				return err
			}
		}
	}

	// 2) Outputs: WAC write FG avg from unit_cost / cost_per_unit
	if raw, ok := payload["outputs"].([]interface{}); ok && len(raw) > 0 {
		for _, entry := range raw {
			m, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			itemID := asString(m["item_id"], asString(m["itemId"], ""))
			qty := asFloat(m["usable_quantity"], asFloat(m["actual_quantity"], asFloat(m["quantity"], 0)))
			if itemID == "" || qty <= 0 {
				continue
			}
			uc := moneyFromMap(m, "unit_cost", "unitCost")
			if uc.IsZero() {
				uc = moneyFromMap(payload, "cost_per_unit", "unit_cost", "output_unit_cost")
			}
			sub := map[string]interface{}{}
			for k, v := range payload {
				sub[k] = v
			}
			sub["item_id"] = itemID
			sub["quantity"] = qty
			sub["unit_cost"] = uc.Float64()
			sub["total_cost"] = uc.MulQty(qty).Float64()
			sub["category"] = "Finished Goods"
			ev := *event
			ev.ItemID = &itemID
			q := qty
			ev.Quantity = &q
			if err := e.handleStockIncrease(ctx, tx, &ev, sub, shopID); err != nil {
				return err
			}
		}
		return nil
	}

	// Single-output fallback (event-level item + cost_per_unit)
	return e.handleStockIncrease(ctx, tx, event, payload, shopID)
}

// maybeSeedReorderPoint mirrors Nest: suggested = max(0.001, round(0.20 * approved, 3))
// Only overwrites unset (0) or legacy default 10.
func maybeSeedReorderPoint(ctx context.Context, tx pgx.Tx, itemID string, approvedQty float64, payload map[string]interface{}) error {
	explicit := asFloat(payload["reorder_point"], asFloat(payload["reorderPoint"], 0))
	suggested := explicit
	if suggested <= 0 {
		if approvedQty <= 0 {
			return nil
		}
		raw := approvedQty * 0.20
		suggested = math.Round(raw*1000) / 1000
		if suggested < 0.001 {
			suggested = 0.001
		}
	}
	var current *float64
	_ = tx.QueryRow(ctx, `SELECT reorder_point FROM "Item" WHERE item_id = $1`, itemID).Scan(&current)
	cur := 0.0
	if current != nil {
		cur = *current
	}
	// Only seed if unset or legacy global default 10
	if cur > 0 && cur != 10 {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE "Item" SET reorder_point = $2, updated_at = $3 WHERE item_id = $1`,
		itemID, suggested, time.Now().UTC())
	return err
}

func ensureItemTx(ctx context.Context, tx pgx.Tx, itemID string, payload map[string]interface{}) error {
	name := asString(payload["item_name"], asString(payload["itemName"], itemID))
	unit := asString(payload["units"], asString(payload["unit"], "unit"))
	category := asString(payload["category"], "")
	const q = `
		INSERT INTO "Item" (id, item_id, name, unit, category, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $6)
		ON CONFLICT (item_id) DO NOTHING
	`
	_, err := tx.Exec(ctx, q, uuid.New().String(), itemID, name, unit, category, time.Now().UTC())
	return err
}

// --- helpers ---

func asString(v interface{}, fallback string) string {
	if v == nil {
		return fallback
	}
	switch x := v.(type) {
	case string:
		if x != "" {
			return x
		}
	case float64:
		return fmt.Sprintf("%.0f", x)
	}
	return fallback
}

func asFloat(v interface{}, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		var f float64
		fmt.Sscanf(x, "%f", &f)
		return f
	}
	return fallback
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func firstNonEmpty(ptrs ...*string) *string {
	for _, p := range ptrs {
		if p != nil && *p != "" {
			return p
		}
	}
	return nil
}

func moneyFromEvent(m *money.Money, payload map[string]interface{}, keys ...string) money.Money {
	if m != nil {
		return *m
	}
	return moneyFromMap(payload, keys...)
}

func moneyFromMap(m map[string]interface{}, keys ...string) money.Money {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch x := v.(type) {
			case float64:
				return money.FromFloat(x)
			case string:
				if mm, err := money.New(x); err == nil {
					return mm
				}
			}
		}
	}
	return money.Zero()
}
