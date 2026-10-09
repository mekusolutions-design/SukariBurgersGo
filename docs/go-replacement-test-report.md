# Test Report

## Commands run

```
go build ./cmd/api
go test ./tests/unit/ ./pkg/money/
```

## Results

- build: PASS
- unit tests: PASS
- race suite: NOT RUN (optional)
- integration (DATABASE_URL): NOT RUN — no live Postgres in audit environment
- Nest dual-contract suite: NOT RUN
- Frontend e2e: NOT RUN

## UNVERIFIED

Full numerical dashboard/COGS equivalence against Nest on shared DB requires a live environment.
