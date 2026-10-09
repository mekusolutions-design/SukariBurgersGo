package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
)

func (s *ResolutionService) ListCategories(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, category_id, name, active FROM "MenuCategory"
		WHERE shop_id = $1 ORDER BY name`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, cid, name string
		var active bool
		_ = rows.Scan(&id, &cid, &name, &active)
		out = append(out, map[string]interface{}{
			"id": id, "category_id": cid, "name": name, "active": active, "shop_id": shopID,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func (s *ResolutionService) ListFinishedGoodCategories(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, name, COALESCE(description,''), is_active
		FROM "FinishedGoodCategory" WHERE is_active = true ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, code, name, desc string
		var active bool
		_ = rows.Scan(&id, &code, &name, &desc, &active)
		out = append(out, map[string]interface{}{
			"id": id, "code": code, "name": name, "description": desc, "is_active": active,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func (s *ResolutionService) AssignFinishedGoodCategory(ctx context.Context, itemID, categoryID string) (map[string]interface{}, error) {
	if itemID == "" || categoryID == "" {
		return nil, apperrors.BadRequest("item_id and category_id required")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE "Item" SET finished_good_category_id = $1, updated_at = $2
		WHERE item_id = $3 OR id = $3`, categoryID, time.Now().UTC(), itemID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, apperrors.NotFound("Item not found")
	}
	return map[string]interface{}{"success": true, "item_id": itemID, "finished_good_category_id": categoryID}, nil
}

func (s *ResolutionService) ListFinishedGoodsForMenu(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT i.item_id, i.name, COALESCE(i.unit,''), p.available_stock, p.total_value::text
		FROM "Item" i
		LEFT JOIN "InventoryProjection" p ON p.item_id = i.item_id AND p.shop_id = $1
		WHERE i.finished_good_category_id IS NOT NULL
		ORDER BY i.name`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, name, unit, val string
		var stock *float64
		_ = rows.Scan(&id, &name, &unit, &stock, &val)
		v := 0.0
		if m, e := money.New(val); e == nil {
			v = m.Float64()
		}
		// Dual snake/camel so web Fixed FG + Inventory Select share labels
		stockVal := 0.0
		if stock != nil {
			stockVal = *stock
		}
		row := map[string]interface{}{
			"item_id": id, "itemId": id,
			"name":            name,
			"unit":            unit,
			"available_stock": stockVal, "availableStock": stockVal,
			"total_value": v, "totalValue": v,
		}
		out = append(out, row)
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func (s *ResolutionService) MenuAvailability(ctx context.Context, shopID, menuID string) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	resolved, err := s.ResolveMenuItemLine(ctx, shopID, menuID, 1)
	if err != nil {
		return map[string]interface{}{
			"menu_item_id": menuID, "available": false, "reason": err.Error(),
		}, nil
	}
	avail := true
	reason := ""
	for _, d := range resolved.Deductions {
		var stock float64
		_ = s.pool.QueryRow(ctx, `
			SELECT available_stock FROM "InventoryProjection" WHERE shop_id=$1 AND item_id=$2`,
			shopID, d.ItemID).Scan(&stock)
		if stock < d.Quantity {
			avail = false
			reason = fmt.Sprintf("insufficient %s (need %.4f have %.4f)", d.ItemID, d.Quantity, stock)
			break
		}
	}
	return map[string]interface{}{
		"menu_item_id": menuID, "available": avail, "reason": reason,
		"name": resolved.Name, "selling_price": resolved.UnitPrice,
		"stock_deductions": resolved.Deductions,
	}, nil
}

func (s *ResolutionService) CreateMenuItem(ctx context.Context, shopID, menuItemID, name, productionType, recipeID, stockItemID string, price float64) (*CatalogItem, error) {
	return s.CreateCatalogItem(ctx, CreateCatalogInput{
		ShopID: shopID, MenuItemID: menuItemID, Name: name,
		ProductionType: productionType, RecipeID: recipeID, StockItemID: stockItemID,
		SellingPrice: price,
	})
}

// CreateMenuCategory creates a POS category.
func (s *ResolutionService) CreateMenuCategory(ctx context.Context, shopID, categoryID, name string) (map[string]interface{}, error) {
	if categoryID == "" || name == "" {
		return nil, apperrors.BadRequest("category_id and name required")
	}
	if shopID == "" {
		shopID = "1"
	}
	id := uuid.New().String()
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO "MenuCategory" (id, category_id, shop_id, name, active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,true,$5,$5)
		ON CONFLICT (shop_id, category_id) DO UPDATE SET name = EXCLUDED.name, updated_at = EXCLUDED.updated_at`,
		id, categoryID, shopID, name, now)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id, "category_id": categoryID, "name": name, "shop_id": shopID}, nil
}

// Ensure unused import json if needed
var _ = json.Marshal
