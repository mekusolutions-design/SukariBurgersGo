package recipe

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/pkg/money"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

type Recipe struct {
	ID            string   `json:"id"`
	RecipeCode    string   `json:"recipe_code"`
	ItemID        string   `json:"item_id"`
	ItemName      string   `json:"item_name"`
	StandardYield float64  `json:"standard_yield"`
	Unit          string   `json:"unit"`
	Notes         string   `json:"notes,omitempty"`
	Category      string   `json:"category,omitempty"`
	RecipeType    string   `json:"recipe_type"`
	YieldQuantity *float64 `json:"yield_quantity,omitempty"`
	YieldUnit     string   `json:"yield_unit,omitempty"`
	YieldBasis    string   `json:"yield_basis"`
	IsActive      bool     `json:"is_active"`
}

type Ingredient struct {
	ID              string  `json:"id"`
	RecipeID        string  `json:"recipe_id"`
	RawItemID       string  `json:"raw_item_id"`
	RawItemName     string  `json:"raw_item_name"`
	QuantityPerUnit float64 `json:"quantity_per_unit"`
	Unit            string  `json:"unit"`
	UnitCost        float64 `json:"unit_cost"`
}

func (s *Service) List(ctx context.Context, activeOnly bool) ([]Recipe, error) {
	q := `SELECT id, recipe_code, item_id, item_name, standard_yield, unit, COALESCE(notes,''), COALESCE(category,''),
		recipe_type::text, yield_quantity, COALESCE(yield_unit,''), yield_basis::text, is_active
		FROM "Recipe"`
	if activeOnly {
		q += ` WHERE is_active = true`
	}
	q += ` ORDER BY item_name`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recipe
	for rows.Next() {
		var r Recipe
		if err := rows.Scan(&r.ID, &r.RecipeCode, &r.ItemID, &r.ItemName, &r.StandardYield, &r.Unit,
			&r.Notes, &r.Category, &r.RecipeType, &r.YieldQuantity, &r.YieldUnit, &r.YieldBasis, &r.IsActive); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if out == nil {
		out = []Recipe{}
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, id string) (*Recipe, []Ingredient, error) {
	const q = `SELECT id, recipe_code, item_id, item_name, standard_yield, unit, COALESCE(notes,''), COALESCE(category,''),
		recipe_type::text, yield_quantity, COALESCE(yield_unit,''), yield_basis::text, is_active
		FROM "Recipe" WHERE id = $1 OR recipe_code = $1`
	var r Recipe
	err := s.pool.QueryRow(ctx, q, id).Scan(&r.ID, &r.RecipeCode, &r.ItemID, &r.ItemName, &r.StandardYield, &r.Unit,
		&r.Notes, &r.Category, &r.RecipeType, &r.YieldQuantity, &r.YieldUnit, &r.YieldBasis, &r.IsActive)
	if err != nil {
		return nil, nil, apperrors.NotFound("Recipe not found")
	}
	const iq = `SELECT id, recipe_id, raw_item_id, raw_item_name, quantity_per_unit, unit, unit_cost
		FROM "RecipeIngredient" WHERE recipe_id = $1`
	rows, err := s.pool.Query(ctx, iq, r.ID)
	if err != nil {
		return &r, nil, err
	}
	defer rows.Close()
	var ings []Ingredient
	for rows.Next() {
		var i Ingredient
		var uc *string
		if err := rows.Scan(&i.ID, &i.RecipeID, &i.RawItemID, &i.RawItemName, &i.QuantityPerUnit, &i.Unit, &uc); err != nil {
			return &r, nil, err
		}
		if uc != nil {
			if m, e := money.New(*uc); e == nil {
				i.UnitCost = m.Float64()
			}
		}
		ings = append(ings, i)
	}
	if ings == nil {
		ings = []Ingredient{}
	}
	return &r, ings, nil
}

type CreateInput struct {
	RecipeCode    string
	ItemID        string
	ItemName      string
	StandardYield float64
	Unit          string
	Notes         string
	Category      string
	RecipeType    string
	YieldBasis    string
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*Recipe, error) {
	if in.RecipeCode == "" || in.ItemID == "" {
		return nil, apperrors.BadRequest("recipe_code and item_id required")
	}
	if in.RecipeType == "" {
		in.RecipeType = "SINGLE_OUTPUT"
	}
	if in.YieldBasis == "" {
		in.YieldBasis = "BATCH"
	}
	if in.Unit == "" {
		in.Unit = "unit"
	}
	id := uuid.New().String()
	now := time.Now().UTC()
	const q = `INSERT INTO "Recipe" (id, recipe_code, item_id, item_name, standard_yield, unit, notes, category, recipe_type, yield_basis, is_active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::"RecipeType",$10::"YieldBasis",true,$11,$11)
		RETURNING id, recipe_code, item_id, item_name, standard_yield, unit, COALESCE(notes,''), COALESCE(category,''), recipe_type::text, yield_quantity, COALESCE(yield_unit,''), yield_basis::text, is_active`
	var r Recipe
	err := s.pool.QueryRow(ctx, q, id, in.RecipeCode, in.ItemID, in.ItemName, in.StandardYield, in.Unit, in.Notes, in.Category, in.RecipeType, in.YieldBasis, now).
		Scan(&r.ID, &r.RecipeCode, &r.ItemID, &r.ItemName, &r.StandardYield, &r.Unit, &r.Notes, &r.Category, &r.RecipeType, &r.YieldQuantity, &r.YieldUnit, &r.YieldBasis, &r.IsActive)
	if err != nil {
		return nil, fmt.Errorf("create recipe: %w", err)
	}
	return &r, nil
}

func (s *Service) AddIngredient(ctx context.Context, recipeID, rawItemID, rawItemName string, qty float64, unit string, unitCost float64) (*Ingredient, error) {
	if qty <= 0 {
		return nil, apperrors.BadRequest("quantity_per_unit must be positive")
	}
	id := uuid.New().String()
	uc := money.FromFloat(unitCost).String()
	const q = `INSERT INTO "RecipeIngredient" (id, recipe_id, raw_item_id, raw_item_name, quantity_per_unit, unit, unit_cost)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (recipe_id, raw_item_id) DO UPDATE SET quantity_per_unit = EXCLUDED.quantity_per_unit, unit = EXCLUDED.unit, unit_cost = EXCLUDED.unit_cost
		RETURNING id, recipe_id, raw_item_id, raw_item_name, quantity_per_unit, unit, unit_cost`
	var i Ingredient
	var ucOut *string
	err := s.pool.QueryRow(ctx, q, id, recipeID, rawItemID, rawItemName, qty, unit, uc).
		Scan(&i.ID, &i.RecipeID, &i.RawItemID, &i.RawItemName, &i.QuantityPerUnit, &i.Unit, &ucOut)
	if err != nil {
		return nil, fmt.Errorf("add ingredient: %w", err)
	}
	if ucOut != nil {
		if m, e := money.New(*ucOut); e == nil {
			i.UnitCost = m.Float64()
		}
	}
	return &i, nil
}

// Cost computes recipe cost for a given batch multiplier.
func (s *Service) Cost(ctx context.Context, recipeID string, batches float64) (float64, error) {
	if batches <= 0 {
		batches = 1
	}
	_, ings, err := s.Get(ctx, recipeID)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, i := range ings {
		uc := i.UnitCost
		if uc <= 0 {
			// fallback inventory average
			const q = `SELECT CASE WHEN available_stock > 0 THEN total_value / available_stock ELSE 0 END
				FROM "InventoryProjection" WHERE item_id = $1 LIMIT 1`
			_ = s.pool.QueryRow(ctx, q, i.RawItemID).Scan(&uc)
		}
		total += i.QuantityPerUnit * batches * uc
	}
	return total, nil
}


func (s *Service) ListCategories(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT COALESCE(category, 'uncategorized') AS category
		FROM "Recipe"
		WHERE ($1 = '' OR shop_id = $1 OR shop_id IS NULL)
		ORDER BY 1`, shopID)
	if err != nil {
		// Recipe may not have shop_id — fallback
		rows, err = s.pool.Query(ctx, `SELECT DISTINCT COALESCE(category, 'uncategorized') FROM "Recipe" ORDER BY 1`)
		if err != nil {
			return []map[string]interface{}{}, nil
		}
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			out = append(out, map[string]interface{}{"id": c, "name": c})
		}
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func (s *Service) GetByItemID(ctx context.Context, itemID string) (map[string]interface{}, error) {
	var id, code, name string
	err := s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(recipe_code,''), COALESCE(item_name, '')
		FROM "Recipe" WHERE item_id = $1 OR recipe_code = $1
		ORDER BY created_at DESC LIMIT 1`, itemID).Scan(&id, &code, &name)
	if err != nil {
		return nil, apperrors.NotFound("recipe not found for item")
	}
	return map[string]interface{}{"id": id, "recipe_code": code, "item_id": itemID, "name": name}, nil
}

func (s *Service) Availability(ctx context.Context, shopID, recipeID string) (map[string]interface{}, error) {
	// Max batches from ingredient stock / qty_per_unit
	rows, err := s.pool.Query(ctx, `
		SELECT ri.raw_item_id, ri.quantity_per_unit,
		       COALESCE(ip.available_stock, 0)
		FROM "RecipeIngredient" ri
		LEFT JOIN "InventoryProjection" ip
		  ON ip.item_id = ri.raw_item_id AND ip.shop_id = $1
		WHERE ri.recipe_id = $2`, shopID, recipeID)
	if err != nil {
		return map[string]interface{}{"recipeId": recipeID, "maxBatches": 0, "available": false}, nil
	}
	defer rows.Close()
	maxB := -1.0
	for rows.Next() {
		var rawID string
		var need, stock float64
		if rows.Scan(&rawID, &need, &stock) != nil {
			continue
		}
		if need <= 0 {
			continue
		}
		b := stock / need
		if maxB < 0 || b < maxB {
			maxB = b
		}
	}
	if maxB < 0 {
		maxB = 0
	}
	return map[string]interface{}{
		"recipeId": recipeID, "maxBatches": int(maxB), "available": maxB >= 1,
	}, nil
}


func (s *Service) Update(ctx context.Context, id string, body map[string]interface{}) (map[string]interface{}, error) {
	// Minimal field updates for Nest PUT /recipe/:id parity
	name, _ := body["item_name"].(string)
	if name == "" {
		name, _ = body["itemName"].(string)
	}
	notes, _ := body["notes"].(string)
	cat, _ := body["category"].(string)
	_, err := s.pool.Exec(ctx, `
		UPDATE "Recipe" SET
			item_name = COALESCE(NULLIF($2,''), item_name),
			notes = COALESCE(NULLIF($3,''), notes),
			category = COALESCE(NULLIF($4,''), category),
			updated_at = NOW()
		WHERE id = $1 OR recipe_code = $1`, id, name, notes, cat)
	if err != nil {
		return nil, apperrors.Internal("failed to update recipe")
	}
	rec, _, err := s.Get(ctx, id)
	if err != nil {
		return map[string]interface{}{"id": id, "success": true}, nil
	}
	return map[string]interface{}{"id": rec.ID, "recipe_code": rec.RecipeCode, "item_name": rec.ItemName, "success": true}, nil
}

func (s *Service) AddOutput(ctx context.Context, recipeID, itemID, itemName string, qty float64, unit string) (map[string]interface{}, error) {
	if itemID == "" {
		return nil, apperrors.BadRequest("item_id required")
	}
	id := uuid.New().String()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO "RecipeOutput" (id, recipe_id, item_id, item_name, standard_quantity, unit, is_default, is_active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,false,true,NOW(),NOW())
		ON CONFLICT DO NOTHING`, id, recipeID, itemID, itemName, qty, unit)
	if err != nil {
		// Table may not exist — store as event-less no-op response with payload for clients
		return map[string]interface{}{
			"id": id, "recipe_id": recipeID, "item_id": itemID, "item_name": itemName,
			"quantity": qty, "unit": unit, "persisted": false,
		}, nil
	}
	return map[string]interface{}{
		"id": id, "recipe_id": recipeID, "item_id": itemID, "item_name": itemName,
		"quantity": qty, "unit": unit, "persisted": true,
	}, nil
}
