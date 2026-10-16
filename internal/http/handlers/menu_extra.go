package handlers

import (
	"fmt"
	"net/http"

	"github.com/restflow/api-go/internal/auth"
	"github.com/restflow/api-go/internal/menu"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

func (h *MenuHandler) ListMenus(w http.ResponseWriter, r *http.Request) {
	// Match Nest's @CurrentShop behavior; query strings cannot switch tenants.
	shopID := auth.ShopIDFromContext(r.Context())
	if shopID == "" {
		apperrors.WriteJSON(w, r, apperrors.Forbidden("shop identity required"))
		return
	}
	out, err := h.svc.ListMenus(r.Context(), shopID)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	// Nest listMenus shape: { success, items, count }
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"items":   out,
		"menus":   out,
		"count":   len(out),
	})
}

func (h *MenuHandler) ListCategories(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListCategories(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}

func (h *MenuHandler) ListFGCategories(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListFinishedGoodCategories(r.Context())
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}

func (h *MenuHandler) AssignFGCategory(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	out, err := h.svc.AssignFinishedGoodCategory(r.Context(),
		mapString(m, "item_id", "itemId"),
		mapString(m, "category_id", "categoryId", "finished_good_category_id"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *MenuHandler) ListFinishedGoods(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListFinishedGoodsForMenu(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}

func (h *MenuHandler) Availability(w http.ResponseWriter, r *http.Request) {
	menuID := r.PathValue("menuId")
	if menuID == "" {
		menuID = r.URL.Query().Get("menu_id")
	}
	out, err := h.svc.MenuAvailability(r.Context(), auth.ShopIDFromContext(r.Context()), menuID)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *MenuHandler) CreateMenu(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}

	// Nest multi-component menu (web Catalog create) — POS/Go path
	if raw, ok := m["lines"].([]interface{}); ok && len(raw) > 0 {
		lines := make([]menu.CreateMenuLine, 0, len(raw))
		for i, e := range raw {
			lm, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			line := menu.CreateMenuLine{
				ComponentKey:             mapString(lm, "component_key", "componentKey"),
				ComponentType:            mapString(lm, "component_type", "componentType"),
				FinishedGoodID:           mapString(lm, "finished_good_id", "finishedGoodId"),
				FinishedGoodName:         mapString(lm, "finished_good_name", "finishedGoodName"),
				FinishedGoodCategoryID:   mapString(lm, "finished_good_category_id", "finishedGoodCategoryId"),
				FinishedGoodCategoryCode: mapString(lm, "finished_good_category_code", "finishedGoodCategoryCode"),
				FinishedGoodCategoryName: mapString(lm, "finished_good_category_name", "finishedGoodCategoryName"),
				QuantityRequired:         mapFloat(lm, "quantity_required", "quantityRequired"),
				Unit:                     mapString(lm, "unit"),
				MinSelect:                int(mapFloat(lm, "min_select", "minSelect")),
				MaxSelect:                int(mapFloat(lm, "max_select", "maxSelect")),
				AllowRepeat:              mapBool(lm, "allow_repeat", "allowRepeat"),
			}
			if line.ComponentKey == "" {
				line.ComponentKey = fmt.Sprintf("line_%d", i+1)
			}
			if arr, ok := lm["option_item_ids"].([]interface{}); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok && s != "" {
						line.OptionItemIDs = append(line.OptionItemIDs, s)
					}
				}
			}
			if arr, ok := lm["optionItemIds"].([]interface{}); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok && s != "" {
						line.OptionItemIDs = append(line.OptionItemIDs, s)
					}
				}
			}
			// option_items objects → extract ids
			if arr, ok := lm["option_items"].([]interface{}); ok {
				for _, v := range arr {
					if om, ok := v.(map[string]interface{}); ok {
						id := mapString(om, "item_id", "itemId", "finished_good_id", "finishedGoodId")
						if id != "" {
							line.OptionItemIDs = append(line.OptionItemIDs, id)
						}
					}
				}
			}
			lines = append(lines, line)
		}
		out, err := h.svc.CreateMenuEvent(r.Context(), menu.CreateMenuInput{
			ShopID:       auth.ShopIDFromContext(r.Context()),
			Name:         mapString(m, "name"),
			MenuCode:     mapString(m, "menu_code", "menuCode"),
			Category:     mapString(m, "category"),
			SellingPrice: mapFloat(m, "selling_price", "sellingPrice", "price"),
			TaxRate:      mapFloat(m, "tax_rate", "taxRate"),
			Lines:        lines,
			ActorUserID:  auth.UserIDFromContext(r.Context()),
		})
		if err != nil {
			apperrors.WriteJSON(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
		return
	}

	// Legacy catalog-style create
	item, err := h.svc.CreateMenuItem(r.Context(), auth.ShopIDFromContext(r.Context()),
		mapString(m, "menu_item_id", "menuItemId", "id"),
		mapString(m, "name"),
		mapString(m, "production_type", "productionType"),
		mapString(m, "recipe_id", "recipeId"),
		mapString(m, "stock_item_id", "stockItemID", "stockItemId"),
		mapFloat(m, "selling_price", "sellingPrice", "price"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *MenuHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	out, err := h.svc.CreateMenuCategory(r.Context(), auth.ShopIDFromContext(r.Context()),
		mapString(m, "category_id", "categoryId", "id"),
		mapString(m, "name"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
