# CLIENT_CONTRACT_AUDIT.md

## Web (Next.js)

- Central catalog: `apps/web/src/lib/api/endpoints.ts`
- Call pattern: `apiClient` + React Query; many features use `asArray()` unwrap
- **Fragile assumption:** some pages still expect bare arrays; Go historically returned `{items}` (dashboard alerts fixed)
- POS New order: `maxPortions <= 0 || !isAvailable` → 86'd
- Trace expects searchable events; joins must use table `users` (Prisma `@@map("users")`)

## Mobile (Expo)

- Single client: `apps/mobile/src/api/apiClient.ts` + `API_ENDPOINTS`
- **One base URL** — cannot hybrid per-feature without client changes
- Paths: `/auth/login|register`, `/received`, `/inventory`, `/kitchen/closing-stock`, `/kitchen/active-orders`, `/pos/orders`, `/waste`, `/notifications`, settings
- Offline: store + queue; needs idempotency keys on mutations

## Contract rules for Go

1. Prefer Nest response field names (camelCase where web maps both).  
2. Always accept `shopId` query but **authorize from JWT shop**.  
3. List endpoints: support `{items,total,page,pageSize}` **and** bare array via web `asArray`.  
4. Errors: stable JSON `{statusCode,message,error}` compatible with web `toApiError`.
