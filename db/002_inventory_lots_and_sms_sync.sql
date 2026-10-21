-- Add batch/expiry tracking and a durable SMS stock-snapshot outbox.
-- Safe to apply after 001_wms.sql; existing aggregate balances are preserved.
BEGIN;

CREATE TABLE IF NOT EXISTS wms_inventory_lot (
    id bigserial PRIMARY KEY,
    sku text NOT NULL REFERENCES wms_inventory_product(sku),
    batch_number text NOT NULL,
    expiry_date date,
    quantity bigint NOT NULL CHECK (quantity >= 0),
    created_by text NOT NULL,
    created_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (sku, batch_number)
);
CREATE INDEX IF NOT EXISTS wms_inventory_lot_expiry_idx ON wms_inventory_lot(expiry_date,sku);

ALTER TABLE wms_inventory_movement ADD COLUMN IF NOT EXISTS lot_id bigint REFERENCES wms_inventory_lot(id);
ALTER TABLE wms_inventory_movement ADD COLUMN IF NOT EXISTS batch_number text;
ALTER TABLE wms_inventory_movement ADD COLUMN IF NOT EXISTS expiry_date date;

-- Preserve any aggregate stock already entered in an earlier WMS version.
-- The expiry is intentionally NULL because it cannot be inferred from old data.
INSERT INTO wms_inventory_lot(sku,batch_number,expiry_date,quantity,created_by,created_name)
SELECT p.sku,'LEGACY-' || p.sku,NULL,p.quantity,'migration','Legacy stock (original inputter unknown)'
FROM wms_inventory_product p
WHERE p.quantity>0 AND NOT EXISTS (SELECT 1 FROM wms_inventory_lot l WHERE l.sku=p.sku);

CREATE TABLE IF NOT EXISTS wms_inventory_sync_outbox (
    id bigserial PRIMARY KEY,
    sku text NOT NULL,
    product_name text NOT NULL,
    quantity bigint NOT NULL CHECK (quantity >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error text
);
CREATE INDEX IF NOT EXISTS wms_inventory_sync_pending_idx
    ON wms_inventory_sync_outbox(next_attempt_at,id) WHERE sent_at IS NULL;

COMMIT;
