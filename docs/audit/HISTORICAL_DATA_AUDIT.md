# HISTORICAL_DATA_AUDIT.md

## Principle

Nest-written rows in shared PostgreSQL must remain readable by Go without migration of event meaning.

## Evidence from code

- Go reads Prisma-shaped tables: `"Event"`, `"InventoryProjection"`, `"Item"`, `"VarianceBatch"`, `"users"` (not `"User"`)
- Trace bug class: wrong relation name → fixed to `users`
- Menu data often lives only as `menu_created` events → Go must fold events (implemented)
- Variance UI expects event-fold batches → implemented

## Required live proof (operator)

On shared Supabase:

```sql
SELECT event_type, COUNT(*) FROM "Event" GROUP BY 1 ORDER BY 2 DESC;
SELECT COUNT(*) FROM "InventoryProjection";
SELECT COUNT(*) FROM "VarianceBatch";
SELECT COUNT(*) FROM users;
```

Then:

```
GET /trace?shopId=1
GET /menu?shopId=1
GET /variance/batches?shopId=1
GET /pos/menu-availability?shopId=1
```

Compare to Nest responses on same DB.

## Status

**Historical compatibility: PARTIAL** — schema awareness improved; full replay golden tests not executed in this environment (no live DB).
