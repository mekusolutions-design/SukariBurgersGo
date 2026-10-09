# RestFlow Go Migration — Complete Core Surface

**Status:** API + worker build clean; unit/contract tests pass.

## Binaries

```bash
go build -o restflow-api ./cmd/api/
go build -o restflow-worker ./cmd/worker/
go test ./pkg/money/ ./tests/unit/ ./tests/contract/
```

## Endpoint inventory (implemented)

| Method | Path |
|--------|------|
| GET | /health, /health/live, /health/ready |
| POST | /auth/login, /auth/register |
| GET | /users/me |
| GET | /inventory, /inventory/summary, /inventory/{itemId} |
| POST | /received |
| POST | /pos/order |
| GET | /pos/orders, /pos/orders/{orderId} |
| POST | /pos/orders/{orderId}/send-to-kitchen, .../payment |
| GET | /kitchen/active-orders |
| POST | /kitchen/orders/{orderId}/status |
| GET/POST | /recipe, /recipe/{id}, /recipe/ingredient, /recipe/{id}/cost |
| POST/GET | /waste/record, /waste/analytics |
| POST | /production/start, /production/finish |
| POST | /payment/mpesa, /payment/mpesa/callback, /payment/stripe/webhook |
| GET/POST | /variance/batches, .../flag, .../reason |
| GET/POST | /approvals/pending, /history, .../approve, .../reject |
| POST | /sync/bulk |
| GET | /dashboard/summary, /kpis, /alerts |
| POST | /notifications |
| GET/POST | /menu/catalog-items, /menu/combos, /menu/combos/preview |
| GET | /ws (hub info; full WS upgrade pending) |

## Worker

```bash
restflow-worker health
restflow-worker projections-rebuild [shopId]
```

## Tests

- `pkg/money` unit tests
- `tests/unit` aggregate deductions
- `tests/contract` endpoint checklist
- `tests/integration/README.md` — live DB cases documented

## Still for production hardening

1. Full WebSocket protocol (gorilla/websocket) + Redis pub/sub
2. Integration tests against real Postgres
3. Frontend contract parity suite (response shape snapshots)
4. Complete Stripe signature verification
5. Menu event-folded legacy paths (beyond CatalogMenuItem)
6. Rate limiting middleware
7. OpenTelemetry metrics export
