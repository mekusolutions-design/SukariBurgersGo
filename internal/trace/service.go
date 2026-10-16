package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// SearchOpts matches Nest GET /trace query parameters.
type SearchOpts struct {
	Q           string
	ItemID      string
	BatchNumber string
	EventType   string
	Limit       int
	// Cursor: return events strictly older than this (created_at, id)
	CursorCreatedAt *time.Time
	CursorID        string
}

// Search returns the web-compatible Nest-shaped payload:
//
//	{ query, shopId, eventType, items[], events[], totalEvents }
func (s *Service) Search(ctx context.Context, shopID string, opts SearchOpts) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := strings.TrimSpace(opts.Q)
	itemID := strings.TrimSpace(opts.ItemID)
	batch := strings.TrimPrefix(strings.TrimSpace(opts.BatchNumber), "#")
	eventType := strings.TrimSpace(opts.EventType)
	if strings.EqualFold(eventType, "any") {
		eventType = ""
	}
	searchTerm := q
	if searchTerm == "" {
		searchTerm = itemID
	}

	empty := map[string]interface{}{
		"query":       searchTerm,
		"shopId":      shopID,
		"eventType":   nilString(eventType),
		"items":       []interface{}{},
		"events":      []interface{}{},
		"totalEvents": 0,
	}
	if searchTerm == "" && batch == "" && eventType == "" {
		return empty, nil
	}

	resolvedIDs, _ := s.resolveItemIDs(ctx, shopID, searchTerm)

	// Fetch candidate window using indexes (shop_id, item_id / event_type / created_at).
	// We pull a larger page from DB then rank/filter; cursor allows paging deeper than one window.
	fetchN := limit * 10
	if fetchN < 200 {
		fetchN = 200
	}
	if fetchN > 2000 {
		fetchN = 2000
	}

	rows, err := s.queryCandidates(ctx, shopID, searchTerm, batch, eventType, resolvedIDs, fetchN, opts.CursorCreatedAt, opts.CursorID)
	if err != nil {
		return nil, err
	}

	termLower := strings.ToLower(searchTerm)
	batchLower := strings.ToLower(batch)
	resolvedSet := map[string]struct{}{}
	for _, id := range resolvedIDs {
		resolvedSet[strings.ToLower(id)] = struct{}{}
	}

	var events []map[string]interface{}
	byItem := map[string]*itemAgg{}

	for _, e := range rows {
		if eventType != "" && !eventTypeMatches(e.EventType, eventType) {
			continue
		}
		hay := buildHaystack(e)
		if batchLower != "" {
			if !batchMatch(e, batchLower) {
				continue
			}
		}
		if termLower != "" {
			idLower := strings.ToLower(strPtr(e.ItemID))
			_, inResolved := resolvedSet[idLower]
			if !inResolved && !strings.Contains(hay, termLower) && idLower != termLower {
				continue
			}
		}
		node := toNode(e)
		events = append(events, node)

		iid := strVal(node["itemId"])
		if iid == "" {
			iid = "_unknown"
		}
		agg, ok := byItem[iid]
		if !ok {
			agg = &itemAgg{ItemID: iid, ItemName: asStrPtr(node["itemName"]), Batches: map[string]struct{}{}}
			byItem[iid] = agg
		}
		agg.EventCount++
		if ca, ok := node["createdAt"].(string); ok {
			if agg.LastEventAt == "" || ca > agg.LastEventAt {
				agg.LastEventAt = ca
			}
		}
		if bn := strVal(node["batchNumber"]); bn != "" {
			agg.Batches[bn] = struct{}{}
		}
	}

	// Stable order already DESC from SQL; cap to limit
	total := len(events)
	if len(events) > limit {
		events = events[:limit]
	}
	if events == nil {
		events = []map[string]interface{}{}
	}

	items := make([]map[string]interface{}, 0, len(byItem))
	for _, a := range byItem {
		batches := make([]string, 0, len(a.Batches))
		for b := range a.Batches {
			batches = append(batches, b)
		}
		items = append(items, map[string]interface{}{
			"itemId":      a.ItemID,
			"itemName":    a.ItemName,
			"eventCount":  a.EventCount,
			"lastEventAt": nullIfEmpty(a.LastEventAt),
			"batches":     batches,
		})
	}

	var nextCursor interface{}
	if total > limit && len(events) > 0 {
		last := events[len(events)-1]
		nextCursor = map[string]interface{}{
			"createdAt": last["createdAt"],
			"id":        last["id"],
		}
	}

	return map[string]interface{}{
		"query":       searchTerm,
		"shopId":      shopID,
		"eventType":   nilString(eventType),
		"items":       items,
		"events":      events,
		"totalEvents": total,
		"limit":       limit,
		"nextCursor":  nextCursor,
	}, nil
}

