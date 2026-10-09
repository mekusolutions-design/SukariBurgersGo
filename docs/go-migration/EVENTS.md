# Event Compatibility

Event type strings are preserved exactly (see `internal/eventstore/types.go`).

## Append semantics (mirrored from NestJS)

1. Fast-path lookup by `idempotency_key`.
2. If found and payload equal → return existing (idempotent replay).
3. If found and payload differs → 409 Conflict.
4. Otherwise: transaction begins.
5. Re-check idempotency inside tx.
6. Optional ensure Item exists (SKU).
7. Insert Event.
8. Run projection handlers in same tx (when ProjectionRunner is wired).
9. Commit.

## Payload

Historical events may mix snake_case and camelCase. Go decoders must tolerate both. New events write `shop_id` (snake) into the payload for consistency with existing projection handlers.

## Rebuild

CLI support for projection rebuild will be added in the projections phase:

```
restflow worker projections rebuild --shop=1
```
