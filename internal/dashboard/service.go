package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/pkg/money"
)

// Nest APPROVAL_EVENT_TYPES for openApprovals count
var approvalEventTypes = []string{
	"WASTE_RECORDED", "waste_recorded", "waste",
	"STOCK_ADJUSTMENT", "finished_good_adjusted",
	"RECEIVING", "received",
	"VARIANCE_WRITEOFF", "REFUND", "refill_requested",
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Summary matches Nest getSummary(shopId, from, to) → web ShopSummary.
func (s *Service) Summary(ctx context.Context, shopID string, from, to time.Time) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if from.IsZero() {
		from = startOfUTCDay(time.Now().UTC())
	}
	if to.IsZero() {
		to = startOfUTCDay(time.Now().UTC())
	}
	keys := periodKeys(from, to)
	periodFrom, periodTo := "", ""
	if len(keys) > 0 {
		periodFrom, periodTo = keys[0], keys[len(keys)-1]
	}

	periodRevenue := 0.0
	todayOrders := 0
	if len(keys) > 0 {
		var salesStr string
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(total_sales),0)::text, COALESCE(SUM(total_orders),0)::int
			FROM "SalesProjection"
			WHERE shop_id = $1 AND period = ANY($2)`, shopID, keys).Scan(&salesStr, &todayOrders)
		if m, e := money.New(salesStr); e == nil {
			periodRevenue = m.Float64()
		}
	}

	lowStock, _ := s.countLowStock(ctx, shopID)
	nearExpiry, _ := s.countNearExpiry(ctx, shopID)

	var openApprovals int
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM "Event"
		WHERE shop_id = $1 AND approved_by_id IS NULL
		  AND event_type = ANY($2)
		  AND COALESCE(payload->>'rejected','') <> 'true'`, shopID, approvalEventTypes).Scan(&openApprovals)

	aov := 0.0
	if todayOrders > 0 {
		aov = periodRevenue / float64(todayOrders)
	}

	return map[string]interface{}{
		"shopId":            shopID,
		"shopName":          "Shop " + shopID,
		"todayRevenue":      round2(periodRevenue),
		"todayOrders":       todayOrders,
		"averageOrderValue": round2(aov),
		"periodFrom":        periodFrom,
		"periodTo":          periodTo,
		"openApprovals":     openApprovals,
		"lowStockCount":     lowStock,
		"nearExpiryCount":   nearExpiry,
	}, nil
}

// Nest: reorder_point > 0 required; exclude finished/production/fg categories.
func (s *Service) countLowStock(ctx context.Context, shopID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM "InventoryProjection" p
		INNER JOIN "Item" i ON i.item_id = p.item_id
		WHERE p.shop_id = $1
		  AND COALESCE(i.reorder_point, 0) > 0
		  AND p.available_stock <= i.reorder_point
		  AND COALESCE(i.category,'') !~* 'finished|production|\yfg\y'`, shopID).Scan(&n)
	return n, err
}

func (s *Service) countNearExpiry(ctx context.Context, shopID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM "InventoryProjection" p
		LEFT JOIN "Item" i ON i.item_id = p.item_id
		WHERE p.shop_id = $1
		  AND p.days_to_expiry_min IS NOT NULL
		  AND p.days_to_expiry_min >= 0 AND p.days_to_expiry_min <= 3
		  AND p.available_stock > 0
		  AND COALESCE(i.category,'') !~* 'finished|production|\yfg\y'`, shopID).Scan(&n)
	return n, err
}

