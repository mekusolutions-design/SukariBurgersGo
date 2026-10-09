# TRACE_AUDIT.md

## Status
**FIXED in this deliverable (Go)** — Nest-compatible search contract implemented.
**Live DB verification** — not run in this environment (no PostgreSQL).

## Source
Authoritative: `"Event"` table. No fabricated history from projections alone.
`InventoryProjection` / `Item` used only for names, units, current stock on item timeline.

## Endpoints
| Method | Path | Behavior |
|--------|------|----------|
| GET | `/trace` | Search: `q`, `itemId`, `batchNumber`, `eventType`, `limit`; shop from JWT |
| GET | `/trace/item/{itemId}` | Full timeline + stock snapshot |

## Filters
- Shop: **authenticated shop only** (client `shopId` ignored for isolation)
- Item: catalog name/SKU resolve + `item_id ILIKE` + payload `item_name` / `itemId`
- Batch: `batch_number` + payload batch fields
- Event type: Nest-compatible aliases (received, waste, pos_sale, …)
- Ordering: `created_at DESC, id DESC`
- Pagination: `limit` (1–200); `nextCursor` `{createdAt,id}` when more matches in window

## Response (search) — web contract
```json
{
  "query": "",
  "shopId": "1",
  "eventType": null,
  "items": [{ "itemId", "itemName", "eventCount", "lastEventAt", "batches" }],
  "events": [{ "id", "eventType", "itemId", "itemName", "actor", "summary", "createdAt", "payload", ... }],
  "totalEvents": 0,
  "limit": 50,
  "nextCursor": null
}
```
Web `trace/api.ts` reads `root.events` and `root.items` / `totalEvents`.

## Historical coverage
Does **not** hard-cap primary strategy at “last 500 only” as the sole path.
Uses DB filters + ordered window up to 2000 candidates per request; cursor for deeper pages.
Still not a full table sequential scan of all history in one response (by design).

## Tests
- Unit: `eventTypeAliases`, `eventTypeMatches`, `buildSummary` (`internal/trace/service_test.go`)
- Integration with seeded events: **blocked** (no DATABASE_URL in build env)

## Acceptance scenario (must be run on staging)
Seed events for TOMATO-001 across months → `GET /trace?q=TOMATO-001` returns all in page → `GET /trace/item/TOMATO-001` timeline → other shop excluded.
