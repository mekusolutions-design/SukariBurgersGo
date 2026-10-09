# Production deploy on Render (Nest replacement)

## Pre-flight (required once)

1. **Schema**: Point Prisma at the target DB and apply migrations:
   ```bash
   cd apps/api
   export DATABASE_URL="<Render internal URL>"
   npx prisma migrate deploy
   ```
2. **Seed admin** if needed (`prisma/seed-admin.ts`).
3. Confirm Nest still works against that DB (optional sanity).

## Deploy Go

1. Push this directory to GitHub.
2. Render → Blueprint → `render.yaml`, **or** Web Service + Docker:
   - Health check: `/health/ready`
   - Env: `APP_ENV=production`, `JWT_SECRET`, `DATABASE_URL`, `CORS_ORIGIN`, `BASE_URL`
3. After green deploy, smoke:
   ```bash
   curl -s https://API/health/ready
   curl -s -X POST https://API/auth/login -H 'Content-Type: application/json' \
     -d '{"email":"admin@...","password":"..."}'
   ```
4. Point frontend `NEXT_PUBLIC_API_URL` (and mobile API base) to the Go URL.
5. Keep Nest scaled down for 48h for rollback.

## Post-cutover checks

- [ ] Login / register
- [ ] Receive goods → inventory increases
- [ ] POS order → stock decreases
- [ ] Send to kitchen → appears in active-orders
- [ ] Kitchen status transitions
- [ ] M-Pesa/Stripe if configured
- [ ] WebSocket: `wss://API/ws?token=<jwt>`

## Rollback

Repoint frontend env to Nest URL. Shared Postgres event store remains valid for both.
