package consumption

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Summary matches Nest/web: totalConsumedValue, periodRevenue, foodCostPercent, topProducts, topMenuItems.
func (s *Service) Summary(ctx context.Context, shopID string, from, to time.Time) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -30)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT payload, created_at, total_cost::text FROM "Event"
		WHERE shop_id = $1
		  AND event_type IN ('pos_sale','pos_order_created','ORDER_PAID','sale_completed')
		  AND created_at >= $2 AND created_at <= $3
		ORDER BY created_at DESC LIMIT 5000`, shopID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type prod struct {
		id, name, unit string
		qty, value     float64
	}
	type menu struct {
		id, name       string
		units, revenue float64
		food           float64
	}
	byProd := map[string]*prod{}
	byMenu := map[string]*menu{}
	periodRevenue := 0.0
	totalConsumedValue := 0.0

	for rows.Next() {
		var raw []byte
		var created time.Time
		var totalCostStr *string
		if rows.Scan(&raw, &created, &totalCostStr) != nil {
			continue
		}
		var p map[string]interface{}
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		if v, ok := p["total_amount"].(float64); ok {
			periodRevenue += v
		} else if v, ok := p["total"].(float64); ok {
			periodRevenue += v
		}

		// Menu lines
		for _, key := range []string{"items", "lines", "order_items"} {
			arr, ok := p[key].([]interface{})
			if !ok {
				continue
			}
			for _, e := range arr {
				m, _ := e.(map[string]interface{})
				if m == nil {
					continue
				}
				mid := str(m["menu_id"])
				if mid == "" {
					mid = str(m["menuId"])
				}
				if mid == "" {
					mid = str(m["menu_item_id"])
				}
				if mid == "" {
					continue
				}
				name := str(m["menu_name"])
				if name == "" {
					name = str(m["name"])
				}
				if name == "" {
					name = mid
				}
				qty := num(m["quantity"])
				price := num(m["selling_price"])
				if price == 0 {
					price = num(m["unit_price"])
				}
				rev := qty * price
				if rev == 0 {
					rev = num(m["line_total"])
				}
				agg, ok := byMenu[mid]
				if !ok {
					agg = &menu{id: mid, name: name}
					byMenu[mid] = agg
				}
				agg.units += qty
				agg.revenue += rev
			}
		}

		// Stock deductions = consumed products
		if arr, ok := p["stock_deductions"].([]interface{}); ok {
			for _, e := range arr {
				m, _ := e.(map[string]interface{})
				if m == nil {
					continue
				}
				id := str(m["item_id"])
				if id == "" {
					id = str(m["itemId"])
				}
				if id == "" {
					continue
				}
				name := str(m["item_name"])
				if name == "" {
					name = id
				}
				unit := str(m["unit"])
				if unit == "" {
					unit = "pcs"
				}
				qty := num(m["quantity"])
				uc := num(m["unit_cost"])
				if uc == 0 {
					uc = num(m["unitCost"])
				}
				val := qty * uc
				agg, ok := byProd[id]
				if !ok {
					agg = &prod{id: id, name: name, unit: unit}
					byProd[id] = agg
				}
				agg.qty += qty
				agg.value += val
				totalConsumedValue += val
			}
		}
	}

	// Resolve names from Item
	for id, pr := range byProd {
		if pr.name == id {
			var n *string
			_ = s.pool.QueryRow(ctx, `SELECT name FROM "Item" WHERE item_id = $1`, id).Scan(&n)
			if n != nil && *n != "" {
				pr.name = *n
			}
		}
		_ = id
	}

	topProducts := make([]map[string]interface{}, 0, len(byProd))
	for _, pr := range byProd {
		topProducts = append(topProducts, map[string]interface{}{
			"itemId": pr.id, "name": pr.name, "unit": pr.unit,
			"quantityConsumed": pr.qty, "valueConsumed": pr.value,
		})
	}
	// sort by value
	for i := 0; i < len(topProducts); i++ {
		for j := i + 1; j < len(topProducts); j++ {
			vi, _ := topProducts[i]["valueConsumed"].(float64)
			vj, _ := topProducts[j]["valueConsumed"].(float64)
			if vj > vi {
				topProducts[i], topProducts[j] = topProducts[j], topProducts[i]
			}
		}
	}
	if len(topProducts) > 20 {
		topProducts = topProducts[:20]
	}

	topMenu := make([]map[string]interface{}, 0, len(byMenu))
	for _, m := range byMenu {
		fc := interface{}(nil)
		if m.revenue > 0 && m.food > 0 {
			fc = round1((m.food / m.revenue) * 100)
		}
		topMenu = append(topMenu, map[string]interface{}{
			"menuItemId": m.id, "name": m.name,
			"unitsSold": m.units, "revenue": m.revenue,
			"foodCostValue": m.food, "foodCostPercent": fc,
		})
	}
	for i := 0; i < len(topMenu); i++ {
		for j := i + 1; j < len(topMenu); j++ {
			vi, _ := topMenu[i]["revenue"].(float64)
			vj, _ := topMenu[j]["revenue"].(float64)
			if vj > vi {
				topMenu[i], topMenu[j] = topMenu[j], topMenu[i]
			}
		}
	}
	if len(topMenu) > 20 {
		topMenu = topMenu[:20]
	}

	var foodCost interface{}
	if periodRevenue >= 1 {
		foodCost = round1((totalConsumedValue / periodRevenue) * 100)
	}

	return map[string]interface{}{
		"totalConsumedValue": round2(totalConsumedValue),
		"periodRevenue":      round2(periodRevenue),
		"foodCostPercent":    foodCost,
		"topProducts":        topProducts,
		"topMenuItems":       topMenu,
		"method":             "pos_stock_deductions",
		"shopId":             shopID,
	}, nil
}

func (s *Service) Product(ctx context.Context, shopID, productID string) (map[string]interface{}, error) {
	sum, err := s.Summary(ctx, shopID, time.Time{}, time.Time{})
	if err != nil {
		return nil, err
	}
	items, _ := sum["topProducts"].([]map[string]interface{})
	for _, it := range items {
		if it["itemId"] == productID {
			hist, _ := s.productHistory(ctx, shopID, productID)
			it["history"] = hist
			return it, nil
		}
	}
	return map[string]interface{}{
		"itemId": productID, "quantityConsumed": 0, "valueConsumed": 0,
		"history": []map[string]interface{}{},
	}, nil
}

func (s *Service) MenuItem(ctx context.Context, shopID, menuItemID string) (map[string]interface{}, error) {
	sum, err := s.Summary(ctx, shopID, time.Time{}, time.Time{})
	if err != nil {
		return nil, err
	}
	items, _ := sum["topMenuItems"].([]map[string]interface{})
	for _, it := range items {
		if it["menuItemId"] == menuItemID {
			hist, _ := s.menuHistory(ctx, shopID, menuItemID)
			it["history"] = hist
			return it, nil
		}
	}
	return map[string]interface{}{
		"menuItemId": menuItemID, "unitsSold": 0, "revenue": 0,
		"history": []map[string]interface{}{},
	}, nil
}

func (s *Service) Alerts(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	return []map[string]interface{}{}, nil
}

func (s *Service) productHistory(ctx context.Context, shopID, productID string) ([]map[string]interface{}, float64) {
	rows, err := s.pool.Query(ctx, `
		SELECT payload, created_at FROM "Event"
		WHERE shop_id = $1 AND event_type IN ('pos_sale','pos_order_created','ORDER_PAID','sale_completed')
		ORDER BY created_at ASC LIMIT 5000`, shopID)
	if err != nil {
		return []map[string]interface{}{}, 0
	}
	defer rows.Close()
	byDay := map[string]float64{}
	var total float64
	idLower := strings.ToLower(productID)
	for rows.Next() {
		var raw []byte
		var created time.Time
		if rows.Scan(&raw, &created) != nil {
			continue
		}
		var p map[string]interface{}
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		day := created.UTC().Format("2006-01-02")
		qty := 0.0
		if arr, ok := p["stock_deductions"].([]interface{}); ok {
			for _, e := range arr {
				m, _ := e.(map[string]interface{})
				if m == nil {
					continue
				}
				id := str(m["item_id"])
				if id == "" {
					id = str(m["itemId"])
				}
				if strings.EqualFold(id, idLower) {
					qty += num(m["quantity"])
				}
			}
		}
		if qty > 0 {
			byDay[day] += qty
			total += qty
		}
	}
	out := make([]map[string]interface{}, 0, len(byDay))
	for d, q := range byDay {
		out = append(out, map[string]interface{}{"date": d, "quantity": q})
	}
	return out, total
}

func (s *Service) menuHistory(ctx context.Context, shopID, menuItemID string) ([]map[string]interface{}, float64) {
	rows, err := s.pool.Query(ctx, `
		SELECT payload, created_at FROM "Event"
		WHERE shop_id = $1 AND event_type IN ('pos_sale','pos_order_created','ORDER_PAID','sale_completed')
		ORDER BY created_at ASC LIMIT 5000`, shopID)
	if err != nil {
		return []map[string]interface{}{}, 0
	}
	defer rows.Close()
	byDay := map[string]float64{}
	var total float64
	mid := strings.ToLower(menuItemID)
	for rows.Next() {
		var raw []byte
		var created time.Time
		if rows.Scan(&raw, &created) != nil {
			continue
		}
		var p map[string]interface{}
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		day := created.UTC().Format("2006-01-02")
		units := 0.0
		for _, key := range []string{"items", "lines", "order_items"} {
			arr, ok := p[key].([]interface{})
			if !ok {
				continue
			}
			for _, e := range arr {
				m, _ := e.(map[string]interface{})
				if m == nil {
					continue
				}
				id := str(m["menu_id"])
				if id == "" {
					id = str(m["menuId"])
				}
				if strings.EqualFold(id, mid) {
					units += num(m["quantity"])
				}
			}
		}
		if units > 0 {
			byDay[day] += units
			total += units
		}
	}
	out := make([]map[string]interface{}, 0, len(byDay))
	for d, q := range byDay {
		out = append(out, map[string]interface{}{"date": d, "unitsSold": q})
	}
	return out, total
}

func str(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func num(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}
func round2(n float64) float64 { return float64(int(n*100+0.5)) / 100 }
func round1(n float64) float64 { return float64(int(n*10+0.5)) / 10 }