// KPIs matches Nest getKpis — period waste, consumption COGS preference.
func (s *Service) KPIs(ctx context.Context, shopID string, from, to time.Time) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -30)
	}
	keys := periodKeys(from, to)
	rangeStart := startOfUTCDay(from)
	rangeEnd := endOfUTCDay(to)

	periodRevenue := 0.0
	if len(keys) > 0 {
		var salesStr string
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(total_sales),0)::text FROM "SalesProjection"
			WHERE shop_id = $1 AND period = ANY($2)`, shopID, keys).Scan(&salesStr)
		if m, e := money.New(salesStr); e == nil {
			periodRevenue = m.Float64()
		}
	}

	// Nest: period-scoped waste (NOT lifetime WasteProjection)
	wasteValue := s.sumWasteInRange(ctx, shopID, rangeStart, rangeEnd)

	// Nest: periodCogs = consumption.totalConsumedValue if > 0 else wasteValue
	consumptionValue := s.sumConsumptionValue(ctx, shopID, rangeStart, rangeEnd)
	periodCogs := wasteValue
	if consumptionValue > 0 {
		periodCogs = consumptionValue
	}

	var invStr string
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(total_value),0)::text FROM "InventoryProjection" WHERE shop_id = $1`, shopID).Scan(&invStr)
	invValue := 0.0
	if m, e := money.New(invStr); e == nil {
		invValue = m.Float64()
	}

	var foodCost, grossMargin, turnover interface{}
	if periodRevenue >= 1 {
		fc := (periodCogs / periodRevenue) * 100
		if fc > 999.9 {
			fc = 999.9
		}
		foodCost = round2(fc)
		gm := ((periodRevenue - periodCogs) / periodRevenue) * 100
		if gm < -999.9 {
			gm = -999.9
		}
		if gm > 999.9 {
			gm = 999.9
		}
		grossMargin = round2(gm)
	} else {
		foodCost = nil
		grossMargin = nil
	}
	if invValue > 0 {
		turnover = round2(periodRevenue / invValue)
	} else {
		turnover = nil
	}

	return map[string]interface{}{
		"foodCostPercent":          foodCost,
		"grossMarginPercent":       grossMargin,
		"inventoryTurnover":        turnover,
		"wasteValue":               round2(wasteValue),
		"inventoryAccuracyPercent": nil,
		"periodRevenue":            round2(periodRevenue),
		"periodCogs":               round2(periodCogs),
	}, nil
}

