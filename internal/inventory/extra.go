package inventory

import (
	"context"
	"time"

	"github.com/restflow/api-go/pkg/money"
)

// Canonical categories — same list as Nest INGREDIENT_CATEGORIES / inventory-categories.
var CanonicalCategories = []string{
	"Meat & Poultry",
	"Fish & Seafood",
	"Dairy & Eggs",
	"Fruits & Vegetables",
	"Grains, Cereals & Pulses",
	"Baking & Bakery",
	"Nuts, Seeds & Alternatives",
	"Herbs, Spices & Seasonings",
	"Oils, Sauces & Condiments",
	"Sweeteners & Food Ingredients",
	"Beverages",
	"Alcohol & Bar",
	"Frozen & Prepared Foods",
	"Packaging & Consumables",
	"Cleaning, Service & Equipment",
}

// Alerts matches Nest getAlerts: low stock + not finished + not written-off expired.
func (s *Service) Alerts(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.fetchAll(ctx, shopID, "", "", false)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0)
	for _, row := range rows {
		if isFinishedCategory(row.Category) || isWrittenOffExpired(row) {
			continue
		}
		thresh := reorderOf(row)
		stock := row.AvailableStock
		if stock > thresh {
			continue
		}
		sev := "low"
		critFloor := thresh * 0.3
		if critFloor < 1 {
			critFloor = 1
		}
		if stock <= critFloor {
			sev = "critical"
		}
		out = append(out, map[string]interface{}{
			"item_id":       row.ItemID,
			"itemId":        row.ItemID,
			"item_name":     row.Name,
			"itemName":      row.Name,
			"current_stock": stock,
			"currentStock":  stock,
			"threshold":     thresh,
			"shop_id":       row.ShopID,
			"shopId":        row.ShopID,
			"severity":      sev,
			"total_value":   row.TotalValue,
			"totalValue":    row.TotalValue,
		})
	}
	return out, nil
}

// Categories returns Nest-shaped canonical list (not distinct DB values).
func (s *Service) Categories(ctx context.Context, shopID string) (map[string]interface{}, error) {
	items := make([]string, len(CanonicalCategories))
	copy(items, CanonicalCategories)
	return map[string]interface{}{
		"success": true,
		"items":   items,
	}, nil
}

type BatchRow struct {
	BatchNumber  string     `json:"batchNumber"`
	ItemID       string     `json:"itemId"`
	Quantity     float64    `json:"quantity"`
	UnitCost     float64    `json:"unitCost"`
	ExpiryDate   *time.Time `json:"expiryDate"`
	ReceivedAt   time.Time  `json:"receivedAt"`
	SupplierID   *string    `json:"supplierId,omitempty"`
	BatchNumberS string     `json:"batch_number"`
	QtyRemaining float64    `json:"qty_remaining"`
}

