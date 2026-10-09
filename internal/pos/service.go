package pos

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/menu"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
)

type Service struct {
	pool       *pgxpool.Pool
	eventStore *eventstore.Store
	resolve    *menu.ResolutionService
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store, resolve *menu.ResolutionService) *Service {
	return &Service{pool: pool, eventStore: es, resolve: resolve}
}

type OrderLineInput struct {
	LineType        string                     `json:"line_type"` // menu_item | combo
	MenuItemID      string                     `json:"menu_item_id"`
	ComboID         string                     `json:"combo_id"`
	Name            string                     `json:"name"`
	Quantity        float64                    `json:"quantity"`
	UnitPrice       float64                    `json:"unit_price"`
	ComboSelections []menu.ComboSelectionInput `json:"combo_selections"`
	// Client stock_deductions are IGNORED — server resolves only
}

type PaymentSplit struct {
	Method string  `json:"method"`
	Amount float64 `json:"amount"`
}

type CreateOrderInput struct {
	ShopID               string
	ActorUserID          string
	OrderType            string // dine_in, takeaway, delivery
	TableNumber          string
	CustomerName         string
	CustomerPhone        string
	DeliveryAddress      string
	Items                []OrderLineInput
	TotalAmount          float64
	TaxAmount            float64
	DiscountAmount       float64
	PaymentMethod        string // cash, mpesa, card, split
	PaymentSplits        []PaymentSplit
	MpesaReference       string
	PaymentStatus        string // paid, pending, failed, unpaid
	Notes                string
	ClientIdempotencyKey string
}

type CreateOrderResult struct {
	Success         bool                  `json:"success"`
	OrderID         string                `json:"orderId"`
	EventID         string                `json:"eventId"`
	StockDeductions []menu.StockDeduction `json:"stock_deductions,omitempty"`
	Message         string                `json:"message,omitempty"`
}

