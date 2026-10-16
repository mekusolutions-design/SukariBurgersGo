package costing

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	pkgcosting "github.com/restflow/api-go/pkg/costing"
	"github.com/restflow/api-go/pkg/money"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// SKU resolves unit cost with Michael / original COG priority:
// 1) on-hand WAC  2) latest production unit cost  3) recipe cost per yield  4) receipt/WAC
func (s *Service) SKU(ctx context.Context, shopID, itemID string) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, apperrors.BadRequest("item_id required")
	}

	var stock float64
	var valStr, avgStr string
	err := s.pool.QueryRow(ctx, `
		SELECT available_stock, COALESCE(total_value,0)::text, COALESCE(avg_unit_cost,0)::text
		FROM "InventoryProjection" WHERE shop_id=$1 AND item_id=$2`, shopID, itemID).
		Scan(&stock, &valStr, &avgStr)

	wac := 0.0
	totalVal := 0.0
	if err == nil {
		if m, e := money.New(avgStr); e == nil {
			wac = m.Float64()
		}
		if m, e := money.New(valStr); e == nil {
			totalVal = m.Float64()
		}
		if wac <= 0 && stock > 0 && totalVal > 0 {
			wac = totalVal / stock
		}
	}

	costSource := "missing"
	unitCost := 0.0

	if wac > 0 && stock > 0 {
		unitCost = wac
		costSource = "inventory_wac"
	}

	if unitCost <= 0 {
		if pc := s.latestProductionUnitCost(ctx, shopID, itemID); pc > 0 {
			unitCost = pc
			costSource = "production"
		}
	}

	recipePerYield := s.recipeCostPerYield(ctx, shopID, itemID)
	if unitCost <= 0 && recipePerYield > 0 {
		unitCost = recipePerYield
		costSource = "recipe_estimate"
	}
	if unitCost <= 0 && wac > 0 {
		unitCost = wac
		costSource = "inventory_wac"
	}

	if unitCost <= 0 && recipePerYield <= 0 && err != nil {
		return nil, apperrors.NotFound("SKU cost not found")
	}

	return map[string]interface{}{
		"item_id": itemID, "itemId": itemID,
		"shop_id": shopID, "shopId": shopID,
		"unit_cost": unitCost, "avgUnitCost": unitCost,
		"available_stock": stock, "availableStock": stock,
		"total_value": totalVal, "totalValue": totalVal,
		"recipe_unit_cost": recipePerYield, "recipeUnitCost": recipePerYield,
		"cost_source": costSource, "costSource": costSource,
	}, nil
}

func (s *Service) latestProductionUnitCost(ctx context.Context, shopID, itemID string) float64 {
	var ucStr *string
	var payload []byte
	err := s.pool.QueryRow(ctx, `
		SELECT unit_cost::text, payload::text
		FROM "Event"
		WHERE shop_id = $1 AND item_id = $2 AND event_type = 'production_finished'
		ORDER BY created_at DESC LIMIT 1`, shopID, itemID).Scan(&ucStr, &payload)
	if err != nil {
		// table might use lowercase event
		_ = s.pool.QueryRow(ctx, `
			SELECT unit_cost::text, payload::text
			FROM event
			WHERE shop_id = $1 AND item_id = $2 AND type = 'production_finished'
			ORDER BY created_at DESC LIMIT 1`, shopID, itemID).Scan(&ucStr, &payload)
	}
	if ucStr != nil && *ucStr != "" {
		if m, e := money.New(*ucStr); e == nil && m.Float64() > 0 {
			return m.Float64()
		}
	}
	return 0
}

// recipeCostPerYield = batchCost / standard_yield (not raw ingredient sum).
func (s *Service) recipeCostPerYield(ctx context.Context, shopID, itemID string) float64 {
	var recipeID string
	var yield float64
	err := s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(standard_yield, 0)
		FROM "Recipe"
		WHERE is_active AND (item_id = $1 OR recipe_code = $1)
		ORDER BY updated_at DESC NULLS LAST LIMIT 1`, itemID).Scan(&recipeID, &yield)
	if err != nil || recipeID == "" {
		return 0
	}
	rows, err := s.pool.Query(ctx, `
		SELECT ri.quantity_per_unit, COALESCE(ri.unit_cost,0)::text, ri.raw_item_id
		FROM "RecipeIngredient" ri WHERE ri.recipe_id = $1`, recipeID)
	if err != nil {
		return 0
	}
	defer rows.Close()
	batch := 0.0
	for rows.Next() {
		var qty float64
		var ucStr, rawID string
		if rows.Scan(&qty, &ucStr, &rawID) != nil {
			continue
		}
		uc := 0.0
		if m, e := money.New(ucStr); e == nil {
			uc = m.Float64()
		}
		if uc <= 0 && rawID != "" {
			var avg string
			_ = s.pool.QueryRow(ctx, `
				SELECT COALESCE(avg_unit_cost,0)::text FROM "InventoryProjection"
				WHERE shop_id=$1 AND item_id=$2`, shopID, rawID).Scan(&avg)
			if m, e := money.New(avg); e == nil {
				uc = m.Float64()
			}
		}
		batch += qty * uc
	}
	if yield <= 0 {
		// try output standard quantity
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(standard_quantity),0) FROM "RecipeOutput"
			WHERE recipe_id = $1 AND COALESCE(is_active,true)`, recipeID).Scan(&yield)
	}
	if batch <= 0 || yield <= 0 {
		return 0
	}
	return pkgcosting.RoundMoney6(batch / yield)
}
