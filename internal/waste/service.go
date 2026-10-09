package waste

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/costing"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
)

type Service struct {
	pool       *pgxpool.Pool
	eventStore *eventstore.Store
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store) *Service {
	return &Service{pool: pool, eventStore: es}
}

type RecordInput struct {
	ShopID, ActorUserID, ItemID, ItemName, BatchNumber, Unit, Reason, PhotoURL, IdempotencyKey string
	Quantity, UnitCost                                                                         float64
}

func (s *Service) Record(ctx context.Context, in RecordInput) (map[string]interface{}, error) {
	if in.ItemID == "" || in.Quantity <= 0 {
		return nil, apperrors.BadRequest("item_id and positive quantity required")
	}
	if in.ShopID == "" {
		return nil, apperrors.Forbidden("shop identity required")
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = uuid.New().String()
	}
	// Nest: resolve name/unit from catalog when missing
	if in.ItemName == "" || in.Unit == "" {
		var name, unit string
		_ = s.pool.QueryRow(ctx, `SELECT COALESCE(name,''), COALESCE(unit,'pcs') FROM "Item" WHERE item_id = $1`, in.ItemID).Scan(&name, &unit)
		if in.ItemName == "" {
			in.ItemName = name
		}
		if in.Unit == "" {
			in.Unit = unit
		}
	}
	if in.ItemName == "" {
		in.ItemName = in.ItemID
	}
	if in.Unit == "" {
		in.Unit = "pcs"
	}
	// Nest: value = qty × avg unit cost when client omits unit_cost
	if in.UnitCost <= 0 {
		var avgStr *string
		_ = s.pool.QueryRow(ctx, `
			SELECT avg_unit_cost::text FROM "InventoryProjection"
			WHERE shop_id = $1 AND item_id = $2`, in.ShopID, in.ItemID).Scan(&avgStr)
		if avgStr != nil && *avgStr != "" {
			if m, err := money.New(*avgStr); err == nil {
				in.UnitCost = m.Float64()
			}
		}
	}
	uc := money.FromFloatUnitCost(in.UnitCost)
	total := costing.LineTotal(uc, in.Quantity)
	qty := in.Quantity
	var actorPtr *string
	if in.ActorUserID != "" {
		actorPtr = &in.ActorUserID
	}
	var reason, photo *string
	if in.Reason != "" {
		reason = &in.Reason
	}
	if in.PhotoURL != "" {
		photo = &in.PhotoURL
	}
	wasteID := "WASTE-" + in.ItemID + "-" + uuid.New().String()[:8]
	payload := map[string]interface{}{
		"waste_id": wasteID, "wasteId": wasteID,
		"item_id": in.ItemID, "itemId": in.ItemID,
		"item_name": in.ItemName, "itemName": in.ItemName,
		"quantity_wasted": in.Quantity, "quantity": in.Quantity,
		"unit_of_measure": in.Unit, "unit": in.Unit,
		"unit_cost": uc.Float64(), "total_waste_value": total.Float64(),
		"total_cost":   total.Float64(),
		"waste_reason": in.Reason, "batch_number": in.BatchNumber,
		"shop_id": in.ShopID, "shopId": in.ShopID,
		"recorded_at": time.Now().UTC().Format(time.RFC3339),
	}
	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventWasteRecorded, ActorUserID: actorPtr, IdempotencyKey: in.IdempotencyKey,
		ItemID: &in.ItemID, Quantity: &qty, UnitCost: &uc, TotalCost: &total,
		WasteReason: reason, WastePhotoURL: photo, Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "eventId": event.ID}, nil
}

