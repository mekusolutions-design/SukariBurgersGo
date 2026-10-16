package waste

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/restflow/api-go/pkg/money"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

func (s *Service) ExpiredCandidates(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT item_id, available_stock, next_expiry_date, days_to_expiry_min, total_value::text
		FROM "InventoryProjection"
		WHERE shop_id = $1 AND (
			(days_to_expiry_min IS NOT NULL AND days_to_expiry_min <= 3)
			OR (next_expiry_date IS NOT NULL AND next_expiry_date <= NOW() + INTERVAL '3 days')
		)
		ORDER BY days_to_expiry_min NULLS LAST`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, val string
		var stock float64
		var exp *time.Time
		var days *int
		_ = rows.Scan(&id, &stock, &exp, &days, &val)
		v := 0.0
		if m, e := money.New(val); e == nil {
			v = m.Float64()
		}
		row := map[string]interface{}{"item_id": id, "available_stock": stock, "total_value": v}
		if exp != nil {
			row["next_expiry_date"] = *exp
		}
		if days != nil {
			row["days_to_expiry_min"] = *days
		}
		out = append(out, row)
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func (s *Service) WriteOffExpired(ctx context.Context, shopID, actorUserID, itemID string, qty float64, idem string) (map[string]interface{}, error) {
	if itemID == "" || qty <= 0 {
		return nil, apperrors.BadRequest("item_id and positive quantity required")
	}
	if idem == "" {
		idem = "writeoff-" + itemID + "-" + uuid.New().String()[:8]
	}
	return s.Record(ctx, RecordInput{
		ShopID: shopID, ActorUserID: actorUserID, ItemID: itemID,
		Quantity: qty, Reason: "expired_write_off", IdempotencyKey: idem,
	})
}

func (s *Service) Events(ctx context.Context, shopID string, limit int, from, to *time.Time) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if to == nil {
		n := time.Now().UTC()
		to = &n
	}
	if from == nil {
		f := to.AddDate(0, 0, -365)
		from = &f
	}
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.item_id, e.quantity, e.total_cost::text, e.unit_cost::text,
		       e.waste_reason, e.waste_photo_url, e.batch_number, e.payload, e.created_at,
		       u.name, u.email
		FROM "Event" e
		LEFT JOIN "users" u ON u.id = e.actor_user_id
		WHERE e.shop_id = $1
		  AND e.event_type IN ('waste_recorded', 'waste', 'WASTE_RECORDED')
		  AND e.created_at >= $2 AND e.created_at <= $3
		ORDER BY e.created_at DESC
		LIMIT $4`, shopID, *from, *to, limit)
	if err != nil {
		return []map[string]interface{}{}, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id string
		var itemID, reason, photo, batch, actorName, actorEmail *string
		var qty *float64
		var totalCostStr, unitCostStr *string
		var payload []byte
		var created time.Time
		if err := rows.Scan(&id, &itemID, &qty, &totalCostStr, &unitCostStr, &reason, &photo, &batch, &payload, &created, &actorName, &actorEmail); err != nil {
			continue
		}
		var p map[string]interface{}
		_ = json.Unmarshal(payload, &p)
		if p == nil {
			p = map[string]interface{}{}
		}
		iid := strOr(itemID, asStr(p["item_id"]))
		name := asStr(p["item_name"])
		if name == "" {
			name = iid
		}
		q := 0.0
		if qty != nil {
			q = *qty
		}
		if q == 0 {
			if v, ok := p["quantity_wasted"].(float64); ok {
				q = v
			}
		}
		val := 0.0
		if totalCostStr != nil {
			if m, e := money.New(*totalCostStr); e == nil {
				val = m.Float64()
			}
		}
		if val == 0 {
			if v, ok := p["total_waste_value"].(float64); ok {
				val = v
			}
		}
		cause := "other"
		if reason != nil && *reason != "" {
			cause = mapWasteCause(*reason)
		} else if v, ok := p["waste_reason"].(string); ok {
			cause = mapWasteCause(v)
		}
		reported := "Unknown"
		if actorName != nil && *actorName != "" {
			reported = *actorName
		} else if actorEmail != nil && *actorEmail != "" {
			reported = *actorEmail
		}
		unit := asStr(p["unit_of_measure"])
		if unit == "" {
			unit = asStr(p["unit"])
		}
		if unit == "" {
			unit = "pcs"
		}
		var photoURL interface{}
		if photo != nil {
			photoURL = *photo
		} else if v, ok := p["waste_photo_url"]; ok {
			photoURL = v
		} else {
			photoURL = nil
		}
		bn := strOr(batch, asStr(p["batch_number"]))
		out = append(out, map[string]interface{}{
			"id": id, "itemId": iid, "itemName": name, "quantity": q, "unit": unit,
			"cause": cause, "value": val, "reportedBy": reported,
			"moduleSource": asStrDefault(p["module_source"], "unknown"),
			"createdAt": created.UTC().Format(time.RFC3339),
			"photoUrl": photoURL, "notes": p["notes"],
			"batchNumber": nullEmpty(bn),
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func strOr(p *string, fallback string) string {
	if p != nil && *p != "" {
		return *p
	}
	return fallback
}
func asStr(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func asStrDefault(v interface{}, d string) string {
	if s := asStr(v); s != "" {
		return s
	}
	return d
}
func nullEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func (s *Service) EventByID(ctx context.Context, shopID, eventID string) (map[string]interface{}, error) {
	events, _ := s.Events(ctx, shopID, 500, nil, nil)
	for _, e := range events {
		if e["event_id"] == eventID {
			return e, nil
		}
	}
	return nil, apperrors.NotFound("Waste event not found")
}
