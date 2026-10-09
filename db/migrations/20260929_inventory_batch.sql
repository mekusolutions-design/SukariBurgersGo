-- Optional: enable FEFO lots (Nest InventoryBatch). Safe if already applied.
CREATE TABLE IF NOT EXISTS "InventoryBatch" (
  id            TEXT PRIMARY KEY,
  shop_id       TEXT NOT NULL DEFAULT '1',
  item_id       TEXT NOT NULL,
  batch_number  TEXT NOT NULL,
  qty_received  DOUBLE PRECISION NOT NULL DEFAULT 0,
  qty_remaining DOUBLE PRECISION NOT NULL DEFAULT 0,
  unit_cost     NUMERIC(18, 6) NOT NULL DEFAULT 0,
  received_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expiry_date   TIMESTAMPTZ,
  supplier_id   TEXT,
  event_id      TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS "InventoryBatch_shop_item_expiry_idx"
  ON "InventoryBatch" (shop_id, item_id, expiry_date);
CREATE INDEX IF NOT EXISTS "InventoryBatch_shop_item_remaining_idx"
  ON "InventoryBatch" (shop_id, item_id, qty_remaining);
CREATE INDEX IF NOT EXISTS "InventoryBatch_batch_number_idx"
  ON "InventoryBatch" (batch_number);