// Batches for GET /inventory/{itemId}/batches — InventoryBatch FEFO order.
func (s *Service) Batches(ctx context.Context, shopID, itemID string) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT batch_number, item_id, qty_remaining, unit_cost::text,
		       expiry_date, received_at, supplier_id
		FROM "InventoryBatch"
		WHERE shop_id = $1 AND item_id = $2 AND qty_remaining > 0
		ORDER BY expiry_date ASC NULLS LAST, received_at ASC
		LIMIT 200`, shopID, itemID)
	if err != nil {
		// Fallback: events with batch_number (Nest kitchen-style)
		return s.batchesFromEvents(ctx, shopID, itemID)
	}
	defer rows.Close()
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		var bn, iid, uc string
		var qty float64
		var exp, recv *time.Time
		var sup *string
		if rows.Scan(&bn, &iid, &qty, &uc, &exp, &recv, &sup) != nil {
			continue
		}
		cost := 0.0
		if m, e := money.New(uc); e == nil {
			cost = m.Float64()
		}
		recvAt := time.Time{}
		if recv != nil {
			recvAt = *recv
		}
		var expStr interface{}
		if exp != nil {
			expStr = exp.UTC().Format(time.RFC3339)
		}
		out = append(out, map[string]interface{}{
			"batchNumber": bn, "batch_number": bn,
			"itemId": iid, "item_id": iid,
			"quantity": qty, "qty_remaining": qty,
			"unitCost": cost, "unit_cost": cost,
			"expiryDate": expStr, "expiry_date": expStr,
			"receivedAt": recvAt.UTC().Format(time.RFC3339), "received_at": recvAt.UTC().Format(time.RFC3339),
			"supplierId": sup, "supplier_id": sup,
		})
	}
	if len(out) == 0 {
		return s.batchesFromEvents(ctx, shopID, itemID)
	}
	return out, nil
}

func (s *Service) batchesFromEvents(ctx context.Context, shopID, itemID string) ([]map[string]interface{}, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(batch_number,''), expiry_date, COALESCE(quantity,0), created_at, event_type,
		       COALESCE(payload::text, '{}')
		FROM "Event"
		WHERE (item_id = $1 OR payload->>'item_id' = $1)
		  AND ($2 = '' OR shop_id = $2)
		  AND batch_number IS NOT NULL AND batch_number <> ''
		ORDER BY created_at ASC
		LIMIT 100`, itemID, shopID)
	if err != nil {
		return []map[string]interface{}{}, nil
	}
	defer rows.Close()
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		var bn, et, payload string
		var qty float64
		var exp, created *time.Time
		if rows.Scan(&bn, &exp, &qty, &created, &et, &payload) != nil {
			continue
		}
		var expStr interface{}
		if exp != nil {
			expStr = exp.UTC().Format(time.RFC3339)
		}
		createdStr := ""
		if created != nil {
			createdStr = created.UTC().Format(time.RFC3339)
		}
		out = append(out, map[string]interface{}{
			"batchNumber": bn, "batch_number": bn,
			"itemId": itemID, "item_id": itemID,
			"quantity": qty,
			"expiryDate": expStr, "expiry_date": expStr,
			"receivedAt": createdStr, "created_at": createdStr,
			"event_type": et,
		})
	}
	return out, nil
}

// RowToAPI adds camelCase aliases for Nest/web consumers.
func RowToAPI(r StockRow) map[string]interface{} {
	var days interface{}
	if r.DaysToExpiryMin != nil {
		days = *r.DaysToExpiryMin
	}
	var nextExp interface{}
	if r.NextExpiryDate != nil {
		nextExp = r.NextExpiryDate.UTC().Format(time.RFC3339)
	}
	var updated interface{}
	if r.UpdatedAt != nil {
		updated = r.UpdatedAt.UTC().Format(time.RFC3339)
	}
	var lastRecv interface{}
	if r.LastReceivedAt != nil {
		lastRecv = r.LastReceivedAt.UTC().Format(time.RFC3339)
	}
	return map[string]interface{}{
		"id": r.ID,
		"item_id": r.ItemID, "itemId": r.ItemID,
		"name": r.Name,
		"unit": r.Unit,
		"category": r.Category,
		"reorder_point": r.ReorderPoint, "reorderPoint": r.ReorderPoint,
		"min_stock": r.MinStock, "minStock": r.MinStock,
		"available_stock": r.AvailableStock, "availableStock": r.AvailableStock,
		"expired_stock": r.ExpiredStock, "expiredStock": r.ExpiredStock,
		"damaged_stock": r.DamagedStock, "damagedStock": r.DamagedStock,
		"total_value": r.TotalValue, "totalValue": r.TotalValue,
		"days_to_expiry_min": days, "daysToExpiryMin": days,
		"next_expiry_date": nextExp, "nextExpiryDate": nextExp,
		"last_received_at": lastRecv, "lastReceivedAt": lastRecv,
		"shop_id": r.ShopID, "shopId": r.ShopID,
		"updated_at": updated, "updatedAt": updated,
	}
}
