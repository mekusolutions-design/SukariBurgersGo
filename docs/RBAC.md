# RestFlow RBAC (Michael product rules)

## Roles
`ADMIN` | `MANAGER` | `KITCHEN` | `POS`

## Rules

| Action | ADMIN | MANAGER | KITCHEN | POS |
|--------|-------|---------|---------|-----|
| Menu **write** (create/update catalog, categories, combos, FG assign) | allow | deny | deny | deny |
| Menu **read** | allow | allow | allow | allow |
| POS (orders, payment, menu-availability) | allow | allow | **deny** | allow |
| Kitchen / production | allow | allow | allow | deny* |
| Team list | allow | allow | deny | deny |
| **Change user role** | allow | **deny** | deny | deny |

\*POS users use POS app; kitchen board is kitchen routes.

## API enforcement
- **Nest:** `@Roles('ADMIN')` on menu writes; POS routes exclude `KITCHEN`; `PATCH /users/:id/role` → `ADMIN` only.
- **Go:** `roleAdmin` on menu POSTs and `PATCH /users/{id}/role`; POS routes use `rolePOS` (no kitchen).

## Clients
- **Web:** `canAccess` nav; `canEditMenu` / `canChangeUserRole` / `canUsePos` in `lib/permissions.ts`.
- **Mobile:** `useRole().canUsePos` blocks POS navigation for kitchen.

UI hide is not security — APIs return **403**.