func (s *Service) CreateOrder(ctx context.Context, in CreateOrderInput) (*CreateOrderResult, error) {
	shopID := in.ShopID
	if shopID == "" {
		return nil, apperrors.Forbidden("shop identity required")
	}
	if len(in.Items) == 0 {
		return nil, apperrors.BadRequest("At least one item is required")
	}
	// Client total_amount is ignored for financial records; server computes from lines.

	orderID := fmt.Sprintf("ORD-%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:16])
	idemKey := fmt.Sprintf("pos-order-%s", orderID)
	if len(in.ClientIdempotencyKey) >= 8 {
		idemKey = fmt.Sprintf("pos-order-client-%s", strings.TrimSpace(in.ClientIdempotencyKey))
	}

	// Idempotency fast-path
	existing, err := s.eventStore.FindByIdempotencyKey(ctx, idemKey)
	if err == nil && existing != nil {
		return &CreateOrderResult{
			Success: true,
			OrderID: orderIDFromPayload(existing),
			EventID: existing.ID,
			Message: "Already processed (idempotent replay)",
		}, nil
	}

	paymentMethod := in.PaymentMethod
	if paymentMethod == "" {
		paymentMethod = "cash"
	}
	paymentStatus := in.PaymentStatus
	if paymentStatus == "" {
		paymentStatus = "paid"
	}

	orderType := strings.ReplaceAll(in.OrderType, "-", "_")
	if orderType == "" {
		orderType = "dine_in"
	}

	// Server-side resolution — never trust client deductions
	type enrichedLine struct {
		LineType        string                     `json:"line_type"`
		MenuItemID      string                     `json:"menu_item_id,omitempty"`
		ComboID         string                     `json:"combo_id,omitempty"`
		Name            string                     `json:"name"`
		Quantity        float64                    `json:"quantity"`
		UnitPrice       float64                    `json:"unit_price"`
		LineTotal       float64                    `json:"line_total"`
		FoodCost        float64                    `json:"food_cost"`
		StockDeductions []menu.StockDeduction      `json:"stock_deductions"`
		Selections      []menu.ResolvedMenuItem    `json:"selections,omitempty"`
		ComboSelections []menu.ComboSelectionInput `json:"combo_selections,omitempty"`
	}

	var enriched []enrichedLine
	var allDeductions []menu.StockDeduction

	for _, line := range in.Items {
		qty := line.Quantity
		if qty <= 0 {
			return nil, apperrors.BadRequest("line quantity must be positive")
		}
		lineType := line.LineType
		if lineType == "" {
			lineType = "menu_item"
		}

		if lineType == "combo" {
			if line.ComboID == "" {
				return nil, apperrors.BadRequest("combo_id is required for combo lines")
			}
			resolved, err := s.resolve.ResolveComboLine(ctx, shopID, line.ComboID, qty, line.ComboSelections)
			if err != nil {
				return nil, err
			}
			foodCost := 0.0
			for _, d := range resolved.StockDeductions {
				foodCost += money.FromFloatUnitCost(d.UnitCost).MulQty(d.Quantity).Float64()
			}
			// Server-authoritative: Combo.selling_price only
			unitPrice := 0.0
			if qty > 0 {
				unitPrice = resolved.Revenue / qty
			}
			el := enrichedLine{
				LineType: "combo", ComboID: line.ComboID, Name: resolved.Label,
				Quantity: qty, UnitPrice: unitPrice, LineTotal: unitPrice * qty,
				FoodCost: foodCost, StockDeductions: resolved.StockDeductions,
				Selections: resolved.MenuItems, ComboSelections: line.ComboSelections,
			}
			enriched = append(enriched, el)
			allDeductions = append(allDeductions, resolved.StockDeductions...)
			continue
		}

		// menu_item
		menuID := line.MenuItemID
		if menuID == "" {
			return nil, apperrors.BadRequest("menu_item_id is required for menu_item lines")
		}
		resolved, err := s.resolve.ResolveMenuItemLine(ctx, shopID, menuID, qty)
		if err != nil {
			return nil, err
		}
		foodCost := 0.0
		for _, d := range resolved.Deductions {
			foodCost += money.FromFloatUnitCost(d.UnitCost).MulQty(d.Quantity).Float64()
		}
		// Server-authoritative only — never accept client unit_price
		unitPrice := resolved.UnitPrice
		if unitPrice < 0 {
			unitPrice = 0
		}
		name := line.Name
		if name == "" {
			name = resolved.Name
		}
		el := enrichedLine{
			LineType: "menu_item", MenuItemID: menuID, Name: name,
			Quantity: qty, UnitPrice: unitPrice, LineTotal: unitPrice * qty,
			FoodCost: foodCost, StockDeductions: resolved.Deductions,
		}
		enriched = append(enriched, el)
		allDeductions = append(allDeductions, resolved.Deductions...)
	}

	orderLevelDeductions := menu.AggregateDeductions(allDeductions)

	// Server-authoritative order total (ignore client TotalAmount)
	serverTotalAmount := 0.0
	for _, el := range enriched {
		serverTotalAmount += el.LineTotal
	}
	if serverTotalAmount < 0 {
		serverTotalAmount = 0
	}

	// Split payments must match server-authoritative total (not client TotalAmount)
	if paymentMethod == "split" {
		if len(in.PaymentSplits) < 2 {
			return nil, apperrors.BadRequest("Split payment requires at least two payment lines")
		}
		var sum float64
		for _, sp := range in.PaymentSplits {
			sum += sp.Amount
		}
		if math.Abs(sum-serverTotalAmount) > 0.02 {
			return nil, apperrors.BadRequest(fmt.Sprintf(
				"Split amounts (%.2f) must equal server total (%.2f)", sum, serverTotalAmount,
			))
		}
	}

	// Stock availability check (fail closed)
	if err := s.resolve.CheckAvailability(ctx, shopID, orderLevelDeductions); err != nil {
		return nil, err
	}

	totalQty := 0.0
	for _, e := range enriched {
		totalQty += e.Quantity
	}

	totalMoney := money.FromFloat(serverTotalAmount)
	actor := in.ActorUserID
	var actorPtr *string
	if actor != "" {
		actorPtr = &actor
	}

	// Build items for payload (JSON-serializable)
	itemsPayload := make([]map[string]interface{}, 0, len(enriched))
	for _, e := range enriched {
		item := map[string]interface{}{
			"line_type":        e.LineType,
			"name":             e.Name,
			"quantity":         e.Quantity,
			"unit_price":       e.UnitPrice,
			"line_total":       e.LineTotal,
			"food_cost":        e.FoodCost,
			"stock_deductions": e.StockDeductions,
		}
		if e.MenuItemID != "" {
			item["menu_item_id"] = e.MenuItemID
		}
		if e.ComboID != "" {
			item["combo_id"] = e.ComboID
			item["combo_selections"] = e.ComboSelections
			item["selections"] = e.Selections
		}
		itemsPayload = append(itemsPayload, item)
	}

	foodCostTotal := 0.0
	for _, e := range enriched {
		foodCostTotal += e.FoodCost
	}

	payload := map[string]interface{}{
		"order_id":         orderID,
		"order_type":       orderType,
		"table_number":     nullIfEmpty(in.TableNumber),
		"customer_name":    nullIfEmpty(in.CustomerName),
		"customer_phone":   nullIfEmpty(in.CustomerPhone),
		"delivery_address": nullIfEmpty(in.DeliveryAddress),
		"items":            itemsPayload,
		"lines":            itemsPayload,
		"stock_deductions": orderLevelDeductions,
		"total_amount":     serverTotalAmount,
		"total":            serverTotalAmount,
		"revenue":          serverTotalAmount,
		"food_cost":        foodCostTotal,
		"food_cost_total":  foodCostTotal,
		"cogs":             foodCostTotal,
		"tax_amount":       in.TaxAmount,
		"discount_amount":  in.DiscountAmount,
		"payment_method":   paymentMethod,
		"payment_splits":   in.PaymentSplits,
		"payment_status":   paymentStatus,
		"mpesa_reference":  nullIfEmpty(in.MpesaReference),
		"notes":            nullIfEmpty(in.Notes),
		"status":           "not_sent",
		"kitchen_status":   "not_sent",
		"sent_to_kitchen":  false,
		"shop_id":          shopID,
		"source":           "go_pos",
		"created_at":       time.Now().UTC().Format(time.RFC3339),
	}

	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType:      eventstore.EventPosSale,
		ActorUserID:    actorPtr,
		IdempotencyKey: idemKey,
		Quantity:       &totalQty,
		TotalCost:      &totalMoney,
		Payload:        payload,
	})
	if err != nil {
		return nil, err
	}

	return &CreateOrderResult{
		Success:         true,
		OrderID:         orderID,
		EventID:         event.ID,
		StockDeductions: orderLevelDeductions,
		Message:         "Order placed successfully (not yet sent to kitchen)",
	}, nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func orderIDFromPayload(e *eventstore.Event) string {
	if e == nil || len(e.Payload) == 0 {
		return ""
	}
	// lightweight extract
	s := string(e.Payload)
	const key = `"order_id":"`
	i := strings.Index(s, key)
	if i < 0 {
		return ""
	}
	rest := s[i+len(key):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// ListOrders returns recent pos_sale events for a shop.
func (s *Service) ListOrders(ctx context.Context, shopID string, limit, offset int) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// Minimal columns — avoid numeric→string scan issues on unit_cost/total_cost.
	const q = `
		SELECT id::text,
		       COALESCE(event_type, ''),
		       COALESCE(shop_id, ''),
		       COALESCE(payload::text, '{}'),
		       created_at
		FROM "Event"
		WHERE shop_id = $1 AND event_type = $2
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`
	rows, err := s.pool.Query(ctx, q, shopID, eventstore.EventPosSale, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list orders query: %w", err)
	}
	defer rows.Close()

	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, et, sid, payloadStr string
		var createdAt time.Time
		if err := rows.Scan(&id, &et, &sid, &payloadStr, &createdAt); err != nil {
			return nil, fmt.Errorf("list orders scan: %w", err)
		}
		var payload map[string]interface{}
		_ = json.Unmarshal([]byte(payloadStr), &payload)
		if payload == nil {
			payload = map[string]interface{}{}
		}
		orderID, _ := payload["order_id"].(string)
		if orderID == "" {
			orderID = id
		}
		items, _ := payload["items"].([]interface{})
		itemCount := len(items)
		status, _ := payload["status"].(string)
		if status == "" {
			status, _ = payload["kitchen_status"].(string)
		}
		if status == "" {
			status = "not_sent"
		}
		orderType, _ := payload["order_type"].(string)
		if orderType == "" {
			orderType = "dine_in"
		}
		payStatus, _ := payload["payment_status"].(string)
		if payStatus == "" {
			payStatus = "unpaid"
		}
		total := 0.0
		switch v := payload["total_amount"].(type) {
		case float64:
			total = v
		case json.Number:
			f, _ := v.Float64()
			total = f
		}
		out = append(out, map[string]interface{}{
			"orderId":        orderID,
			"order_id":       orderID,
			"status":         status,
			"orderType":      orderType,
			"order_type":     orderType,
			"totalAmount":    total,
			"total_amount":   total,
			"itemCount":      itemCount,
			"paymentStatus":  payStatus,
			"payment_status": payStatus,
			"paymentMethod":  payload["payment_method"],
			"createdAt":      createdAt.UTC().Format(time.RFC3339),
			"created_at":     createdAt.UTC().Format(time.RFC3339),
			"tableNumber":    payload["table_number"],
			"event_id":       id,
			"shop_id":        sid,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list orders rows: %w", err)
	}

	// Fold later kitchen / status / payment events onto each order.
	// Nest list shows lifecycle status; pos_sale alone is usually "not_sent".
	statusByOrder := map[string]string{}
	payByOrder := map[string]string{}
	srows, serr := s.pool.Query(ctx, `
		SELECT COALESCE(event_type, ''), COALESCE(payload::text, '{}'), created_at
		FROM "Event"
		WHERE shop_id = $1
		  AND event_type IN ('order_sent_to_kitchen', 'order_status_updated', 'order_payment_updated')
		ORDER BY created_at ASC
		LIMIT 2000
	`, shopID)
	if serr == nil {
		defer srows.Close()
		for srows.Next() {
			var et, payloadStr string
			var createdAt time.Time
			if err := srows.Scan(&et, &payloadStr, &createdAt); err != nil {
				break
			}
			var payload map[string]interface{}
			_ = json.Unmarshal([]byte(payloadStr), &payload)
			if payload == nil {
				continue
			}
			oid, _ := payload["order_id"].(string)
			if oid == "" {
				continue
			}
			switch et {
			case "order_sent_to_kitchen":
				statusByOrder[oid] = "pending"
			case "order_status_updated":
				st, _ := payload["status"].(string)
				if st == "" {
					st, _ = payload["kitchen_status"].(string)
				}
				if st != "" {
					statusByOrder[oid] = st
				}
			case "order_payment_updated":
				ps, _ := payload["payment_status"].(string)
				if ps != "" {
					payByOrder[oid] = ps
				}
			}
		}
	}

	for i := range out {
		oid, _ := out[i]["orderId"].(string)
		if st, ok := statusByOrder[oid]; ok && st != "" {
			out[i]["status"] = st
			out[i]["kitchenStatus"] = st
			if st != "not_sent" {
				out[i]["sentToKitchen"] = true
			}
		}
		if ps, ok := payByOrder[oid]; ok && ps != "" {
			out[i]["paymentStatus"] = ps
			out[i]["payment_status"] = ps
		}
	}

	return out, nil
}

func jsonUnmarshal(b []byte, v interface{}) error {
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

// GetOrder loads a pos_sale event by order_id for a shop.
func (s *Service) GetOrder(ctx context.Context, shopID, orderID string) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil, apperrors.BadRequest("orderId is required")
	}

	// 1) Load the original pos_sale
	events, err := s.eventStore.QueryByShopAndType(ctx, shopID, eventstore.EventPosSale, 1000, 0)
	if err != nil {
		return nil, err
	}
	var base map[string]interface{}
	var createdAt time.Time
	var eventID string
	for _, e := range events {
		var p map[string]interface{}
		if err := jsonUnmarshal(e.Payload, &p); err != nil {
			continue
		}
		oid, _ := p["order_id"].(string)
		if oid == "" {
			oid, _ = p["orderId"].(string)
		}
		if oid != orderID {
			continue
		}
		base = p
		createdAt = e.CreatedAt
		eventID = e.ID
		break
	}
	if base == nil {
		return nil, apperrors.NotFound("Order not found")
	}

	status, _ := base["status"].(string)
	if status == "" {
		status, _ = base["kitchen_status"].(string)
	}
	if status == "" {
		status = "not_sent"
	}
	kitchenStatus := status
	sentToKitchen := false
	if v, ok := base["sent_to_kitchen"].(bool); ok {
		sentToKitchen = v
	}
	payStatus, _ := base["payment_status"].(string)
	if payStatus == "" {
		payStatus = "unpaid"
	}
	timeline := []map[string]interface{}{
		{
			"status":    status,
			"timestamp": createdAt.UTC().Format(time.RFC3339),
			"actor":     nil,
		},
	}

	// 2) Fold later lifecycle events for this order
	foldTypes := []string{
		eventstore.EventOrderSentToKitchen,
		eventstore.EventOrderStatusUpdated,
		eventstore.EventOrderPaymentUpdated,
	}
	for _, et := range foldTypes {
		evs, e2 := s.eventStore.QueryByShopAndType(ctx, shopID, et, 500, 0)
		if e2 != nil {
			continue
		}
		// Oldest → newest so last write wins
		for i := len(evs) - 1; i >= 0; i-- {
			// Query returns DESC typically — normalize by scanning all
		}
		for _, e := range evs {
			var p map[string]interface{}
			if err := jsonUnmarshal(e.Payload, &p); err != nil {
				continue
			}
			oid, _ := p["order_id"].(string)
			if oid == "" {
				oid, _ = p["orderId"].(string)
			}
			if oid != orderID {
				continue
			}
			switch e.EventType {
			case eventstore.EventOrderSentToKitchen:
				sentToKitchen = true
				status = "pending"
				kitchenStatus = "pending"
				if st, ok := p["status"].(string); ok && st != "" {
					status = st
					kitchenStatus = st
				}
				timeline = append(timeline, map[string]interface{}{
					"status":    kitchenStatus,
					"timestamp": e.CreatedAt.UTC().Format(time.RFC3339),
					"actor":     nil,
				})
			case eventstore.EventOrderStatusUpdated:
				if st, ok := p["status"].(string); ok && st != "" {
					status = st
					kitchenStatus = st
					timeline = append(timeline, map[string]interface{}{
						"status":    st,
						"timestamp": e.CreatedAt.UTC().Format(time.RFC3339),
						"actor":     nil,
					})
				}
			case eventstore.EventOrderPaymentUpdated:
				if ps, ok := p["payment_status"].(string); ok && ps != "" {
					payStatus = ps
				}
			}
		}
	}

	// 3) Normalize items for web (unitPrice, lineTotal, menuItemId)
	rawItems, _ := base["items"].([]interface{})
	if rawItems == nil {
		rawItems, _ = base["lines"].([]interface{})
	}
	items := make([]map[string]interface{}, 0)
	totalAmount := 0.0
	if v, ok := base["total_amount"].(float64); ok {
		totalAmount = v
	} else if v, ok := base["total"].(float64); ok {
		totalAmount = v
	}
	for _, raw := range rawItems {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		qty := asFloatMap(m, "quantity", "qty")
		unit := asFloatMap(m, "unit_price", "selling_price", "unitPrice")
		lineTotal := asFloatMap(m, "line_total", "lineTotal")
		if lineTotal == 0 && unit > 0 && qty > 0 {
			lineTotal = unit * qty
		}
		name, _ := m["name"].(string)
		if name == "" {
			name, _ = m["menu_name"].(string)
		}
		menuID, _ := m["menu_item_id"].(string)
		if menuID == "" {
			menuID, _ = m["menu_id"].(string)
		}
		items = append(items, map[string]interface{}{
			"menuItemId":      menuID,
			"menu_item_id":    menuID,
			"name":            name,
			"quantity":        qty,
			"unitPrice":       unit,
			"unit_price":      unit,
			"lineTotal":       lineTotal,
			"line_total":      lineTotal,
			"lineType":        m["line_type"],
			"comboId":         m["combo_id"],
			"selections":      m["selections"],
			"comboSelections": m["combo_selections"],
			"stockDeductions": m["stock_deductions"],
		})
	}

	orderType, _ := base["order_type"].(string)
	if orderType == "" {
		orderType = "dine_in"
	}
	payMethod, _ := base["payment_method"].(string)

	return map[string]interface{}{
		"orderId":         orderID,
		"order_id":        orderID,
		"status":          status,
		"kitchenStatus":   kitchenStatus,
		"kitchen_status":  kitchenStatus,
		"sentToKitchen":   sentToKitchen,
		"sent_to_kitchen": sentToKitchen,
		"orderType":       orderType,
		"order_type":      orderType,
		"totalAmount":     totalAmount,
		"total_amount":    totalAmount,
		"itemCount":       len(items),
		"items":           items,
		"paymentStatus":   payStatus,
		"payment_status":  payStatus,
		"paymentMethod":   payMethod,
		"payment_method":  payMethod,
		"paymentSplits":   base["payment_splits"],
		"createdAt":       createdAt.UTC().Format(time.RFC3339),
		"created_at":      createdAt.UTC().Format(time.RFC3339),
		"tableNumber":     base["table_number"],
		"table_number":    base["table_number"],
		"customerName":    base["customer_name"],
		"customer_name":   base["customer_name"],
		"customerPhone":   base["customer_phone"],
		"customer_phone":  base["customer_phone"],
		"events":          timeline,
		"event_id":        eventID,
		"shop_id":         shopID,
	}, nil
}

func asFloatMap(m map[string]interface{}, keys ...string) float64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case string:
			var f float64
			fmt.Sscanf(v, "%f", &f)
			if f != 0 {
				return f
			}
		}
	}
	return 0
}

