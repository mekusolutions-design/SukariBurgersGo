package kitchen

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/internal/eventstore"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

type Service struct {
	pool       *pgxpool.Pool
	eventStore *eventstore.Store
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store) *Service {
	return &Service{pool: pool, eventStore: es}
}

// Order matches Nest KitchenOrder shape for web/mobile kitchen board.
type Order struct {
	OrderID       string                   `json:"order_id"`
	OrderId       string                   `json:"orderId"` // camel alias
	Items         []map[string]interface{} `json:"items"`
	OrderType     string                   `json:"order_type"`
	TableNumber   interface{}              `json:"table_number"`
	CustomerName  interface{}              `json:"customer_name"`
	Status        string                   `json:"status"`
	KitchenStatus string                   `json:"kitchen_status"`
	TotalAmount   float64                  `json:"total_amount"`
	PaymentMethod interface{}              `json:"payment_method"`
	PaymentStatus interface{}              `json:"payment_status"`
	Timestamp     time.Time                `json:"timestamp"`
	ShopID        string                   `json:"shop_id"`
}

// ActiveOrders mirrors Nest getActiveOrders: pos_sale + order_sent_to_kitchen + status within 48h.
func (s *Service) ActiveOrders(ctx context.Context, shopID string) ([]Order, error) {
	if shopID == "" {
		shopID = "1"
	}
	since := time.Now().UTC().Add(-48 * time.Hour)
	rows, err := s.pool.Query(ctx, `
		SELECT event_type, created_at, payload
		FROM "Event"
		WHERE shop_id = $1
		  AND event_type IN ('pos_sale','order_status_updated','order_sent_to_kitchen')
		  AND created_at >= $2
		ORDER BY created_at ASC
		LIMIT 500`, shopID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ordersMap := map[string]*Order{}
	for rows.Next() {
		var et string
		var created time.Time
		var payload []byte
		if rows.Scan(&et, &created, &payload) != nil {
			continue
		}
		var p map[string]interface{}
		if json.Unmarshal(payload, &p) != nil {
			continue
		}
		oid := strAny(p["order_id"], p["orderId"])
		if oid == "" {
			continue
		}
		switch et {
		case "pos_sale", "order_sent_to_kitchen":
			// Only seed active board if kitchen-relevant
			ks := strAny(p["kitchen_status"], p["status"])
			if et == "pos_sale" {
				// Nest includes pos_sale when kitchen status is pending/preparing/ready
				// or when send was implicit
				if ks == "" {
					ks = "pending"
				}
			}
			if et == "order_sent_to_kitchen" && ks == "" {
				ks = "pending"
			}
			items := mapKitchenItems(p)
			total := floatAny(p["total_amount"], p["total"], p["revenue"])
			ot := strAny(p["order_type"], p["orderType"])
			if ot == "" {
				ot = "dine_in"
			}
			existing, ok := ordersMap[oid]
			if !ok {
				o := &Order{
					OrderID: oid, OrderId: oid, Items: items, OrderType: ot,
					TableNumber: firstAny(p["table_number"], p["tableNumber"]),
					CustomerName: firstAny(p["customer_name"], p["customerName"]),
					Status: ks, KitchenStatus: ks,
					TotalAmount: total,
					PaymentMethod: firstAny(p["payment_method"], p["paymentMethod"]),
					PaymentStatus: firstAny(p["payment_status"], p["paymentStatus"]),
					Timestamp: created, ShopID: shopID,
				}
				ordersMap[oid] = o
			} else {
				// Merge later events (status from kitchen may refine)
				if ks != "" && ks != "not_sent" {
					existing.Status = ks
					existing.KitchenStatus = ks
				}
				if len(items) > 0 {
					existing.Items = items
				}
				if total > 0 {
					existing.TotalAmount = total
				}
				existing.Timestamp = created
			}
		case "order_status_updated":
			st := strAny(p["status"], p["kitchen_status"])
			if st == "" {
				continue
			}
			if o, ok := ordersMap[oid]; ok {
				o.Status = st
				o.KitchenStatus = st
			} else {
				// Status-only without prior order — still surface if active
				if st == "pending" || st == "preparing" || st == "ready" {
					ordersMap[oid] = &Order{
						OrderID: oid, OrderId: oid, Status: st, KitchenStatus: st,
						Items: mapKitchenItems(p), Timestamp: created, ShopID: shopID,
					}
				}
			}
		}
	}

	out := make([]Order, 0, len(ordersMap))
	for _, o := range ordersMap {
		st := o.Status
		if st == "completed" || st == "cancelled" || st == "served" || st == "not_sent" {
			continue
		}
		if st == "" {
			st = "pending"
			o.Status = st
			o.KitchenStatus = st
		}
		out = append(out, *o)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	return out, nil
}

func mapKitchenItems(p map[string]interface{}) []map[string]interface{} {
	raw, _ := p["items"].([]interface{})
	if len(raw) == 0 {
		raw, _ = p["lines"].([]interface{})
	}
	var items []map[string]interface{}
	for _, it := range raw {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		name := strAny(m["menu_name"], m["name"], m["item_name"])
		if name == "" {
			name = "Item"
		}
		line := map[string]interface{}{
			"menu_name": name,
			"quantity":  floatAny(m["quantity"], m["qty"]),
			"notes":     m["notes"],
			"line_type": firstAny(m["line_type"], m["lineType"]),
			"menu_id":   firstAny(m["menu_id"], m["combo_id"], m["comboId"]),
			"combo_id":  firstAny(m["combo_id"], m["comboId"]),
		}
		// selections for combo display
		sels := extractSelections(m)
		if len(sels) > 0 {
			line["selections"] = sels
			line["resolved_menu_items"] = sels
		}
		items = append(items, line)
	}
	return items
}

func extractSelections(m map[string]interface{}) []map[string]interface{} {
	var sels []map[string]interface{}
	for _, key := range []string{"resolved_menu_items", "resolvedMenuItems"} {
		arr, _ := m[key].([]interface{})
		for _, r := range arr {
			rm, _ := r.(map[string]interface{})
			if rm == nil {
				continue
			}
			sels = append(sels, map[string]interface{}{
				"group_index":    floatAny(rm["group_index"], rm["groupIndex"]),
				"group_name":     firstAny(rm["group_name"], rm["menuCategoryName"], rm["category_name"]),
				"menu_item_id":   strAny(rm["menu_item_id"], rm["menuItemId"], rm["id"]),
				"menu_item_name": strAny(rm["name"], rm["menu_item_name"], rm["menuItemName"]),
				"quantity":       qtyOr1(rm["quantity"], rm["qty"]),
			})
		}
	}
	if len(sels) > 0 {
		return sels
	}
	// stock_deductions fallback
	arr, _ := m["stock_deductions"].([]interface{})
	for _, d := range arr {
		dm, _ := d.(map[string]interface{})
		if dm == nil {
			continue
		}
		nm := strAny(dm["item_name"], dm["itemName"], dm["item_id"])
		if nm == "" {
			continue
		}
		sels = append(sels, map[string]interface{}{
			"group_index": 0, "menu_item_id": strAny(dm["item_id"], dm["itemId"], nm),
			"menu_item_name": nm, "quantity": qtyOr1(dm["quantity"], dm["qty"]),
		})
	}
	return sels
}

func (s *Service) UpdateOrderStatus(ctx context.Context, shopID, orderID, status, actorUserID string) (map[string]interface{}, error) {
	if orderID == "" {
		return nil, apperrors.BadRequest("order_id required")
	}
	allowed := map[string]bool{"pending": true, "preparing": true, "ready": true, "completed": true, "cancelled": true, "served": true}
	if !allowed[status] {
		return nil, apperrors.BadRequest("invalid kitchen status")
	}
	if shopID == "" {
		shopID = "1"
	}
	var actorPtr *string
	if actorUserID != "" {
		actorPtr = &actorUserID
	}
	_, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType:      eventstore.EventOrderStatusUpdated,
		ActorUserID:    actorPtr,
		IdempotencyKey: fmt.Sprintf("kitchen-status-%s-%s-%d", orderID, status, time.Now().UnixNano()),
		Payload: map[string]interface{}{
			"order_id": orderID, "orderId": orderID,
			"status": status, "kitchen_status": status,
			"shop_id": shopID,
		},
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success": true, "order_id": orderID, "orderId": orderID,
		"status": status, "kitchen_status": status,
	}, nil
}

func strAny(vals ...interface{}) string {
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func floatAny(vals ...interface{}) float64 {
	for _, v := range vals {
		switch t := v.(type) {
		case float64:
			return t
		case float32:
			return float64(t)
		case int:
			return float64(t)
		case int64:
			return float64(t)
		}
	}
	return 0
}

func firstAny(vals ...interface{}) interface{} {
	for _, v := range vals {
		if v != nil {
			if s, ok := v.(string); ok && s == "" {
				continue
			}
			return v
		}
	}
	return nil
}

func qtyOr1(vals ...interface{}) float64 {
	for _, v := range vals {
		switch t := v.(type) {
		case float64:
			if t > 0 {
				return t
			}
		case float32:
			if t > 0 {
				return float64(t)
			}
		case int:
			if t > 0 {
				return float64(t)
			}
		case int64:
			if t > 0 {
				return float64(t)
			}
		}
	}
	return 1
}
