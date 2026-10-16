package production

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/internal/eventstore"
	apperrors "github.com/restflow/api-go/pkg/errors"
	"github.com/restflow/api-go/pkg/costing"
	"github.com/restflow/api-go/pkg/money"
)

type Service struct {
	pool       *pgxpool.Pool
	eventStore *eventstore.Store
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store, _ interface{}) *Service {
	return &Service{pool: pool, eventStore: es}
}

// NewServiceCompat matches main.go NewService(pool, eventStore, resolveSvc)
func NewServiceWithResolver(pool *pgxpool.Pool, es *eventstore.Store, _ interface{}) *Service {
	return NewService(pool, es, nil)
}

type StartInput struct {
	ShopID, ActorUserID, RecipeID, RecipeName, IdempotencyKey string
	BatchSize float64
}

type Line struct {
	ItemID           string  `json:"item_id"`
	ItemName         string  `json:"item_name"`
	Unit             string  `json:"unit"`
	WeightUnit       string  `json:"weight_unit,omitempty"`
	ActualQuantity   float64 `json:"actual_quantity"`
	StandardQuantity float64 `json:"standard_quantity,omitempty"`
	UnitCost         float64 `json:"unit_cost,omitempty"`
	UnitWeight       float64 `json:"unit_weight,omitempty"`
	UsableQuantity   float64 `json:"usable_quantity,omitempty"`
}

type FinishInput struct {
	ShopID, ActorUserID, ProductionID, ItemID, ItemName string
	RecipeID                                            string
	BatchNumber, ExpiryDate, WasteReason                string
	PlannedQty, ActualQty, WasteQty, UnitCost, TotalCost float64
	Inputs, Outputs                                     []Line
	Raw                                                 map[string]interface{}
}

func TodayUTC() string {
	return time.Now().UTC().Format("2006-01-02")
}

func (s *Service) Start(ctx context.Context, in StartInput) (map[string]interface{}, error) {
	if in.ShopID == "" {
		in.ShopID = "1"
	}
	if in.BatchSize <= 0 {
		in.BatchSize = 1
	}
	prodID := fmt.Sprintf("PROD-%s-%s", in.RecipeID, time.Now().UTC().Format("20060102-150405"))
	if in.RecipeID == "" {
		prodID = "PROD-" + uuid.New().String()[:8]
	}
	idem := in.IdempotencyKey
	if idem == "" {
		idem = "prod-start-" + prodID
	}
	var actorPtr *string
	if in.ActorUserID != "" {
		actorPtr = &in.ActorUserID
	}
	itemPtr := &in.RecipeID
	qty := in.BatchSize
	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType:      eventstore.EventProductionStarted,
		ActorUserID:    actorPtr,
		IdempotencyKey: idem,
		ItemID:         itemPtr,
		Quantity:       &qty,
		Payload: map[string]interface{}{
			"production_id": prodID,
			"productionId":  prodID,
			"recipe_id":     in.RecipeID,
			"recipe_name":   in.RecipeName,
			"batch_size":    in.BatchSize,
			"shop_id":       in.ShopID,
			"started_at":    time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success": true, "productionId": prodID, "production_id": prodID, "eventId": event.ID,
		"message": "Production started",
	}, nil
}

