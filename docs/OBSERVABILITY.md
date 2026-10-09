# Observability (logging, metrics, errors)

## Design principles
1. **Zero-config safe**: no Sentry DSN → Sentry is no-op; metrics on by default, can disable.
2. **Stdout JSON logs** remain the source of truth for Render log streams.
3. **Low cardinality** Prometheus labels (path IDs normalized to `{id}`).
4. **No secrets in Sentry** (Authorization/Cookie stripped in BeforeSend).

## Components

| Piece | Endpoint / env | Purpose |
|--------|----------------|---------|
| Structured logs | stdout | `request` with method, path, status, duration_ms, request_id |
| Health | `GET /health/ready` | Render + uptime checks |
| Metrics | `GET /metrics` | Prometheus scrape |
| Sentry | `SENTRY_DSN` | Errors + panics + 10% traces |

## Environment variables

```bash
# Sentry (optional)
SENTRY_DSN=https://...@o....ingest.sentry.io/...
SENTRY_ENVIRONMENT=staging   # default: APP_ENV / NODE_ENV
SENTRY_RELEASE=               # default: RENDER_GIT_COMMIT if set

# Metrics
METRICS_ENABLED=true          # set false to disable /metrics + middleware
METRICS_TOKEN=optional-secret # if set, require Authorization: Bearer <token>
```

## Prometheus metrics

- `restflow_http_requests_total{method,route,status}`
- `restflow_http_request_duration_seconds{method,route,status}`
- `restflow_http_in_flight`
- `restflow_event_appends_total` / `restflow_projection_errors_total` / `restflow_ws_connections` (for future instrumentation)

## Grafana (example)

1. Add Prometheus datasource scraping `https://YOUR-API.onrender.com/metrics` (use METRICS_TOKEN header if set).
2. Dashboard: request rate, p95 latency, 5xx rate, in-flight.

## Sentry (example)

1. Create project (Go).
2. Set `SENTRY_DSN` on Render.
3. Releases: set `SENTRY_RELEASE` or rely on `RENDER_GIT_COMMIT`.

## Middleware order (outer → inner)

```
SecurityHeaders → Recover → Sentry → RequestID → Metrics → Logger → CORS → MaxBody → RateLimit → routes
```

## Ops checklist

- [ ] Uptime monitor on `/health/ready`
- [ ] Sentry DSN in staging + production
- [ ] Grafana/Prometheus or Render metrics
- [ ] Alert: 5xx rate > threshold
- [ ] Alert: health check failing