// SendToKitchen marks an order as sent and appends order_sent_to_kitchen.
func (s *Service) SendToKitchen(ctx context.Context, shopID, orderID, actorUserID string) (map[string]interface{}, error) {
	existing, err := s.GetOrder(ctx, shopID, orderID)
	if err != nil {
		return nil, err
	}
	sent, _ := existing["sent_to_kitchen"].(bool)
	ks, _ := existing["kitchen_status"].(string)
	if sent || ks == "pending" || ks == "preparing" || ks == "ready" {
		return map[string]interface{}{
			"success": true,
			"orderId": orderID,
			"message": "Already sent to kitchen",
		}, nil
	}

	idemKey := "send-kitchen-" + orderID
	actor := actorUserID
	var actorPtr *string
	if actor != "" {
		actorPtr = &actor
	}

	// Kitchen snapshot only — strip financial mutation fields so projections
	// cannot re-apply inventory/revenue even if mis-routed.
	payload := map[string]interface{}{
		"order_id": orderID, "shop_id": shopID,
		"sent_to_kitchen": true, "status": "pending", "kitchen_status": "pending",
		"sent_at": time.Now().UTC().Format(time.RFC3339),
	}
	for _, k := range []string{"items", "lines", "table_number", "customer_name", "customer_phone", "order_type", "notes"} {
		if v, ok := existing[k]; ok {
			payload[k] = v
		}
	}
	// Informational totals only (not processed as sales)
	if v, ok := existing["total_amount"]; ok {
		payload["display_total"] = v
	}
	delete(payload, "stock_deductions")
	delete(payload, "cogs")
	delete(payload, "food_cost")
	delete(payload, "food_cost_total")
	for _, key := range []string{"items", "lines"} {
		arr, ok := payload[key].([]interface{})
		if !ok {
			// existing may store []map[string]interface{}
			if maps, ok2 := payload[key].([]map[string]interface{}); ok2 {
				arr = make([]interface{}, len(maps))
				for i, m := range maps {
					arr[i] = m
				}
			} else {
				continue
			}
		}
		cleaned := make([]interface{}, 0, len(arr))
		for _, e := range arr {
			m, ok := e.(map[string]interface{})
			if !ok {
				cleaned = append(cleaned, e)
				continue
			}
			cp := make(map[string]interface{}, len(m))
			for k, v := range m {
				cp[k] = v
			}
			delete(cp, "stock_deductions")
			delete(cp, "stockDeductions")
			delete(cp, "food_cost")
			delete(cp, "cogs")
			cleaned = append(cleaned, cp)
		}
		payload[key] = cleaned
	}

	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType:      eventstore.EventOrderSentToKitchen,
		ActorUserID:    actorPtr,
		IdempotencyKey: idemKey,
		Payload:        payload,
	})
	if err != nil {
		// Idempotent replay / already sent
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "idempotency") || strings.Contains(msg, "Conflict") {
			return map[string]interface{}{
				"success": true,
				"orderId": orderID,
				"message": "Already sent to kitchen",
				"status":  "pending",
			}, nil
		}
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"orderId": orderID,
		"eventId": event.ID,
		"message": "Order sent to kitchen",
		"status":  "pending",
	}, nil
}