func (s *Service) sumWasteInRange(ctx context.Context, shopID string, from, to time.Time) float64 {
	var totalStr string
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(COALESCE(total_cost,0)),0)::text FROM "Event"
		WHERE shop_id = $1
		  AND event_type IN ('waste_recorded','waste','WASTE_RECORDED')
		  AND created_at >= $2 AND created_at <= $3`, shopID, from, to).Scan(&totalStr)
	if m, e := money.New(totalStr); e == nil {
		return m.Float64()
	}
	return 0
}

func (s *Service) sumConsumptionValue(ctx context.Context, shopID string, from, to time.Time) float64 {
	// Approximate Nest consumption summary: sum COGS from pos_sale in range
	return s.sumCogsFromEvents(ctx, shopID, from, to)
}

func (s *Service) sumCogsFromEvents(ctx context.Context, shopID string, from, to time.Time) float64 {
	rows, err := s.pool.Query(ctx, `
		SELECT payload FROM "Event"
		WHERE shop_id = $1
		  AND event_type IN ('pos_sale','pos_order_created','ORDER_PAID','sale_completed')
		  AND created_at >= $2 AND created_at <= $3
		LIMIT 5000`, shopID, from, to)
	if err != nil {
		return 0
	}
	defer rows.Close()
	total := 0.0
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			continue
		}
		var p map[string]interface{}
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		if v, ok := p["cogs"].(float64); ok {
			total += v
			continue
		}
		if v, ok := p["total_cogs"].(float64); ok {
			total += v
			continue
		}
		if arr, ok := p["stock_deductions"].([]interface{}); ok {
			for _, e := range arr {
				m, _ := e.(map[string]interface{})
				if m == nil {
					continue
				}
				qty := asF(m["quantity"])
				uc := asF(m["unit_cost"])
				if uc == 0 {
					uc = asF(m["unitCost"])
				}
				total += qty * uc
			}
		}
	}
	return total
}

// Alerts matches Nest getAlerts — low stock (ROP>0), near expiry, pending approvals.
func (s *Service) Alerts(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	var out []map[string]interface{}

	// Low stock — Nest findLowStockRows
	rows, err := s.pool.Query(ctx, `
		SELECT p.item_id, COALESCE(i.name, p.item_id), p.available_stock, p.updated_at
		FROM "InventoryProjection" p
		INNER JOIN "Item" i ON i.item_id = p.item_id
		WHERE p.shop_id = $1
		  AND COALESCE(i.reorder_point, 0) > 0
		  AND p.available_stock <= i.reorder_point
		  AND COALESCE(i.category,'') !~* 'finished|production|\yfg\y'
		ORDER BY p.available_stock ASC
		LIMIT 15`, shopID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, name string
			var stock float64
			var updated *time.Time
			_ = rows.Scan(&id, &name, &stock, &updated)
			sev := "warning"
			if stock <= 3 {
				sev = "critical"
			}
			created := time.Now().UTC().Format(time.RFC3339)
			if updated != nil {
				created = updated.UTC().Format(time.RFC3339)
			}
			out = append(out, map[string]interface{}{
				"id": "low-" + id, "type": "low_stock", "severity": sev,
				"message":   fmt.Sprintf("Low stock: %s (%.0f left)", name, stock),
				"href":      fmt.Sprintf("/shop/%s/dashboard/inventory?highlight=%s", shopID, id),
				"createdAt": created,
			})
		}
	}

	// Near expiry
	rows2, err := s.pool.Query(ctx, `
		SELECT p.item_id, COALESCE(i.name, p.item_id), p.days_to_expiry_min, p.updated_at
		FROM "InventoryProjection" p
		LEFT JOIN "Item" i ON i.item_id = p.item_id
		WHERE p.shop_id = $1
		  AND p.days_to_expiry_min IS NOT NULL
		  AND p.days_to_expiry_min >= 0 AND p.days_to_expiry_min <= 3
		  AND p.available_stock > 0
		ORDER BY p.days_to_expiry_min ASC
		LIMIT 15`, shopID)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var id, name string
			var days int
			var updated *time.Time
			_ = rows2.Scan(&id, &name, &days, &updated)
			sev := "warning"
			if days <= 1 {
				sev = "critical"
			}
			created := time.Now().UTC().Format(time.RFC3339)
			if updated != nil {
				created = updated.UTC().Format(time.RFC3339)
			}
			dayWord := "days"
			if days == 1 {
				dayWord = "day"
			}
			out = append(out, map[string]interface{}{
				"id": "expiry-" + id, "type": "near_expiry", "severity": sev,
				"message":   fmt.Sprintf("Near expiry: %s (%d %s left)", name, days, dayWord),
				"href":      fmt.Sprintf("/shop/%s/dashboard/inventory?highlight=%s", shopID, id),
				"createdAt": created,
			})
		}
	}

	// Pending approval alerts
	rows3, err := s.pool.Query(ctx, `
		SELECT id, event_type, created_at FROM "Event"
		WHERE shop_id = $1 AND approved_by_id IS NULL
		  AND event_type = ANY($2)
		  AND COALESCE(payload->>'rejected','') <> 'true'
		ORDER BY created_at DESC LIMIT 10`, shopID, approvalEventTypes)
	if err == nil {
		defer rows3.Close()
		for rows3.Next() {
			var id, et string
			var created time.Time
			_ = rows3.Scan(&id, &et, &created)
			out = append(out, map[string]interface{}{
				"id": "approval-" + id, "type": "approval", "severity": "info",
				"message":   fmt.Sprintf("Pending approval: %s", stringsReplace(et)),
				"href":      fmt.Sprintf("/shop/%s/dashboard/approvals", shopID),
				"createdAt": created.UTC().Format(time.RFC3339),
			})
		}
	}

	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func stringsReplace(et string) string {
	r := make([]rune, 0, len(et))
	for _, c := range et {
		if c == '_' {
			r = append(r, ' ')
		} else {
			r = append(r, c)
		}
	}
	return string(r)
}

func periodKeys(from, to time.Time) []string {
	var keys []string
	cur := startOfUTCDay(from)
	end := startOfUTCDay(to)
	for !cur.After(end) {
		keys = append(keys, cur.Format("2006-01-02"))
		cur = cur.AddDate(0, 0, 1)
	}
	return keys
}

func startOfUTCDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func endOfUTCDay(t time.Time) time.Time {
	u := startOfUTCDay(t)
	return u.Add(24*time.Hour - time.Nanosecond)
}

func round2(n float64) float64 {
	if n >= 0 {
		return float64(int(n*100+0.5)) / 100
	}
	return float64(int(n*100-0.5)) / 100
}

func asF(v interface{}) float64 {
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

// RebuildSales recomputes SalesProjection for the shop from POS events.
func (s *Service) RebuildSales(ctx context.Context, shopID string) (int, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DATE(created_at AT TIME ZONE 'UTC')::text AS period,
		       COALESCE(SUM(COALESCE(total_cost, (payload->>'total_amount')::float, (payload->>'revenue')::float, 0)),0),
		       COUNT(*)::int
		FROM "Event"
		WHERE shop_id = $1
		  AND event_type IN ('pos_order_created','ORDER_PAID','sale_completed','pos_sale')
		GROUP BY 1
		ORDER BY 1 DESC
		LIMIT 90`, shopID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var period, sales string
		var orders int
		if rows.Scan(&period, &sales, &orders) != nil {
			continue
		}
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO "SalesProjection" (id, shop_id, period, total_sales, total_orders, avg_order_value, updated_at)
			VALUES ($5, $1, $2, $3::numeric, $4,
			        CASE WHEN $4 > 0 THEN ($3::numeric / $4) ELSE 0 END, NOW())
			ON CONFLICT (shop_id, period) DO UPDATE SET
			  total_sales = EXCLUDED.total_sales,
			  total_orders = EXCLUDED.total_orders,
			  avg_order_value = EXCLUDED.avg_order_value,
			  updated_at = NOW()`, shopID, period, sales, orders, uuid.New().String())
		n++
	}
	return n, nil
}
