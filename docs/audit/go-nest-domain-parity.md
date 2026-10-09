# RestFlowGo domain corrections

## Verified already correct
- Projection: only pos_sale deducts inventory
- CreateOrder: server-resolved order-level stock_deductions
- Consumption: order-level stock_deductions only

## This package
1. Combo classic/expanded Nest parity
2. SendToKitchen nested stock_deductions strip
3. Variance 0.00 hide (isMaterialVariance)
4. Pre-prep POST /production/pre-prep + projection
