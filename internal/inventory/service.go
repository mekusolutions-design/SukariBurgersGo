package inventory

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
)

// Match Nest DEFAULT_REORDER_POINT when Item.reorder_point is unset.
const defaultReorderPoint = 0.0

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

type StockRow struct {
	ID                  string     `json:"id"`
	ItemID              string     `json:"item_id"`
	Name                string     `json:"name"`
	Unit                string     `json:"unit"`
	Category            string     `json:"category"`
	ReorderPoint        float64    `json:"reorder_point"`
	MinStock            float64    `json:"min_stock"`
	AvailableStock      float64    `json:"available_stock"`
	ExpiredStock        float64    `json:"expired_stock"`
	DamagedStock        float64    `json:"damaged_stock"`
	TotalValue          float64    `json:"total_value"`
	AvgUnitCost         float64    `json:"avg_unit_cost"`
	LastReceiptUnitCost *float64   `json:"last_receipt_unit_cost"`
	CostMissing         bool       `json:"cost_missing"`
	DaysToExpiryMin     *int       `json:"days_to_expiry_min"`
	LastReceivedAt      *time.Time `json:"last_received_at"`
	NextExpiryDate      *time.Time `json:"next_expiry_date"`
	ShopID              string     `json:"shop_id"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
}

type PaginatedStock struct {
	Items      []StockRow `json:"items"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	Limit      int        `json:"limit"`
	PageSize   int        `json:"pageSize"`
	TotalPages int        `json:"totalPages"`
}

type ListOptions struct {
	ShopID            string
	Page              int
	Limit             int
	Search            string
	Category          string
	Status            string // all | in-stock | low-stock | out-of-stock | near-expiry | expired
	LowStockOnly      bool
	IncludeFinished   bool
	IncludeWrittenOff bool
}

type Summary struct {
	ShopID          string  `json:"shopId"`
	TotalItems      int     `json:"totalItems"`
	TotalValue      float64 `json:"totalValue"`
	LowStockCount   int     `json:"lowStockCount"`
	OutOfStockCount int     `json:"outOfStockCount"`
	LowOrOutCount   int     `json:"lowOrOutCount"`
	ExpiredCount    int     `json:"expiredCount"`
	NearExpiryCount int     `json:"nearExpiryCount"`
}

