# Integration tests

Require a live PostgreSQL with RestFlow schema applied (Prisma migrations).

```bash
export DATABASE_URL=postgresql://postgres:postgres@localhost:5432/restflow?sslmode=disable
export JWT_SECRET=test-secret
go test ./tests/integration/ -v -count=1
```

Suggested cases:
1. Register → login → /users/me
2. POST /received → inventory increases
3. POST /pos/order → stock deductions → inventory decreases
4. send-to-kitchen → kitchen active-orders
5. Duplicate idempotency key → replay, no double stock move
6. Cross-shop: user shop A cannot read shop B inventory
