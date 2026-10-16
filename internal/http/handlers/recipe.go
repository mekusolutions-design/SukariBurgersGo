package handlers

import (
	"net/http"
	"strconv"

	"github.com/restflow/api-go/internal/auth"
	"github.com/restflow/api-go/internal/recipe"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

type RecipeHandler struct {
	svc *recipe.Service
}

func NewRecipeHandler(svc *recipe.Service) *RecipeHandler {
	return &RecipeHandler{svc: svc}
}

func (h *RecipeHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.List(r.Context(), r.URL.Query().Get("active") != "false")
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.Internal("failed to list recipes"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items, "total": len(items)})
}

func (h *RecipeHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("recipeId")
	rec, ings, err := h.svc.Get(r.Context(), id)
	if err != nil {
		// Nest clients may pass business item_id; fall back to item/code lookup
		rec2, err2 := h.svc.GetByItemID(r.Context(), id)
		if err2 != nil {
			apperrors.WriteJSON(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, rec2)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"recipe": rec, "ingredients": ings})
}

func (h *RecipeHandler) Create(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	rec, err := h.svc.Create(r.Context(), recipe.CreateInput{
		RecipeCode: mapString(m, "recipe_code", "recipeCode"),
		ItemID: mapString(m, "item_id", "itemId"),
		ItemName: mapString(m, "item_name", "itemName", "name"),
		StandardYield: mapFloat(m, "standard_yield", "standardYield"),
		Unit: mapString(m, "unit"),
		Notes: mapString(m, "notes"),
		Category: mapString(m, "category"),
		RecipeType: mapString(m, "recipe_type", "recipeType"),
		YieldBasis: mapString(m, "yield_basis", "yieldBasis"),
	})
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

func (h *RecipeHandler) AddIngredient(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	recipeID := mapString(m, "recipe_id", "recipeId")
	if recipeID == "" {
		recipeID = r.PathValue("recipeId")
	}
	ing, err := h.svc.AddIngredient(
		r.Context(), recipeID,
		mapString(m, "raw_item_id", "rawItemId", "item_id", "itemId"),
		mapString(m, "raw_item_name", "rawItemName", "item_name", "itemName"),
		mapFloat(m, "quantity_per_unit", "quantityPerUnit", "quantity"),
		mapString(m, "unit"),
		mapFloat(m, "unit_cost", "unitCost"),
	)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, ing)
}

func (h *RecipeHandler) Cost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("recipeId")
	batches, _ := strconv.ParseFloat(r.URL.Query().Get("batches"), 64)
	cost, err := h.svc.Cost(r.Context(), id, batches)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"recipe_id": id, "batches": batches, "total_cost": cost,
	})
}

func (h *RecipeHandler) Categories(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.ListCategories(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": rows})
}

func (h *RecipeHandler) ByItem(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("itemId")
	rec, err := h.svc.GetByItemID(r.Context(), itemID)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *RecipeHandler) Availability(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("recipeId")
	shopID := auth.ShopIDFromContext(r.Context())
	out, err := h.svc.Availability(r.Context(), shopID, id)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