type itemAgg struct {
	ItemID      string
	ItemName    *string
	EventCount  int
	LastEventAt string
	Batches     map[string]struct{}
}

// ForItem returns full timeline for one SKU / item_id (web TraceItemResult).
func (s *Service) ForItem(ctx context.Context, shopID, itemID string, limit int) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, apperrors.BadRequest("itemId required")
	}
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}

	rows, err := s.queryByItem(ctx, shopID, itemID, limit)
	if err != nil {
		return nil, err
	}

	// Name / haystack fallback if exact item_id sparse
	if len(rows) == 0 {
		term := strings.ToLower(itemID)
		scan, err := s.queryCandidates(ctx, shopID, itemID, "", "", nil, limit, nil, "")
		if err != nil {
			return nil, err
		}
		for _, e := range scan {
			if strings.Contains(buildHaystack(e), term) || strings.EqualFold(strPtr(e.ItemID), itemID) {
				rows = append(rows, e)
			}
		}
	}

	var name *string
	var unit *string
	var stock *float64
	_ = s.pool.QueryRow(ctx, `
		SELECT i.name, i.unit, p.available_stock
		FROM "Item" i
		LEFT JOIN "InventoryProjection" p ON p.item_id = i.item_id AND p.shop_id = $1
		WHERE i.item_id = $2
		LIMIT 1`, shopID, itemID).Scan(&name, &unit, &stock)
	if name == nil {
		_ = s.pool.QueryRow(ctx, `
			SELECT available_stock FROM "InventoryProjection"
			WHERE shop_id = $1 AND item_id = $2`, shopID, itemID).Scan(&stock)
	}

	if len(rows) == 0 && stock == nil {
		return nil, apperrors.NotFound(fmt.Sprintf("No trace history for item %s", itemID))
	}

	events := make([]map[string]interface{}, 0, len(rows))
	batchSet := map[string]struct{}{}
	related := map[string]struct{}{}
	for _, e := range rows {
		n := toNode(e)
		events = append(events, n)
		if bn := strVal(n["batchNumber"]); bn != "" {
			batchSet[bn] = struct{}{}
		}
		if iid := strVal(n["itemId"]); iid != "" && !strings.EqualFold(iid, itemID) {
			related[iid] = struct{}{}
		}
		if name == nil {
			if in, ok := n["itemName"].(string); ok && in != "" {
				name = &in
			}
		}
	}
	batches := make([]string, 0, len(batchSet))
	for b := range batchSet {
		batches = append(batches, b)
	}
	rel := make([]string, 0, len(related))
	for r := range related {
		rel = append(rel, r)
	}
	var currentStock interface{}
	if stock != nil {
		currentStock = *stock
	} else {
		currentStock = nil
	}

	return map[string]interface{}{
		"itemId":         itemID,
		"itemName":       name,
		"unit":           unit,
		"currentStock":   currentStock,
		"events":         events,
		"batches":        batches,
		"relatedItemIds": rel,
	}, nil
}

