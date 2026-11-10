-- Keep the chosen courier's display name with the fulfilment choice, so the
-- Orders page and Delivery list still show it after a page refresh (before booking).
BEGIN;

ALTER TABLE order_fulfilment ADD COLUMN IF NOT EXISTS courier_name text;

COMMIT;
