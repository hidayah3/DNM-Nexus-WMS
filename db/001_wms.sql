-- WMS stores order snapshots pushed by SMS; SMS remains the order system of record.
CREATE TABLE IF NOT EXISTS wms_order (
    id text PRIMARY KEY,
    order_date timestamptz NOT NULL,
    customer_name text NOT NULL,
    items_summary text NOT NULL,
    status text NOT NULL,
    shipment_draft jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS wms_order_date_idx ON wms_order(order_date DESC);

CREATE TABLE IF NOT EXISTS order_fulfilment (
    order_id text PRIMARY KEY,
    method text NOT NULL CHECK (method IN ('delivery', 'self_collection')),
    courier_code text,
    updated_by text,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((method = 'delivery') OR (method = 'self_collection' AND courier_code IS NULL))
);
ALTER TABLE order_fulfilment DROP CONSTRAINT IF EXISTS order_fulfilment_check;
ALTER TABLE order_fulfilment ADD CONSTRAINT order_fulfilment_check
    CHECK ((method = 'delivery') OR (method = 'self_collection' AND courier_code IS NULL));

CREATE TABLE IF NOT EXISTS delivery_batch (
    id bigserial PRIMARY KEY,
    created_by text,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','completed','partial_failure','failed')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS delivery_batch_order (
    batch_id bigint NOT NULL REFERENCES delivery_batch(id) ON DELETE CASCADE,
    order_id text NOT NULL,
    courier_code text NOT NULL,
    push_status text NOT NULL DEFAULT 'pending' CHECK (push_status IN ('pending','processing','succeeded','failed')),
    error_code text,
    error_message text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (batch_id, order_id)
);

CREATE TABLE IF NOT EXISTS shipment (
    order_id text PRIMARY KEY,
    provider text NOT NULL,
    provider_order_id text,
    service_id text,
    courier_name text,
    shipment_number text,
    tracking_number text,
    tracking_url text,
    awb_number text,
    awb_url text,
    awb_file_key text,
    provider_awb_reference text,
    status text NOT NULL DEFAULT 'created',
    quoted_amount numeric(12,2),
    quoted_currency char(3),
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Shipment payload copied from the SMS order repository by the integration adapter.
-- Keep this table mapped to the real order ID and refresh it when order shipping data changes.
CREATE TABLE IF NOT EXISTS wms_shipment_details (
    order_id text PRIMARY KEY,
    draft jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- OAuth tokens are encrypted by the Go API with WMS_TOKEN_ENCRYPTION_KEY before storage.
CREATE TABLE IF NOT EXISTS wms_provider_auth (
    provider text PRIMARY KEY,
    encrypted_tokens bytea NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- WMS inventory register and immutable movement audit. The SMS auth gateway must
-- authenticate the user and set X-WMS-Actor; never trust this header from the public internet.
CREATE TABLE IF NOT EXISTS wms_inventory_product (
    sku text PRIMARY KEY,
    name text NOT NULL,
    quantity bigint NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS wms_inventory_movement (
    id bigserial PRIMARY KEY,
    sku text NOT NULL REFERENCES wms_inventory_product(sku),
    movement_type text NOT NULL CHECK (movement_type IN ('add','reduce')),
    quantity bigint NOT NULL CHECK (quantity > 0),
    stock_after bigint NOT NULL CHECK (stock_after >= 0),
    reason text NOT NULL,
    actor_id text NOT NULL,
    actor_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS wms_inventory_movement_month_idx ON wms_inventory_movement(created_at,sku);

-- Shared admin-editable DHL charges; seeds are from the supplied sheets and
-- existing edits are preserved when this migration is run again.
CREATE TABLE IF NOT EXISTS wms_shipping_rate (
    region text NOT NULL CHECK (region IN ('west','east')),
    weight_kg integer NOT NULL CHECK (weight_kg BETWEEN 1 AND 10),
    courier_charge numeric(12,2) NOT NULL CHECK (courier_charge >= 0),
    courier_tax numeric(12,2) NOT NULL CHECK (courier_tax >= 0),
    packaging_cost numeric(12,2) NOT NULL CHECK (packaging_cost >= 0),
    updated_by text,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(region,weight_kg)
);
INSERT INTO wms_shipping_rate(region,weight_kg,courier_charge,courier_tax,packaging_cost) VALUES
('west',1,5.48,0.33,2.00),('west',2,5.48,0.33,2.00),('west',3,5.48,0.33,2.00),('west',4,7.36,0.44,3.00),('west',5,8.58,0.51,3.50),
('west',6,9.94,0.60,4.00),('west',7,11.26,0.68,5.00),('west',8,12.59,0.76,5.50),('west',9,13.91,0.83,6.00),('west',10,15.24,0.91,6.50),
('east',1,16.91,1.01,2.00),('east',2,33.73,2.02,2.00),('east',3,50.63,3.04,2.00),('east',4,67.36,4.04,3.00),('east',5,84.27,5.06,3.50),
('east',6,101.08,6.06,4.00),('east',7,117.89,7.07,5.00),('east',8,134.71,8.08,5.50),('east',9,151.62,9.10,6.00),('east',10,168.34,10.10,6.50)
ON CONFLICT(region,weight_kg) DO NOTHING;

-- Additions for databases that already applied the first draft migration.
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS service_id text;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS courier_name text;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS provider_order_id text;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS shipment_number text;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS tracking_url text;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS awb_number text;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS awb_url text;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS quoted_amount numeric(12,2);
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS quoted_currency char(3);
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS last_error text;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS provider_status text;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS provider_status_code integer;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS tracking_status text;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS tracking_status_code integer;
ALTER TABLE shipment ADD COLUMN IF NOT EXISTS provider_event_at timestamptz;

-- EasyParcel sends shipment, AWB, and courier-tracking events to the WMS
-- webhook. Keep an idempotent event ledger, including events that arrive
-- before the synchronous shipment response has been saved.
CREATE TABLE IF NOT EXISTS wms_easyparcel_webhook_event (
    event_key text PRIMARY KEY,
    topic text NOT NULL,
    shipment_number text NOT NULL,
    occurred_at timestamptz NOT NULL,
    provider_status text,
    provider_status_code integer,
    tracking_status text,
    tracking_status_code integer,
    tracking_number text,
    tracking_url text,
    awb_number text,
    awb_url text,
    payload jsonb NOT NULL,
    applied boolean NOT NULL DEFAULT false,
    received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS wms_easyparcel_webhook_pending_idx
    ON wms_easyparcel_webhook_event(shipment_number, occurred_at) WHERE applied=false;

CREATE INDEX IF NOT EXISTS delivery_batch_order_status_idx ON delivery_batch_order(batch_id, push_status);
CREATE INDEX IF NOT EXISTS shipment_tracking_idx ON shipment(tracking_number) WHERE tracking_number IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS shipment_number_idx ON shipment(shipment_number) WHERE shipment_number IS NOT NULL;
