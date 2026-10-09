package menu

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
)

type CatalogItem struct {
	ID             string  `json:"id"`
	MenuItemID     string  `json:"menu_item_id"`
	ShopID         string  `json:"shop_id"`
	Name           string  `json:"name"`
	ProductionType string  `json:"production_type"`
	RecipeID       *string `json:"recipe_id"`
	StockItemID    *string `json:"stock_item_id"`
	SellingPrice   float64 `json:"selling_price"`
	Active         bool    `json:"active"`
}

func (s *ResolutionService) ListCatalog(ctx context.Context, shopID string) ([]CatalogItem, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, menu_item_id, shop_id, name, production_type::text, recipe_id, stock_item_id, selling_price, active
		FROM "CatalogMenuItem" WHERE shop_id = $1 ORDER BY name`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CatalogItem
	for rows.Next() {
		var c CatalogItem
		var priceStr string
		if err := rows.Scan(&c.ID, &c.MenuItemID, &c.ShopID, &c.Name, &c.ProductionType, &c.RecipeID, &c.StockItemID, &priceStr, &c.Active); err != nil {
			return nil, err
		}
		if m, e := money.New(priceStr); e == nil {
			c.SellingPrice = m.Float64()
		}
		out = append(out, c)
	}
	if out == nil {
		out = []CatalogItem{}
	}
	return out, rows.Err()
}

type CreateCatalogInput struct {
	ShopID, MenuItemID, Name, ProductionType, RecipeID, StockItemID string
	SellingPrice                                                    float64
}

func (s *ResolutionService) CreateCatalogItem(ctx context.Context, in CreateCatalogInput) (*CatalogItem, error) {
	if in.MenuItemID == "" || in.Name == "" {
		return nil, apperrors.BadRequest("menu_item_id and name required")
	}
	if in.ShopID == "" {
		in.ShopID = "1"
	}
	if in.ProductionType == "" {
		in.ProductionType = "stocked"
	}
	id := uuid.New().String()
	now := time.Now().UTC()
	price := money.FromFloat(in.SellingPrice).String()
	var recipeID, stockID *string
	if in.RecipeID != "" {
		recipeID = &in.RecipeID
	}
	if in.StockItemID != "" {
		stockID = &in.StockItemID
	}
	const q = `INSERT INTO "CatalogMenuItem" (id, menu_item_id, shop_id, name, production_type, recipe_id, stock_item_id, selling_price, active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5::"MenuProductionType",$6,$7,$8,true,$9,$9)
		ON CONFLICT (shop_id, menu_item_id) DO UPDATE SET name = EXCLUDED.name, production_type = EXCLUDED.production_type,
			recipe_id = EXCLUDED.recipe_id, stock_item_id = EXCLUDED.stock_item_id, selling_price = EXCLUDED.selling_price, updated_at = EXCLUDED.updated_at
		RETURNING id, menu_item_id, shop_id, name, production_type::text, recipe_id, stock_item_id, selling_price, active`
	var c CatalogItem
	var priceOut string
	err := s.pool.QueryRow(ctx, q, id, in.MenuItemID, in.ShopID, in.Name, in.ProductionType, recipeID, stockID, price, now).
		Scan(&c.ID, &c.MenuItemID, &c.ShopID, &c.Name, &c.ProductionType, &c.RecipeID, &c.StockItemID, &priceOut, &c.Active)
	if err != nil {
		return nil, fmt.Errorf("create catalog item: %w", err)
	}
	if m, e := money.New(priceOut); e == nil {
		c.SellingPrice = m.Float64()
	}
	return &c, nil
}

type ComboView struct {
	ID           string           `json:"id"`
	ComboID      string           `json:"combo_id"`
	ShopID       string           `json:"shop_id"`
	Name         string           `json:"name"`
	SellingPrice float64          `json:"selling_price"`
	Active       bool             `json:"active"`
	Groups       []ComboGroupView `json:"groups"`
}

type ComboGroupView struct {
	GroupIndex     int    `json:"group_index"`
	MenuCategoryID string `json:"menu_category_id"`
	Quantity       int    `json:"quantity"`
}

func (s *ResolutionService) ListCombos(ctx context.Context, shopID string) ([]ComboView, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, combo_id, shop_id, name, selling_price, active FROM "Combo"
		WHERE shop_id = $1 ORDER BY name`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ComboView
	for rows.Next() {
		var c ComboView
		var priceStr string
		if err := rows.Scan(&c.ID, &c.ComboID, &c.ShopID, &c.Name, &priceStr, &c.Active); err != nil {
			return nil, err
		}
		if m, e := money.New(priceStr); e == nil {
			c.SellingPrice = m.Float64()
		}
		grows, err := s.pool.Query(ctx, `
			SELECT group_index, menu_category_id, quantity FROM "ComboSelectionGroup"
			WHERE combo_id = $1 ORDER BY group_index`, c.ID)
		if err == nil {
			for grows.Next() {
				var g ComboGroupView
				_ = grows.Scan(&g.GroupIndex, &g.MenuCategoryID, &g.Quantity)
				c.Groups = append(c.Groups, g)
			}
			grows.Close()
		}
		if c.Groups == nil {
			c.Groups = []ComboGroupView{}
		}
		out = append(out, c)
	}
	if out == nil {
		out = []ComboView{}
	}
	return out, rows.Err()
}

type CreateComboInput struct {
	ShopID, ComboID, Name string
	SellingPrice          float64
	Groups                []ComboGroupView
}

func (s *ResolutionService) CreateCombo(ctx context.Context, in CreateComboInput) (*ComboView, error) {
	if in.ComboID == "" || in.Name == "" {
		return nil, apperrors.BadRequest("combo_id and name required")
	}
	if in.ShopID == "" {
		in.ShopID = "1"
	}
	id := uuid.New().String()
	now := time.Now().UTC()
	price := money.FromFloat(in.SellingPrice).String()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
		INSERT INTO "Combo" (id, combo_id, shop_id, name, selling_price, active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,true,$6,$6)
		ON CONFLICT (shop_id, combo_id) DO UPDATE SET name = EXCLUDED.name, selling_price = EXCLUDED.selling_price, updated_at = EXCLUDED.updated_at`,
		id, in.ComboID, in.ShopID, in.Name, price, now)
	if err != nil {
		return nil, err
	}
	// resolve id after upsert
	var comboUUID string
	_ = tx.QueryRow(ctx, `SELECT id FROM "Combo" WHERE shop_id = $1 AND combo_id = $2`, in.ShopID, in.ComboID).Scan(&comboUUID)
	for _, g := range in.Groups {
		gid := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO "ComboSelectionGroup" (id, combo_id, group_index, menu_category_id, quantity)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (combo_id, group_index) DO UPDATE SET menu_category_id = EXCLUDED.menu_category_id, quantity = EXCLUDED.quantity`,
			gid, comboUUID, g.GroupIndex, g.MenuCategoryID, g.Quantity)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &ComboView{ID: comboUUID, ComboID: in.ComboID, ShopID: in.ShopID, Name: in.Name, SellingPrice: in.SellingPrice, Active: true, Groups: in.Groups}, nil
}
