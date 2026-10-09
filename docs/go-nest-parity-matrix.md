# Nest ↔ Go Route Parity Matrix

| Domain | Nest Route | Go | Method | Roles | Status |
|--------|------------|-----|--------|-------|--------|
| Auth | /auth/login | ✓ | POST | public | matched |
| Auth | /auth/register | ✓ | POST | public | matched |
| Users | /users/me | ✓ | GET | all | matched |
| Users | /users | ✓ | GET | MGR/ADM | matched |
| Users | /users/:id | ✓ | GET | MGR/ADM | matched |
| Users | /users/:id/role | ✓ | PATCH | MGR/ADM | matched |
| Inventory | /inventory | ✓ | GET | all | matched |
| Inventory | /inventory/summary | ✓ | GET | all | matched |
| Inventory | /inventory/alerts | ✓ | GET | all | matched |
| Inventory | /inventory/categories | ✓ | GET | all | matched |
| Inventory | /inventory/:itemId | ✓ | GET | all | matched |
| Received | /received | ✓ | POST | KIT/MGR/ADM | matched |
| POS | /pos/order | ✓ | POST | POS/MGR/ADM | matched |
| POS | /pos/orders | ✓ | GET | +KIT | matched |
| POS | /pos/orders/:id | ✓ | GET | +KIT | matched |
| POS | /pos/orders/:id/send-to-kitchen | ✓ | POST | POS/MGR/ADM | matched |
| POS | /pos/orders/:id/payment | ✓ | POST | POS/MGR/ADM | matched |
| POS | /pos/menu-availability | ✓ | GET | +KIT | matched |
| Kitchen | /kitchen/active-orders | ✓ | GET | KIT/MGR/ADM | matched |
| Kitchen | /kitchen/production-history | ✓ | GET | KIT/MGR/ADM | matched |
| Kitchen | /kitchen/production-queue | ✓ | GET | KIT/MGR/ADM | matched |
| Kitchen | /kitchen/closing-stock | ✓ | GET/POST | KIT/MGR/ADM | matched |
| Kitchen | /kitchen/orders/:id/status | ✓ | POST | KIT/MGR/ADM | matched |
| Kitchen | /kitchen/batches/:itemId | ✓ | GET | KIT/MGR/ADM | matched |
| Recipe | /recipe | ✓ | GET/POST | read all / write MGR | partial (no outputs/PUT/categories yet) |
| Recipe | /recipe/:id | ✓ | GET | all | matched |
| Recipe | /recipe/ingredient | ✓ | POST | MGR | matched |
| Recipe | /recipe/:id/cost | ✓ | GET | all | matched |
| Waste | all Nest waste routes | ✓ | | | matched |
| Production | start/finish | ✓ | POST | KIT | history missing as dedicated route |
| Payment | mpesa + callbacks + stripe webhook | ✓ | | | stripe initiate partial |
| Variance | all | ✓ | | MGR | matched |
| Approvals | pending/history/approve/reject | ✓ | | MGR | get :id partial |
| Sync | /sync/bulk | ✓ | POST | all | matched |
| Dashboard | summary/kpis/alerts | ✓ | | | rebuild-sales missing |
| Notifications | POST | ✓ | | MGR | matched |
| Menu | catalog/combos/preview | ✓ | | | legacy /menu list partial |
| Consumption | all | ✓ | | MGR | matched |
| Costing | /costing/sku/:itemId | ✓ | GET | all | matched |
| Finished goods | list/add/adjust | ✓ | | | matched |
| Refill | all | ✓ | | KIT | matched |
| Trace | all | ✓ | | MGR | matched |
| Health | live/ready | ✓ | | public | matched |
| WS | /ws | ✓ | | JWT | protocol subset of Nest gateways |

## Critical behavioral parity (this pass)

- pos_sale only for inventory + sales projections
- RBAC per Nest matrix
- Combo category membership validation
- Server-authoritative pricing
- Conditional stock update (no concurrent oversell)