func normalizeStatus(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func isFinishedCategory(cat string) bool {
	c := strings.ToLower(cat)
	return strings.Contains(c, "finished") ||
		strings.Contains(c, "production") ||
		strings.Contains(c, "fg")
}

// isPastExpiry matches Nest after mapStockRow:
// Nest always recomputes days from next_expiry_date and treats days < 0 as past.
// Prefer live next_expiry_date so stale days_to_expiry_min cannot keep dead stock visible.
func isPastExpiry(row StockRow) bool {
	if row.NextExpiryDate != nil {
		return daysUntil(*row.NextExpiryDate) < 0
	}
	if row.DaysToExpiryMin != nil {
		return *row.DaysToExpiryMin < 0
	}
	return false
}

func isWrittenOffExpired(row StockRow) bool {
	return row.AvailableStock <= 0 && isPastExpiry(row)
}

func reorderOf(row StockRow) float64 {
	if row.ReorderPoint > 0 {
		return row.ReorderPoint
	}
	return defaultReorderPoint
}

func liveDays(row StockRow) *int {
	if row.NextExpiryDate != nil {
		d := daysUntil(*row.NextExpiryDate)
		return &d
	}
	return row.DaysToExpiryMin
}

func matchesStatus(row StockRow, status string) bool {
	available := row.AvailableStock
	reorder := reorderOf(row)
	days := liveDays(row)
	switch status {
	case "", "all":
		return true
	case "in-stock":
		// Nest: available > reorder && (days == null || days > 3)
		okDays := days == nil || *days > 3
		return available > reorder && okDays
	case "low-stock", "low":
		return available > 0 && available <= reorder
	case "out-of-stock":
		// Nest: available <= 0 && !pastExpiry (written-off expired excluded)
		return available <= 0 && !isPastExpiry(row)
	case "near-expiry":
		return available > 0 && days != nil && *days >= 0 && *days <= 3
	case "expired":
		return isPastExpiry(row) && available > 0
	default:
		return true
	}
}

func (s *Service) fetchAll(ctx context.Context, shopID, search, category string, includeFinished bool) ([]StockRow, error) {
	where := []string{`p.shop_id = $1`}
	args := []interface{}{shopID}
	argN := 2

	if search != "" {
		where = append(where, fmt.Sprintf(`(i.name ILIKE $%d OR i.item_id ILIKE $%d)`, argN, argN))
		args = append(args, "%"+search+"%")
		argN++
	}
	if category != "" {
		where = append(where, fmt.Sprintf(`LOWER(COALESCE(i.category,'')) = LOWER($%d)`, argN))
		args = append(args, category)
		argN++
	}

	q := fmt.Sprintf(`
		SELECT p.id, p.item_id, COALESCE(i.name, p.item_id), COALESCE(i.unit, ''),
			COALESCE(i.category, ''),
			COALESCE(i.reorder_point, 0), COALESCE(i.min_stock, 0),
			COALESCE(p.available_stock, 0), COALESCE(p.expired_stock, 0), COALESCE(p.damaged_stock, 0),
			COALESCE(p.total_value, 0)::text,
			COALESCE(p.avg_unit_cost, 0)::text,
			p.last_receipt_unit_cost::text,
			p.days_to_expiry_min, p.last_received_at, p.next_expiry_date,
			p.shop_id, p.updated_at
		FROM "InventoryProjection" p
		LEFT JOIN "Item" i ON i.item_id = p.item_id
		WHERE %s
		ORDER BY p.updated_at DESC NULLS LAST
		LIMIT 2000
	`, strings.Join(where, " AND "))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StockRow
	for rows.Next() {
		var r StockRow
		var totalVal string
		var lastUC *string
		var avgStr string
		if err := rows.Scan(
			&r.ID, &r.ItemID, &r.Name, &r.Unit, &r.Category,
			&r.ReorderPoint, &r.MinStock,
			&r.AvailableStock, &r.ExpiredStock, &r.DamagedStock,
			&totalVal, &avgStr, &lastUC,
			&r.DaysToExpiryMin, &r.LastReceivedAt, &r.NextExpiryDate,
			&r.ShopID, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if m, err := money.New(totalVal); err == nil {
			r.TotalValue = m.Float64()
		}
		if m, err := money.New(avgStr); err == nil {
			r.AvgUnitCost = m.Float64()
		}
		if lastUC != nil && *lastUC != "" {
			if m, err := money.New(*lastUC); err == nil {
				v := m.Float64()
				r.LastReceiptUnitCost = &v
			}
		}
		// Derive WAC from value/qty when avg is unset (Nest parity)
		if r.AvgUnitCost <= 0 && r.AvailableStock > 0 && r.TotalValue > 0 {
			r.AvgUnitCost = r.TotalValue / r.AvailableStock
		}
		r.CostMissing = r.AvailableStock > 0 && r.AvgUnitCost <= 0 && r.TotalValue <= 0
		// Nest mapStockRow always computes days from next_expiry_date (ignores stored column).
		if r.NextExpiryDate != nil {
			d := daysUntil(*r.NextExpiryDate)
			r.DaysToExpiryMin = &d
		}
		// else keep DB days_to_expiry_min if present
		if !includeFinished && isFinishedCategory(r.Category) {
			continue
		}
		out = append(out, r)
	}
	if out == nil {
		out = []StockRow{}
	}
	return out, rows.Err()
}

func daysUntil(end time.Time) int {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	e := end.UTC().Truncate(24 * time.Hour)
	return int(math.Round(e.Sub(today).Hours() / 24))
}

func (s *Service) List(ctx context.Context, opts ListOptions) (*PaginatedStock, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.Limit < 1 || opts.Limit > 200 {
		opts.Limit = 20
	}
	if opts.ShopID == "" {
		opts.ShopID = "1"
	}

	status := normalizeStatus(opts.Status)
	if opts.LowStockOnly && (status == "" || status == "all") {
		status = "low-stock"
	}

	rows, err := s.fetchAll(ctx, opts.ShopID, opts.Search, opts.Category, opts.IncludeFinished)
	if err != nil {
		return nil, err
	}

	isDefault := status == "" || status == "all"
	hideWrittenOff := isDefault && !opts.IncludeWrittenOff && strings.TrimSpace(opts.Search) == ""

	filtered := make([]StockRow, 0, len(rows))
	for _, row := range rows {
		if hideWrittenOff && isWrittenOffExpired(row) {
			continue
		}
		if !matchesStatus(row, status) {
			continue
		}
		filtered = append(filtered, row)
	}

	total := len(filtered)
	start := (opts.Page - 1) * opts.Limit
	if start > total {
		start = total
	}
	end := start + opts.Limit
	if end > total {
		end = total
	}
	pageItems := filtered[start:end]

	totalPages := 0
	if opts.Limit > 0 {
		totalPages = (total + opts.Limit - 1) / opts.Limit
	}

	return &PaginatedStock{
		Items:      pageItems,
		Total:      total,
		Page:       opts.Page,
		Limit:      opts.Limit,
		PageSize:   opts.Limit,
		TotalPages: totalPages,
	}, nil
}

func (s *Service) Summary(ctx context.Context, shopID string) (*Summary, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.fetchAll(ctx, shopID, "", "", false)
	if err != nil {
		return nil, err
	}

	sum := &Summary{ShopID: shopID}
	for _, row := range rows {
		if isFinishedCategory(row.Category) {
			continue
		}
		writtenOff := isWrittenOffExpired(row)
		// Tracked items + value: exclude written-off expired (Nest getSummary)
		if !writtenOff {
			sum.TotalValue += row.TotalValue
			sum.TotalItems++
		}
		// Low/out counts also exclude written-off expired (Nest)
		if !writtenOff {
			if matchesStatus(row, "low-stock") {
				sum.LowStockCount++
			}
			if matchesStatus(row, "out-of-stock") {
				sum.OutOfStockCount++
			}
		}
		// Expired / near-expiry use Nest predicates (stock still on hand)
		if matchesStatus(row, "expired") {
			sum.ExpiredCount++
		}
		if matchesStatus(row, "near-expiry") {
			sum.NearExpiryCount++
		}
	}
	sum.TotalValue = money.FromFloat(sum.TotalValue).AsTotal().Float64()
	sum.LowOrOutCount = sum.LowStockCount + sum.OutOfStockCount
	return sum, nil
}

func (s *Service) GetByItemID(ctx context.Context, shopID, itemID string) (*StockRow, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.fetchAll(ctx, shopID, "", "", true)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].ItemID == itemID {
			return &rows[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}