// List is retained for simple diagnostics (newest N events for shop).
func (s *Service) List(ctx context.Context, shopID string, limit int) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.queryCandidates(ctx, shopID, "", "", "", nil, limit, nil, "")
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, e := range rows {
		out = append(out, toNode(e))
	}
	return out, nil
}

// --- internal row + SQL ---

type eventRow struct {
	ID          string
	EventType   string
	ShopID      string
	ItemID      *string
	BatchNumber *string
	Quantity    *float64
	UnitCost    *float64
	TotalCost   *float64
	ActorName   *string
	ItemName    *string
	ItemUnit    *string
	Payload     map[string]interface{}
	CreatedAt   time.Time
}

func (s *Service) resolveItemIDs(ctx context.Context, shopID, token string) ([]string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}
	like := "%" + token + "%"
	rows, err := s.pool.Query(ctx, `
		SELECT item_id FROM "Item"
		WHERE item_id ILIKE $1 OR name ILIKE $1
		LIMIT 50`, like)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	var ids []string
	seen := map[string]struct{}{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	// Also stock projection item_ids for this shop
	rows2, err := s.pool.Query(ctx, `
		SELECT item_id FROM "InventoryProjection"
		WHERE shop_id = $1 AND item_id ILIKE $2
		LIMIT 50`, shopID, like)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var id string
			if rows2.Scan(&id) == nil && id != "" {
				if _, ok := seen[id]; !ok {
					seen[id] = struct{}{}
					ids = append(ids, id)
				}
			}
		}
	}
	return ids, nil
}

func (s *Service) queryCandidates(
	ctx context.Context,
	shopID, searchTerm, batch, eventType string,
	resolvedIDs []string,
	limit int,
	cursorAt *time.Time,
	cursorID string,
) ([]eventRow, error) {
	aliases := eventTypeAliases(eventType)

	// Build dynamic OR for item / batch narrowing when possible.
	args := []interface{}{shopID}
	argN := 2
	var filters []string
	filters = append(filters, `e.shop_id = $1`)

	if len(aliases) > 0 {
		filters = append(filters, fmt.Sprintf(`e.event_type = ANY($%d)`, argN))
		args = append(args, aliases)
		argN++
	}

	var orParts []string
	if len(resolvedIDs) > 0 {
		orParts = append(orParts, fmt.Sprintf(`e.item_id = ANY($%d)`, argN))
		args = append(args, resolvedIDs)
		argN++
	}
	if searchTerm != "" {
		orParts = append(orParts, fmt.Sprintf(`e.item_id ILIKE $%d`, argN))
		args = append(args, "%"+searchTerm+"%")
		argN++
		orParts = append(orParts, fmt.Sprintf(`e.payload->>'item_name' ILIKE $%d`, argN))
		args = append(args, "%"+searchTerm+"%")
		argN++
		orParts = append(orParts, fmt.Sprintf(`e.payload->>'itemName' ILIKE $%d`, argN))
		args = append(args, "%"+searchTerm+"%")
		argN++
		orParts = append(orParts, fmt.Sprintf(`e.payload->>'item_id' ILIKE $%d`, argN))
		args = append(args, "%"+searchTerm+"%")
		argN++
	}
	if batch != "" {
		orParts = append(orParts, fmt.Sprintf(`e.batch_number ILIKE $%d`, argN))
		args = append(args, "%"+batch+"%")
		argN++
		orParts = append(orParts, fmt.Sprintf(`e.payload->>'batch_number' ILIKE $%d`, argN))
		args = append(args, "%"+batch+"%")
		argN++
		orParts = append(orParts, fmt.Sprintf(`e.payload->>'batch_id' ILIKE $%d`, argN))
		args = append(args, "%"+batch+"%")
		argN++
		orParts = append(orParts, fmt.Sprintf(`e.payload->>'display_batch_code' ILIKE $%d`, argN))
		args = append(args, "%"+batch+"%")
		argN++
	}
	if len(orParts) > 0 {
		filters = append(filters, "("+strings.Join(orParts, " OR ")+")")
	}

	if cursorAt != nil && cursorID != "" {
		filters = append(filters, fmt.Sprintf(`(e.created_at, e.id) < ($%d::timestamptz, $%d)`, argN, argN+1))
		args = append(args, *cursorAt, cursorID)
		argN += 2
	}

	args = append(args, limit)
	limitArg := argN

	q := fmt.Sprintf(`
		SELECT e.id, e.event_type, e.shop_id, e.item_id, e.batch_number, e.quantity,
		       e.unit_cost, e.total_cost, e.payload, e.created_at,
		       u.name, i.name, i.unit
		FROM "Event" e
		LEFT JOIN "users" u ON u.id = e.actor_user_id
		LEFT JOIN "Item" i ON i.item_id = e.item_id
		WHERE %s
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $%d`, strings.Join(filters, " AND "), limitArg)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		// Fallback: shop window only (still ordered / limited — not unbounded)
		return s.scanShopWindow(ctx, shopID, aliases, limit, cursorAt, cursorID)
	}
	defer rows.Close()
	return scanEventRows(rows)
}