func (s *Service) Finish(ctx context.Context, in FinishInput) (map[string]interface{}, error) {
	if in.ProductionID == "" {
		return nil, apperrors.BadRequest("production_id required")
	}
	if in.ShopID == "" {
		in.ShopID = "1"
	}
	// Nest resolveLines: when inputs/outputs incomplete, scale from recipe
	in = s.resolveLines(ctx, in)

	// Michael: multi-output header yield = mass in recipe unit when unit_weight set
	// (not raw piece sum). Per-output lines keep piece counts for stock.
	headerQty, massGrams, fromMass := normalizeHeaderYield(in)
	if headerQty > 0 {
		in.ActualQty = headerQty
	}

	if in.ActualQty <= 0 && len(in.Outputs) == 0 {
		return nil, apperrors.BadRequest("Provide actual_quantity_produced and/or outputs[] with quantities")
	}

	idem := "prod-finish-" + in.ProductionID
	var actorPtr *string
	if in.ActorUserID != "" {
		actorPtr = &in.ActorUserID
	}

	// Resolve output item
	outputItemID := in.ItemID
	if outputItemID == "" && len(in.Outputs) > 0 {
		outputItemID = in.Outputs[0].ItemID
	}

	// Enrich input unit costs from inventory WAC when missing
	for i := range in.Inputs {
		if in.Inputs[i].ItemID == "" || in.Inputs[i].UnitCost > 0 {
			continue
		}
		var avgStr string
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(avg_unit_cost,0)::text FROM "InventoryProjection"
			WHERE shop_id=$1 AND item_id=$2`, in.ShopID, in.Inputs[i].ItemID).Scan(&avgStr)
		if m, e := money.New(avgStr); e == nil && m.Float64() > 0 {
			in.Inputs[i].UnitCost = m.Float64()
		}
	}

	// Build stock_deductions from inputs (Nest deducts actual ingredient qty)
	var deductions []map[string]interface{}
	totalInputCost := 0.0
	for _, line := range in.Inputs {
		if line.ItemID == "" || line.ActualQuantity <= 0 {
			continue
		}
		lineTotal := line.UnitCost * line.ActualQuantity
		totalInputCost += lineTotal
		d := map[string]interface{}{
			"item_id": line.ItemID, "item_name": line.ItemName,
			"quantity": line.ActualQuantity, "unit": line.Unit,
		}
		if line.UnitCost > 0 {
			d["unit_cost"] = line.UnitCost
			d["total_cost"] = lineTotal
		}
		deductions = append(deductions, d)
	}

	// Michael / Oct 3: good = actual yield to stock; waste = field only; no double subtract
	goodQty := in.ActualQty
	if goodQty < 0 {
		goodQty = 0
	}
	wasteQty := in.WasteQty
	if wasteQty < 0 {
		wasteQty = 0
	}
	totalMade := goodQty + wasteQty
	productionCost := costing.ResolveProductionCost(totalInputCost, in.TotalCost, in.UnitCost, totalMade)
	_, unitCostVal, toStockValue, wasteValue, unaccounted, reconOK := costing.FinishCostMath(
		goodQty, wasteQty, productionCost, in.PlannedQty,
	)
	in.UnitCost = unitCostVal
	in.TotalCost = toStockValue
	usable := goodQty // stock receives good only

	qty := usable
	var itemPtr *string
	if outputItemID != "" {
		itemPtr = &outputItemID
	}
	var unitCostPtr, totalCostPtr *money.Money
	if unitCostVal > 0 {
		m := money.FromFloatUnitCost(unitCostVal)
		unitCostPtr = &m
	}
	if toStockValue > 0 {
		m := money.FromFloat(toStockValue)
		totalCostPtr = &m
	}
	// Serialize lines as snake_case maps so projection reads actual_quantity
	inputMaps := make([]map[string]interface{}, 0, len(in.Inputs))
	for _, line := range in.Inputs {
		if line.ItemID == "" {
			continue
		}
		m := map[string]interface{}{
			"item_id": line.ItemID, "itemId": line.ItemID,
			"item_name": line.ItemName, "itemName": line.ItemName,
			"actual_quantity": line.ActualQuantity, "quantity": line.ActualQuantity,
			"unit": line.Unit, "unit_cost": line.UnitCost,
		}
		if line.UnitCost > 0 && line.ActualQuantity > 0 {
			m["total_cost"] = line.UnitCost * line.ActualQuantity
		}
		inputMaps = append(inputMaps, m)
	}
	outputMaps := make([]map[string]interface{}, 0, len(in.Outputs))
	for _, line := range in.Outputs {
		if line.ItemID == "" {
			continue
		}
		uq := line.UsableQuantity
		if uq <= 0 {
			uq = line.ActualQuantity
		}
		// Primary output: apply production waste to usable
		if line.ItemID == outputItemID || len(in.Outputs) == 1 {
			uq = usable
			if uq <= 0 && line.ActualQuantity > 0 && in.WasteQty <= 0 {
				uq = line.ActualQuantity
			}
		}
		m := map[string]interface{}{
			"item_id": line.ItemID, "itemId": line.ItemID,
			"item_name": line.ItemName, "itemName": line.ItemName,
			"actual_quantity": line.ActualQuantity,
			"usable_quantity": uq, "quantity": uq,
			"unit": line.Unit, "unit_cost": unitCostVal,
			"line_cost": uq * unitCostVal,
			"unit_weight": line.UnitWeight, "weight_unit": line.WeightUnit,
		}
		outputMaps = append(outputMaps, m)
	}
	// If no outputs array, synthesize primary FG output for projection
	if len(outputMaps) == 0 && outputItemID != "" && usable > 0 {
		outputMaps = append(outputMaps, map[string]interface{}{
			"item_id": outputItemID, "item_name": in.ItemName,
			"actual_quantity": in.ActualQty, "usable_quantity": usable, "quantity": usable,
			"unit_cost": in.UnitCost,
		})
	}

	yieldPct := 100.0
	if in.PlannedQty > 0 && in.ActualQty > 0 {
		yieldPct = (in.ActualQty / in.PlannedQty) * 100
	}
	var yieldVar interface{} = nil
	if in.PlannedQty > 0 && in.ActualQty > 0 {
		yieldVar = in.PlannedQty - in.ActualQty
	}
	actualYieldWeightKg := 0.0
	if massGrams > 0 {
		actualYieldWeightKg = massGrams / 1000
	}
	_ = fromMass

	payload := map[string]interface{}{
		"production_id":            in.ProductionID,
		"productionId":             in.ProductionID,
		"item_id":                  outputItemID,
		"item_name":                in.ItemName,
		"planned_quantity":         in.PlannedQty,
		"actual_quantity_produced": in.ActualQty,
		"actual_yield":             in.ActualQty,
		"actualYield":              in.ActualQty,
		"output_quantity":          usable,
		"usable_quantity":          usable,
		"waste_quantity":           in.WasteQty,
		"waste_reason":             in.WasteReason,
		"batch_number":             in.BatchNumber,
		"expiry_date":              in.ExpiryDate,
		"unit_cost":                unitCostVal,
		"total_cost":               toStockValue,
		"production_cost":          productionCost,
		"total_input_cost":         totalInputCost,
		"good_quantity":            goodQty,
		"total_made":               totalMade,
		"waste_value":              wasteValue,
		"unaccounted_quantity":     unaccounted,
		"cost_reconciliation_ok":   reconOK,
		"cost_source":              "actual_inputs",
		"shop_id":                  in.ShopID,
		"inputs":                   inputMaps,
		"outputs":                  outputMaps,
		"stock_deductions":         deductions,
		"multi_output":             len(in.Outputs) > 1,
		"yield_percentage":         yieldPct,
		"expected_yield":           in.PlannedQty,
		"actual_yield_weight_kg":   actualYieldWeightKg,
		"yield_variance_weight_kg": yieldVar,
	}
	// Merge any extra client payload
	if in.Raw != nil {
		if extra, ok := in.Raw["payload"].(map[string]interface{}); ok {
			for k, v := range extra {
				payload[k] = v
			}
		}
	}

	var batchPtr *string
	if in.BatchNumber != "" {
		batchPtr = &in.BatchNumber
	}
	var expiryPtr *time.Time
	if in.ExpiryDate != "" {
		if t, err := time.Parse("2006-01-02", in.ExpiryDate); err == nil {
			expiryPtr = &t
		}
	}

	// Ensure FG Item rows exist (Event.item_id FK + projection)
	for _, o := range in.Outputs {
		if o.ItemID != "" {
			_ = s.ensureItem(ctx, o.ItemID, o.ItemName, o.Unit)
		}
	}
	if outputItemID != "" {
		_ = s.ensureItem(ctx, outputItemID, in.ItemName, "pcs")
	}

	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType:      eventstore.EventProductionFinished,
		ActorUserID:    actorPtr,
		IdempotencyKey: idem,
		ItemID:         itemPtr,
		BatchNumber:    batchPtr,
		ExpiryDate:     expiryPtr,
		Quantity:       &qty,
		UnitCost:       unitCostPtr,
		TotalCost:      totalCostPtr,
		Payload:        payload,
	})
	if err != nil {
		return nil, err
	}

	// One production waste event per batch (stable idempotency — Michael F2)
	if wasteQty > 0 && outputItemID != "" {
		wIdem := "waste-production-" + outputItemID + "-" + in.ProductionID
		if in.BatchNumber != "" {
			wIdem = "waste-production-" + outputItemID + "-" + in.BatchNumber
		}
		wQty := wasteQty
		wItem := outputItemID
		wUC := money.FromFloatUnitCost(unitCostVal)
		wTot := money.FromFloat(wasteValue)
		_, _ = s.eventStore.Append(ctx, eventstore.AppendInput{
			EventType:      eventstore.EventWasteRecorded,
			ActorUserID:    actorPtr,
			IdempotencyKey: wIdem,
			ItemID:         &wItem,
			Quantity:       &wQty,
			UnitCost:       &wUC,
			TotalCost:      &wTot,
			WasteReason:    strPtr(in.WasteReason),
			Payload: map[string]interface{}{
				"production_id":     in.ProductionID,
				"productionId":      in.ProductionID,
				"item_id":           outputItemID,
				"item_name":         in.ItemName,
				"quantity":          wasteQty,
				"quantity_wasted":   wasteQty,
				"unit_cost":         unitCostVal,
				"total_waste_value": wasteValue,
				"total_cost":        wasteValue,
				"reason":            in.WasteReason,
				"waste_reason":      in.WasteReason,
				"batch_number":      in.BatchNumber,
				"module_source":     "production",
				"shop_id":           in.ShopID,
			},
		})
	}

	msg := "Production finished successfully"
	if unaccounted > 0 {
		msg = "Production finished; units unaccounted (planned > good + waste)"
	}
	return map[string]interface{}{
		"success":               true,
		"eventId":               event.ID,
		"productionId":          in.ProductionID,
		"production_id":         in.ProductionID,
		"productionCost":        productionCost,
		"production_cost":       productionCost,
		"finishedUnitCost":      unitCostVal,
		"unit_cost":             unitCostVal,
		"goodQuantity":          goodQty,
		"wasteQuantity":         wasteQty,
		"totalMade":             totalMade,
		"unaccountedQuantity":   unaccounted,
		"costReconciliationOk":  reconOK,
		"message":               msg,
	}, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *Service) History(ctx context.Context, shopID string, limit int) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, event_type, item_id, quantity, COALESCE(payload::text,'{}'), created_at
		FROM "Event"
		WHERE shop_id = $1
		  AND event_type IN ('production_started','production_finished')
		ORDER BY created_at DESC
		LIMIT 500`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		id, et     string
		itemID     *string
		qty        *float64
		payload    []byte
		created    time.Time
	}
	var raw []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.et, &r.itemID, &r.qty, &r.payload, &r.created); err != nil {
			return nil, err
		}
		raw = append(raw, r)
	}

	type run struct {
		ProductionID, RecipeID, RecipeName, ItemID, BatchNumber, Status, StartedAt, FinishedAt string
		BatchSize, ActualYield float64
	}
	byID := map[string]*run{}
	order := []string{}
	for _, r := range raw {
		var p map[string]interface{}
		_ = json.Unmarshal(r.payload, &p)
		if p == nil {
			p = map[string]interface{}{}
		}
		pid, _ := p["production_id"].(string)
		if pid == "" {
			pid, _ = p["productionId"].(string)
		}
		if pid == "" {
			pid = r.id
		}
		runRec, ok := byID[pid]
		if !ok {
			runRec = &run{ProductionID: pid, Status: "started"}
			byID[pid] = runRec
			order = append(order, pid)
		}
		if name, ok := p["recipe_name"].(string); ok && name != "" {
			runRec.RecipeName = name
		}
		if rid, ok := p["recipe_id"].(string); ok && rid != "" {
			runRec.RecipeID = rid
		}
		if r.itemID != nil {
			runRec.ItemID = *r.itemID
		}
		if oid, ok := p["output_item_id"].(string); ok && oid != "" {
			runRec.ItemID = oid
		}
		if oid, ok := p["item_id"].(string); ok && oid != "" {
			runRec.ItemID = oid
		}
		if bs, ok := p["batch_size"].(float64); ok {
			runRec.BatchSize = bs
		}
		if r.et == "production_finished" {
			runRec.Status = "finished"
			runRec.FinishedAt = r.created.UTC().Format(time.RFC3339)
			if r.qty != nil {
				runRec.ActualYield = *r.qty
			}
			if oq, ok := p["output_quantity"].(float64); ok {
				runRec.ActualYield = oq
			}
			if oq, ok := p["actual_quantity_produced"].(float64); ok {
				runRec.ActualYield = oq
			}
		}
		if r.et == "production_started" {
			runRec.StartedAt = r.created.UTC().Format(time.RFC3339)
			if st, ok := p["started_at"].(string); ok {
				runRec.StartedAt = st
			}
		}
		if bn, ok := p["batch_number"].(string); ok {
			runRec.BatchNumber = bn
		}
	}

	items := make([]map[string]interface{}, 0, len(order))
	for _, pid := range order {
		if len(items) >= limit {
			break
		}
		r := byID[pid]
		var finished interface{}
		if r.FinishedAt != "" {
			finished = r.FinishedAt
		}
		items = append(items, map[string]interface{}{
			"productionId": r.ProductionID,
			"recipeId":     r.RecipeID,
			"recipeName":   r.RecipeName,
			"itemId":       r.ItemID,
			"batchSize":    r.BatchSize,
			"actualYield":  r.ActualYield,
			"batchNumber":  r.BatchNumber,
			"status":       r.Status,
			"startedAt":    r.StartedAt,
			"finishedAt":   finished,
		})
	}
	return map[string]interface{}{"items": items, "total": len(items), "shopId": shopID}, nil
}