func (s *Service) Analytics(ctx context.Context, shopID string, from, to *time.Time) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if to == nil {
		now := time.Now().UTC()
		to = &now
	}
	if from == nil {
		f := to.AddDate(0, 0, -30)
		from = &f
	}
	// Prefer Event history so names/causes match Nest analytics for the web UI.
	rows, err := s.pool.Query(ctx, `
		SELECT e.item_id, e.quantity, e.total_cost::text, e.waste_reason, e.payload, e.created_at,
		       i.name, i.unit
		FROM "Event" e
		LEFT JOIN "Item" i ON i.item_id = e.item_id
		WHERE e.shop_id = $1
		  AND e.event_type IN ('waste_recorded', 'WASTE_RECORDED', 'waste')
		  AND e.created_at >= $2 AND e.created_at <= $3
		ORDER BY e.created_at DESC
		LIMIT 5000`, shopID, *from, *to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type itemAgg struct {
		itemID, name, unit string
		qty, value         float64
		count              int
	}
	byItem := map[string]*itemAgg{}
	byCauseValue := map[string]float64{}
	byCauseQty := map[string]float64{}
	totalValue := 0.0
	totalQty := 0.0
	incidents := 0

	for rows.Next() {
		var itemID, reason *string
		var qty *float64
		var totalCostStr *string
		var payload []byte
		var created interface{}
		var name, unit *string
		if err := rows.Scan(&itemID, &qty, &totalCostStr, &reason, &payload, &created, &name, &unit); err != nil {
			continue
		}
		var p map[string]interface{}
		_ = json.Unmarshal(payload, &p)
		if p == nil {
			p = map[string]interface{}{}
		}
		id := ""
		if itemID != nil {
			id = *itemID
		}
		if id == "" {
			if v, ok := p["item_id"].(string); ok {
				id = v
			}
		}
		if id == "" {
			continue
		}
		q := 0.0
		if qty != nil {
			q = *qty
		}
		if q == 0 {
			if v, ok := p["quantity_wasted"].(float64); ok {
				q = v
			} else if v, ok := p["quantity"].(float64); ok {
				q = v
			}
		}
		val := 0.0
		if totalCostStr != nil {
			if m, e := money.New(*totalCostStr); e == nil {
				val = m.Float64()
			}
		}
		if val == 0 {
			if v, ok := p["total_waste_value"].(float64); ok {
				val = v
			}
		}
		n := id
		if name != nil && *name != "" {
			n = *name
		} else if v, ok := p["item_name"].(string); ok && v != "" {
			n = v
		}
		u := "pcs"
		if unit != nil && *unit != "" {
			u = *unit
		} else if v, ok := p["unit_of_measure"].(string); ok && v != "" {
			u = v
		} else if v, ok := p["unit"].(string); ok && v != "" {
			u = v
		}
		cause := "other"
		if reason != nil && *reason != "" {
			cause = mapWasteCause(*reason)
		} else if v, ok := p["waste_reason"].(string); ok {
			cause = mapWasteCause(v)
		}

		agg, ok := byItem[id]
		if !ok {
			agg = &itemAgg{itemID: id, name: n, unit: u}
			byItem[id] = agg
		}
		agg.qty += q
		agg.value += val
		agg.count++
		if (agg.name == "" || agg.name == id) && n != id {
			agg.name = n
		}
		byCauseValue[cause] += val
		byCauseQty[cause] += q
		totalValue += val
		totalQty += q
		incidents++
	}

	topItems := make([]map[string]interface{}, 0, len(byItem))
	for _, a := range byItem {
		topItems = append(topItems, map[string]interface{}{
			"itemId": a.itemID, "name": a.name, "unit": a.unit,
			"quantity": a.qty, "value": a.value, "incidentCount": a.count,
		})
	}
	// sort by value desc, top 10
	for i := 0; i < len(topItems); i++ {
		for j := i + 1; j < len(topItems); j++ {
			vi, _ := topItems[i]["value"].(float64)
			vj, _ := topItems[j]["value"].(float64)
			if vj > vi {
				topItems[i], topItems[j] = topItems[j], topItems[i]
			}
		}
	}
	if len(topItems) > 10 {
		topItems = topItems[:10]
	}

	byCause := make([]map[string]interface{}, 0, len(byCauseValue))
	for c, v := range byCauseValue {
		byCause = append(byCause, map[string]interface{}{
			"cause": c, "value": v, "quantity": byCauseQty[c],
		})
	}

	return map[string]interface{}{
		"shopId":                  shopID,
		"totalWasteValue":         totalValue,
		"totalWasteQuantity":      totalQty,
		"totalWastedValue":        totalValue,
		"totalWastedQuantity":     totalQty,
		"wasteIncidents":          incidents,
		"wastePercentOfPurchases": nil,
		"byCause":                 byCause,
		"topItems":                topItems,
		"total_waste_value":       totalValue,
		"total_waste_quantity":    totalQty,
		"top_waste_items":         topItems,
		"by_waste_type":           byCauseValue,
	}, nil
}

func mapWasteCause(raw string) string {
	r := strings.ToLower(raw)
	switch {
	case strings.Contains(r, "expir"):
		return "expired"
	case strings.Contains(r, "spoil") || strings.Contains(r, "rot"):
		return "spoiled"
	case strings.Contains(r, "prep") || strings.Contains(r, "cook"):
		return "prep_error"
	case strings.Contains(r, "return") || strings.Contains(r, "customer"):
		return "customer_return"
	case strings.Contains(r, "over"):
		return "overproduction"
	default:
		return "other"
	}
}
