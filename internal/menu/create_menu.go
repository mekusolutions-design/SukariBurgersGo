package menu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

// CreateMenuInput matches Nest POST /menu multi-component body.
type CreateMenuInput struct {
	ShopID       string
	Name         string
	MenuCode     string
	Category     string
	SellingPrice float64
	TaxRate      float64
	Lines        []CreateMenuLine
	ActorUserID  string
}

type CreateMenuLine struct {
	ComponentKey             string
	ComponentType            string // FIXED | CHOICE | MULTI_CHOICE
	FinishedGoodID           string
	FinishedGoodName         string
	FinishedGoodCategoryID   string
	FinishedGoodCategoryCode string
	FinishedGoodCategoryName string
	OptionItemIDs            []string
	QuantityRequired         float64
	Unit                     string
	MinSelect                int
	MaxSelect                int
	AllowRepeat              bool
}

// CreateMenuEvent appends menu_created with Nest-compatible lines (inventory select OK without FG category).
func (s *ResolutionService) CreateMenuEvent(ctx context.Context, in CreateMenuInput) (map[string]interface{}, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperrors.BadRequest("name is required")
	}
	if len(in.Lines) == 0 {
		return nil, apperrors.BadRequest("at least one menu component line is required")
	}
	if in.ShopID == "" {
		in.ShopID = "1"
	}
	if s.eventStore == nil {
		return nil, apperrors.BadRequest("event store not configured for menu create")
	}

	enriched := make([]map[string]interface{}, 0, len(in.Lines))
	for i, line := range in.Lines {
		ctype := strings.ToUpper(strings.TrimSpace(line.ComponentType))
		if ctype == "" {
			ctype = "FIXED"
		}
		key := strings.TrimSpace(line.ComponentKey)
		if key == "" {
			key = fmt.Sprintf("line_%d", i+1)
		}
		qty := line.QuantityRequired
		if qty <= 0 {
			qty = 1
		}
		unit := line.Unit
		if unit == "" {
			unit = "pcs"
		}

		row := map[string]interface{}{
			"component_key": key, "component_type": ctype,
			"quantity_required": qty, "unit": unit,
			"min_select": line.MinSelect, "max_select": line.MaxSelect,
			"allow_repeat": line.AllowRepeat,
		}

		switch ctype {
		case "CHOICE", "MULTI_CHOICE":
			opts := line.OptionItemIDs
			if opts == nil {
				opts = []string{}
			}
			hasOpts := len(opts) > 0
			hasCat := strings.TrimSpace(line.FinishedGoodCategoryID) != "" ||
				strings.TrimSpace(line.FinishedGoodCategoryCode) != ""
			if !hasOpts && !hasCat {
				return nil, apperrors.BadRequest(
					fmt.Sprintf("Inventory select needs at least one stock item selected, or a valid finished-good category (component %s)", key),
				)
			}
			// Inventory select: explicit SKUs — no FG category required (Nest parity)
			if hasOpts {
				row["option_item_ids"] = opts
			}
			if hasCat {
				row["finished_good_category_id"] = line.FinishedGoodCategoryID
				row["finished_good_category_code"] = line.FinishedGoodCategoryCode
				row["finished_good_category_name"] = line.FinishedGoodCategoryName
			}
			if ctype == "CHOICE" && line.MinSelect == 0 {
				row["min_select"] = 1
			}
			if line.MaxSelect == 0 {
				if ctype == "CHOICE" {
					row["max_select"] = 1
				} else {
					row["max_select"] = qty
				}
			}
		default: // FIXED
			if strings.TrimSpace(line.FinishedGoodID) == "" {
				return nil, apperrors.BadRequest(
					fmt.Sprintf("Fixed component requires finished_good_id (component %s)", key),
				)
			}
			row["finished_good_id"] = line.FinishedGoodID
			row["finished_good_name"] = line.FinishedGoodName
		}
		enriched = append(enriched, row)
	}

	code := strings.TrimSpace(in.MenuCode)
	if code == "" {
		code = strings.ToLower(strings.ReplaceAll(in.Name, " ", "-"))
		if len(code) > 40 {
			code = code[:40]
		}
	}
	menuID := "MENU-" + code
	idem := "menu-create-" + code
	var actor *string
	if in.ActorUserID != "" {
		actor = &in.ActorUserID
	}
	tax := in.TaxRate
	if tax <= 0 {
		tax = 16
	}
	payload := map[string]interface{}{
		"menu_id": menuID, "menu_code": code, "name": in.Name,
		"category": in.Category, "selling_price": in.SellingPrice,
		"tax_rate": tax, "shop_id": in.ShopID, "lines": enriched,
		"is_available": true, "is_visible": true, "status": "active",
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
	if fg := enriched[0]["finished_good_id"]; fg != nil {
		payload["finished_good_id"] = fg
		payload["quantity_required"] = enriched[0]["quantity_required"]
	}

	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventMenuCreated, ActorUserID: actor,
		IdempotencyKey: idem, Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success": true, "menuId": menuID, "menu_id": menuID,
		"eventId": event.ID, "message": "Menu created",
	}, nil
}

// Ensure eventStore field compile-friendly unused import guard
