-- Enable standalone staff entry of actual customer orders in WMS.
-- Imported SMS orders continue to use the existing integration endpoint.
BEGIN;

ALTER TABLE wms_order ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'sms';
ALTER TABLE wms_order ADD COLUMN IF NOT EXISTS created_by text;
ALTER TABLE wms_order ADD COLUMN IF NOT EXISTS created_name text;
ALTER TABLE wms_order DROP CONSTRAINT IF EXISTS wms_order_source_check;
ALTER TABLE wms_order ADD CONSTRAINT wms_order_source_check CHECK (source IN ('sms','manual'));

COMMIT;