// UpdatePayment updates payment status on an order.
func (s *Service) UpdatePayment(ctx context.Context, shopID, orderID, actorUserID string, status, method, mpesaRef, notes string) (map[string]interface{}, error) {
	existing, err := s.GetOrder(ctx, shopID, orderID)
	if err != nil {
		return nil, err
	}
	idemKey := fmt.Sprintf("order-pay-%s-%s-%d", orderID, status, time.Now().UnixNano())
	actor := actorUserID
	var actorPtr *string
	if actor != "" {
		actorPtr = &actor
	}
	payload := map[string]interface{}{
		"order_id":        orderID,
		"payment_status":  status,
		"payment_method":  method,
		"mpesa_reference": nullIfEmpty(mpesaRef),
		"notes":           nullIfEmpty(notes),
		"shop_id":         shopID,
		"previous_status": existing["payment_status"],
	}
	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType:      eventstore.EventOrderPaymentUpdated,
		ActorUserID:    actorPtr,
		IdempotencyKey: idemKey,
		Payload:        payload,
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success":        true,
		"orderId":        orderID,
		"eventId":        event.ID,
		"payment_status": status,
	}, nil
}

// MenuAvailabilityItem matches Nest GET /pos/menu-availability shape (web chips).
type MenuAvailabilityItem struct {
	MenuItemID  string `json:"menuItemId"`
	Name        string `json:"name"`
	IsAvailable bool   `json:"isAvailable"`
	MaxPortions int    `json:"maxPortions"`
	IsLow       bool   `json:"isLow,omitempty"`
}

