-- Adds cancel / complete tracking to orders.
ALTER TABLE wms_order
  ADD COLUMN IF NOT EXISTS cancel_remark  text,
  ADD COLUMN IF NOT EXISTS cancelled_at   timestamptz,
  ADD COLUMN IF NOT EXISTS cancelled_by   text,
  ADD COLUMN IF NOT EXISTS cancelled_name text,
  ADD COLUMN IF NOT EXISTS completed_at   timestamptz,
  ADD COLUMN IF NOT EXISTS completed_by   text;