func (s *Service) queryByItem(ctx context.Context, shopID, itemID string, limit int) ([]eventRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.event_type, e.shop_id, e.item_id, e.batch_number, e.quantity,
		       e.unit_cost, e.total_cost, e.payload, e.created_at,
		       u.name, i.name, i.unit
		FROM "Event" e
		LEFT JOIN "users" u ON u.id = e.actor_user_id
		LEFT JOIN "Item" i ON i.item_id = e.item_id
		WHERE e.shop_id = $1
		  AND (
		    e.item_id = $2
		    OR e.payload->>'item_id' = $2
		    OR e.payload->>'itemId' = $2
		    OR e.payload->>'raw_item_id' = $2
		  )
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $3`, shopID, itemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

func (s *Service) scanShopWindow(ctx context.Context, shopID string, aliases []string, limit int, cursorAt *time.Time, cursorID string) ([]eventRow, error) {
	args := []interface{}{shopID}
	filter := `e.shop_id = $1`
	n := 2
	if len(aliases) > 0 {
		filter += fmt.Sprintf(` AND e.event_type = ANY($%d)`, n)
		args = append(args, aliases)
		n++
	}
	if cursorAt != nil && cursorID != "" {
		filter += fmt.Sprintf(` AND (e.created_at, e.id) < ($%d::timestamptz, $%d)`, n, n+1)
		args = append(args, *cursorAt, cursorID)
		n += 2
	}
	args = append(args, limit)
	q := fmt.Sprintf(`
		SELECT e.id, e.event_type, e.shop_id, e.item_id, e.batch_number, e.quantity,
		       e.unit_cost, e.total_cost, e.payload, e.created_at,
		       u.name, i.name, i.unit
		FROM "Event" e
		LEFT JOIN "users" u ON u.id = e.actor_user_id
		LEFT JOIN "Item" i ON i.item_id = e.item_id
		WHERE %s
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $%d`, filter, n)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

type scannable interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanEventRows(rows scannable) ([]eventRow, error) {
	var out []eventRow
	for rows.Next() {
		var e eventRow
		var payload []byte
		var unitCost, totalCost *string
		if err := rows.Scan(
			&e.ID, &e.EventType, &e.ShopID, &e.ItemID, &e.BatchNumber, &e.Quantity,
			&unitCost, &totalCost, &payload, &e.CreatedAt,
			&e.ActorName, &e.ItemName, &e.ItemUnit,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payload, &e.Payload)
		if e.Payload == nil {
			e.Payload = map[string]interface{}{}
		}
		if unitCost != nil {
			var f float64
			fmt.Sscanf(*unitCost, "%f", &f)
			e.UnitCost = &f
		}
		if totalCost != nil {
			var f float64
			fmt.Sscanf(*totalCost, "%f", &f)
			e.TotalCost = &f
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func toNode(e eventRow) map[string]interface{} {
	p := e.Payload
	itemID := strPtr(e.ItemID)
	if itemID == "" {
		itemID = asString(p["item_id"], asString(p["itemId"], ""))
	}
	itemName := strPtr(e.ItemName)
	if itemName == "" {
		itemName = asString(p["item_name"], asString(p["itemName"], asString(p["name"], "")))
	}
	batch := strPtr(e.BatchNumber)
	if batch == "" {
		batch = asString(p["batch_number"], asString(p["display_batch_code"], asString(p["batch_id"], "")))
	}
	var qty interface{}
	if e.Quantity != nil {
		qty = *e.Quantity
	} else if v, ok := p["quantity"]; ok {
		qty = v
	}
	var totalCost interface{}
	if e.TotalCost != nil {
		totalCost = *e.TotalCost
	}
	actor := strPtr(e.ActorName)
	summary := buildSummary(e.EventType, firstNonEmpty(itemName, itemID), e.Quantity, p)
	return map[string]interface{}{
		"id":          e.ID,
		"eventType":   e.EventType,
		"shopId":      e.ShopID,
		"itemId":      nullIfEmpty(itemID),
		"itemName":    nullIfEmpty(itemName),
		"batchNumber": nullIfEmpty(batch),
		"quantity":    qty,
		"totalCost":   totalCost,
		"actorName":   nullIfEmpty(actor),
		"actor":       nullIfEmpty(actor),
		"createdAt":   e.CreatedAt.UTC().Format(time.RFC3339),
		"summary":     summary,
		"payload":     p,
	}
}

func buildHaystack(e eventRow) string {
	parts := []string{
		strPtr(e.ItemID), strPtr(e.ItemName), strPtr(e.BatchNumber), e.EventType,
		asString(e.Payload["item_id"], ""), asString(e.Payload["item_name"], ""),
		asString(e.Payload["itemName"], ""), asString(e.Payload["raw_item_id"], ""),
		asString(e.Payload["batch_number"], ""), asString(e.Payload["batch_id"], ""),
		asString(e.Payload["display_batch_code"], ""), asString(e.Payload["name"], ""),
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func batchMatch(e eventRow, batchLower string) bool {
	cands := []string{
		strPtr(e.BatchNumber),
		asString(e.Payload["batch_number"], ""),
		asString(e.Payload["batch_id"], ""),
		asString(e.Payload["display_batch_code"], ""),
		asString(e.Payload["production_id"], ""),
	}
	for _, c := range cands {
		c = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(c), "#"))
		if c == "" {
			continue
		}
		if c == batchLower || strings.Contains(c, batchLower) || strings.Contains(batchLower, c) {
			return true
		}
	}
	return false
}

func eventTypeAliases(raw string) []string {
	t := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(raw, " ", "_")))
	if t == "" {
		return nil
	}
	m := map[string][]string{
		"received":              {"received", "RECEIVING", "goods_received"},
		"receiving":             {"received", "RECEIVING", "goods_received"},
		"waste":                 {"waste_recorded", "WASTE_RECORDED", "waste"},
		"waste_recorded":        {"waste_recorded", "WASTE_RECORDED", "waste"},
		"production_finished":   {"production_finished"},
		"production_started":    {"production_started"},
		"pos_sale":              {"pos_sale", "pos_order_created", "ORDER_PAID", "sale_completed"},
		"sale":                  {"pos_sale", "pos_order_created", "ORDER_PAID", "sale_completed"},
		"refill_issued":         {"refill_issued"},
		"refill_requested":      {"refill_requested"},
		"finished_good_adjusted": {"finished_good_adjusted", "STOCK_ADJUSTMENT"},
		"closing_stock_counted": {"closing_stock_counted", "stock_count_completed"},
		"stock_count":           {"closing_stock_counted", "stock_count_completed"},
		"variance":              {"variance_batch", "variance_reason_submitted", "variance_flagged", "VARIANCE_WRITEOFF"},
	}
	if a, ok := m[t]; ok {
		return a
	}
	return []string{raw, t}
}

func eventTypeMatches(actual, filter string) bool {
	aliases := eventTypeAliases(filter)
	al := strings.ToLower(actual)
	for _, a := range aliases {
		if strings.EqualFold(a, actual) || strings.ToLower(a) == al {
			return true
		}
	}
	return strings.Contains(al, strings.ToLower(filter))
}

func buildSummary(eventType, nameOrID string, quantity *float64, payload map[string]interface{}) string {
	name := nameOrID
	if name == "" {
		name = "item"
	}
	qty := ""
	if quantity != nil {
		qty = fmt.Sprintf(" × %v", *quantity)
	}
	t := strings.ToLower(eventType)
	switch {
	case strings.Contains(t, "receiv"):
		return "Received " + name + qty
	case strings.Contains(t, "production_started"):
		return "Production started: " + name + qty
	case strings.Contains(t, "production_finished"):
		return "Production finished: " + name + qty
	case strings.Contains(t, "waste"):
		return "Waste: " + name + qty
	case strings.Contains(t, "pos") || strings.Contains(t, "sale") || strings.Contains(t, "order"):
		return "Sale / order: " + name + qty
	case strings.Contains(t, "refill_issued"):
		return "Refill issued: " + name + qty
	case strings.Contains(t, "refill"):
		return "Refill: " + name + qty
	case strings.Contains(t, "closing") || strings.Contains(t, "stock_count"):
		code := asString(payload["display_batch_code"], asString(payload["batch_id"], ""))
		if code != "" {
			return "Stock count #" + code + ": " + name
		}
		return "Stock count: " + name
	case strings.Contains(t, "variance"):
		return "Variance: " + name + qty
	case strings.Contains(t, "adjust"):
		return "Adjustment: " + name + qty
	default:
		return eventType + ": " + name + qty
	}
}

func strPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func asStrPtr(v interface{}) *string {
	s := strVal(v)
	if s == "" {
		return nil
	}
	return &s
}
func strVal(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
func asString(v interface{}, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
func nilString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// Diagnostics reports event counts for a shop (and optional item) for Trace acceptance tests.
func (s *Service) Diagnostics(ctx context.Context, shopID, itemID string) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	var total int
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM "Event" WHERE shop_id = $1`, shopID).Scan(&total)
	rows, err := s.pool.Query(ctx, `
		SELECT event_type, COUNT(*)::int FROM "Event"
		WHERE shop_id = $1 GROUP BY event_type ORDER BY COUNT(*) DESC LIMIT 50`, shopID)
	byType := []map[string]interface{}{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var et string
			var c int
			_ = rows.Scan(&et, &c)
			byType = append(byType, map[string]interface{}{"eventType": et, "count": c})
		}
	}
	var oldest, newest *time.Time
	_ = s.pool.QueryRow(ctx, `SELECT MIN(created_at), MAX(created_at) FROM "Event" WHERE shop_id = $1`, shopID).Scan(&oldest, &newest)
	out := map[string]interface{}{
		"shopId": shopID, "totalEvents": total, "byEventType": byType,
	}
	if oldest != nil {
		out["oldest"] = oldest.UTC().Format(time.RFC3339)
	}
	if newest != nil {
		out["newest"] = newest.UTC().Format(time.RFC3339)
	}
	if itemID != "" {
		var itemCount int
		_ = s.pool.QueryRow(ctx, `
			SELECT COUNT(*)::int FROM "Event"
			WHERE shop_id = $1 AND (item_id = $2 OR payload->>'item_id' = $2 OR payload->>'itemId' = $2)`,
			shopID, itemID).Scan(&itemCount)
		out["itemId"] = itemID
		out["itemEventCount"] = itemCount
	}
	return out, nil
}