// ListMenuAvailability builds chips from ListMenus in one pass (Nest speed).
// Does NOT call ResolveMenuItemLine per menu — that made POS hang for minutes.
func (s *Service) ListMenuAvailability(ctx context.Context, shopID string) ([]MenuAvailabilityItem, error) {
	if shopID == "" {
		shopID = "1"
	}
	menus, err := s.resolve.ListMenus(ctx, shopID)
	if err != nil {
		return []MenuAvailabilityItem{}, err
	}
	return availabilityFromMenus(menus), nil
}

func availabilityFromMenus(menus []map[string]interface{}) []MenuAvailabilityItem {
	out := make([]MenuAvailabilityItem, 0, len(menus))
	for _, m := range menus {
		mid := strMap(m, "menuId", "menu_id", "menuItemId", "menu_item_id")
		if mid == "" {
			continue
		}
		name := strMap(m, "name")
		if name == "" {
			name = mid
		}
		maxP := intMap(m, "maxPortions", "availableQuantity", "available_quantity")
		isAvail := boolMap(m, "isAvailable", "is_available")
		// If flags missing, treat positive portions as available
		if !isAvail && maxP > 0 {
			isAvail = true
		}
		if maxP < 0 {
			maxP = 0
		}
		out = append(out, MenuAvailabilityItem{
			MenuItemID: mid, Name: name,
			IsAvailable: isAvail, MaxPortions: maxP,
			IsLow: isAvail && maxP > 0 && maxP <= 5,
		})
	}
	return out
}

