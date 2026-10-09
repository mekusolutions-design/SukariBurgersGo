# RESTFLOW_ROUTE_PARITY.md (partial — critical paths)

| Route | Nest | Go | Web | Behavior | Status |
|-------|------|----|-----|----------|--------|
| GET /trace | Y | Y | Y | Search filters + envelope | FIXED |
| GET /trace/item/:id | Y | Y | Y | Timeline | FIXED |
| GET /production/history | Y (was stub) | Y | Y | Event fold | FIXED |
| GET /kitchen/production-history | Y | Y | Y | Exists | PARTIAL |
| POST /pos/order | Y | Y | Y | Server price | PRIOR FIX |
| GET /inventory | Y | Y | Y | List | PARTIAL |
| GET /waste/analytics | Y | Y | Y | Shape risks | PARTIAL |
| GET /menu | Y | Y | Y | Catalog vs events | PARTIAL |
| WS namespaces | multi | /ws | Y | Parity unproven | BLOCKED |

Legend: FIXED this pass | PARTIAL | BLOCKED for certification