// resolveLines mirrors Nest production.service resolveLines.
// When client omits inputs, scale RecipeIngredient by actual/standard_yield.
// When client omits outputs, fill from RecipeOutput or primary item.
func (s *Service) resolveLines(ctx context.Context, in FinishInput) FinishInput {
	clientInputs := len(in.Inputs) > 0
	clientOutputs := len(in.Outputs) > 0
	if clientInputs && clientOutputs {
		return in
	}

	recipeID := in.RecipeID
	if recipeID == "" && in.Raw != nil {
		if v, ok := in.Raw["recipe_id"].(string); ok {
			recipeID = v
		} else if v, ok := in.Raw["recipeId"].(string); ok {
			recipeID = v
		}
	}
	// Resolve start context from production_started event
	if recipeID == "" || in.PlannedQty <= 0 || in.ItemID == "" {
		shopID := in.ShopID
		if shopID == "" {
			shopID = "1"
		}
		var payload []byte
		var itemID *string
		_ = s.pool.QueryRow(ctx, `
			SELECT payload, item_id FROM "Event"
			WHERE shop_id = $1 AND event_type = 'production_started'
			  AND (payload->>'production_id' = $2 OR payload->>'productionId' = $2)
			ORDER BY created_at DESC LIMIT 1`, shopID, in.ProductionID).Scan(&payload, &itemID)
		if len(payload) > 0 {
			var p map[string]interface{}
			_ = json.Unmarshal(payload, &p)
			if recipeID == "" {
				if v, ok := p["recipe_id"].(string); ok {
					recipeID = v
				} else if v, ok := p["recipeId"].(string); ok {
					recipeID = v
				} else if v, ok := p["item_id"].(string); ok {
					recipeID = v
				}
			}
			if in.PlannedQty <= 0 {
				if v, ok := p["planned_quantity"].(float64); ok {
					in.PlannedQty = v
				} else if v, ok := p["batch_size"].(float64); ok {
					in.PlannedQty = v
				}
			}
			if in.ItemID == "" && itemID != nil {
				in.ItemID = *itemID
			}
			if in.ItemName == "" {
				if v, ok := p["item_name"].(string); ok {
					in.ItemName = v
				} else if v, ok := p["recipe_name"].(string); ok {
					in.ItemName = v
				}
			}
		}
	}
	in.RecipeID = recipeID

	primaryActual := in.ActualQty
	if primaryActual <= 0 {
		primaryActual = in.PlannedQty
	}

	if recipeID == "" {
		if !clientOutputs && in.ItemID != "" && primaryActual > 0 {
			in.Outputs = []Line{{
				ItemID: in.ItemID, ItemName: in.ItemName,
				ActualQuantity: primaryActual, StandardQuantity: in.PlannedQty,
				Unit: "pcs",
			}}
			if in.ActualQty <= 0 {
				in.ActualQty = primaryActual
			}
		}
		return in
	}

	// Load recipe by id, recipe_code, or item_id
	var recID, recItemID, recName, recUnit string
	var standardYield float64
	err := s.pool.QueryRow(ctx, `
		SELECT id, item_id, item_name, COALESCE(unit,'pcs'), standard_yield
		FROM "Recipe"
		WHERE id = $1 OR recipe_code = $1 OR item_id = $1
		LIMIT 1`, recipeID).Scan(&recID, &recItemID, &recName, &recUnit, &standardYield)
	if err != nil {
		if !clientOutputs && in.ItemID != "" && primaryActual > 0 {
			in.Outputs = []Line{{
				ItemID: in.ItemID, ItemName: in.ItemName,
				ActualQuantity: primaryActual, Unit: "pcs",
			}}
		}
		return in
	}
	if standardYield <= 0 {
		standardYield = 1
	}
	if primaryActual <= 0 {
		primaryActual = standardYield
	}
	if in.ActualQty <= 0 {
		in.ActualQty = primaryActual
	}
	scale := primaryActual / standardYield

	if !clientInputs {
		rows, err := s.pool.Query(ctx, `
			SELECT raw_item_id, COALESCE(raw_item_name,''), quantity_per_unit, COALESCE(unit,'pcs'), COALESCE(unit_cost,0)
			FROM "RecipeIngredient" WHERE recipe_id = $1`, recID)
		if err == nil {
			defer rows.Close()
			var inputs []Line
			for rows.Next() {
				var rid, rname, unit string
				var per, cost float64
				if rows.Scan(&rid, &rname, &per, &unit, &cost) != nil {
					continue
				}
				std := per * scale
				if rid == "" || std <= 0 {
					continue
				}
				inputs = append(inputs, Line{
					ItemID: rid, ItemName: rname, Unit: unit,
					StandardQuantity: std, ActualQuantity: std, UnitCost: cost,
				})
			}
			in.Inputs = inputs
		}
	}

	if !clientOutputs {
		// Try RecipeOutput table
		rows, err := s.pool.Query(ctx, `
			SELECT item_id, COALESCE(item_name,''), COALESCE(standard_quantity,0), COALESCE(unit,'pcs'),
			       COALESCE(unit_weight,0), COALESCE(weight_unit,'g'), COALESCE(is_active,true)
			FROM "RecipeOutput" WHERE recipe_id = $1`, recID)
		var outs []Line
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var oid, oname, unit, wu string
				var std, uw float64
				var active bool
				if rows.Scan(&oid, &oname, &std, &unit, &uw, &wu, &active) != nil {
					continue
				}
				if !active || oid == "" {
					continue
				}
				outs = append(outs, Line{
					ItemID: oid, ItemName: oname, Unit: unit,
					StandardQuantity: std * scale, ActualQuantity: 0, // multi-output waits for counts
					UnitWeight: uw, WeightUnit: wu,
				})
			}
		}
		if len(outs) > 1 {
			// Multi-output: leave actual_quantity 0 unless primaryActual maps to single
			in.Outputs = outs
		} else if len(outs) == 1 {
			o := outs[0]
			if o.StandardQuantity <= 0 {
				o.StandardQuantity = primaryActual
			}
			o.ActualQuantity = o.StandardQuantity
			if o.ActualQuantity <= 0 {
				o.ActualQuantity = primaryActual
			}
			in.Outputs = []Line{o}
		} else {
			// Single-output recipe: primary FG
			fg := in.ItemID
			if fg == "" {
				fg = recItemID
			}
			name := in.ItemName
			if name == "" {
				name = recName
			}
			in.Outputs = []Line{{
				ItemID: fg, ItemName: name, Unit: recUnit,
				StandardQuantity: primaryActual, ActualQuantity: primaryActual,
			}}
			if in.ItemID == "" {
				in.ItemID = fg
			}
		}
	}

	return in
}

func (s *Service) ensureItem(ctx context.Context, itemID, name, unit string) error {
	if itemID == "" {
		return nil
	}
	var exists string
	err := s.pool.QueryRow(ctx, `SELECT item_id FROM "Item" WHERE item_id = $1`, itemID).Scan(&exists)
	if err == nil {
		return nil
	}
	if name == "" {
		name = itemID
	}
	if unit == "" {
		unit = "pcs"
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO "Item" (id, item_id, name, unit, category, min_stock, reorder_point, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'Finished Goods', 0, 0, $5, $5)
		ON CONFLICT (item_id) DO NOTHING`,
		uuid.New().String(), itemID, name, unit, time.Now().UTC())
	return err
}