func strMap(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if t != "" {
					return t
				}
			}
		}
	}
	return ""
}

func intMap(m map[string]interface{}, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case int:
				return t
			case int64:
				return int(t)
			case float64:
				return int(t)
			}
		}
	}
	return 0
}

func boolMap(m map[string]interface{}, keys ...string) bool {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
	}
	return false
}

// Bootstrap is one RTT for web POS when pointed at Go (Nest parity GET /pos/bootstrap).
// Menus + availability share one ListMenus call; stock map loaded once.
func (s *Service) Bootstrap(ctx context.Context, shopID string) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	// Single ListMenus — availability derived in-memory (no second pass / no N resolves)
	menus, err := s.resolve.ListMenus(ctx, shopID)
	if err != nil {
		menus = []map[string]interface{}{}
	}
	avail := availabilityFromMenus(menus)
	// Combos + categories from Nest tables when present (best-effort)
	var combos []map[string]interface{}
	crows, cerr := s.pool.Query(ctx, `
		SELECT combo_id, name, COALESCE(selling_price,0)::text, COALESCE(active,true)
		FROM "Combo" WHERE shop_id = $1 AND COALESCE(active,true)
		ORDER BY name LIMIT 200`, shopID)
	if cerr == nil {
		defer crows.Close()
		for crows.Next() {
			var id, name, price string
			var active bool
			if crows.Scan(&id, &name, &price, &active) == nil {
				combos = append(combos, map[string]interface{}{
					"comboId": id, "combo_id": id, "name": name,
					"sellingPrice": price, "active": active,
				})
			}
		}
	}
	if combos == nil {
		combos = []map[string]interface{}{}
	}
	var categories []map[string]interface{}
	krows, kerr := s.pool.Query(ctx, `
		SELECT category_id, name, COALESCE(active,true)
		FROM "MenuCategory" WHERE shop_id = $1 AND COALESCE(active,true)
		ORDER BY name LIMIT 100`, shopID)
	if kerr == nil {
		defer krows.Close()
		for krows.Next() {
			var id, name string
			var active bool
			if krows.Scan(&id, &name, &active) == nil {
				categories = append(categories, map[string]interface{}{
					"categoryId": id, "category_id": id, "name": name, "active": active,
				})
			}
		}
	}
	if categories == nil {
		categories = []map[string]interface{}{}
	}
	if menus == nil {
		menus = []map[string]interface{}{}
	}
	if avail == nil {
		avail = []MenuAvailabilityItem{}
	}
	// Nest + web expect menus AND items; availability always an array
	return map[string]interface{}{
		"success":      true,
		"shopId":       shopID,
		"shop_id":      shopID,
		"menus":        menus,
		"items":        menus, // Nest listMenus shape alias
		"count":        len(menus),
		"availability": avail,
		"combos":       combos,
		"categories":   categories,
		"source":       "go_pos",
	}, nil
}
