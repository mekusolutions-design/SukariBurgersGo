# Intentional / remaining differences

1. Nest `order_sent_to_kitchen` historically could re-apply stock_deductions if present in payload — **both fixed** to inventory-only on `pos_sale`.
2. Go M-Pesa callback security: structural + optional secret; Nest may have more HMAC modes — verify against production config.
3. Recipe PUT / outputs / categories endpoints still thinner than Nest.
4. Menu legacy event-fold list endpoints partial; catalog read models preferred.
5. Dashboard rebuild-sales job not exposed as HTTP (use worker CLI).
6. Stripe payment initiation route may be thinner than Nest.
7. WebSocket event names may not match Nest gateway event names 1:1.
