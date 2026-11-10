-- Orders are now pulled from the K-Link order API instead of being typed in.
BEGIN;

-- The raw order JSON from the API, and when the WMS last received it.
ALTER TABLE wms_order ADD COLUMN IF NOT EXISTS source_payload jsonb;
ALTER TABLE wms_order ADD COLUMN IF NOT EXISTS synced_at timestamptz;

-- One row per call to the order API. The raw response is kept so orders can be
-- recovered if the API marks them as sent and something fails on the WMS side.
CREATE TABLE IF NOT EXISTS order_sync_run (
    id            bigserial PRIMARY KEY,
    started_at    timestamptz NOT NULL DEFAULT now(),
    finished_at   timestamptz,
    raw_response  text NOT NULL,
    order_count   int NOT NULL DEFAULT 0,
    created_count int NOT NULL DEFAULT 0,
    updated_count int NOT NULL DEFAULT 0,
    skipped_count int NOT NULL DEFAULT 0,
    failed_count  int NOT NULL DEFAULT 0,
    error         text
);
CREATE INDEX IF NOT EXISTS order_sync_run_started_idx ON order_sync_run(started_at DESC);

COMMIT;
