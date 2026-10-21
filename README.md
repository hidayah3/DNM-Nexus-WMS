# DNM Nexus WMS

## Current release

This package contains the WMS source and programmer handover for the build deployed at https://wms.dnmnexus.com on 19 October 2026. The deployed WMS is a standalone, usable system; it does not need SMS source code to record stock or to enter and process a real order. It contains no demo orders or demo inventory.

The live deployment uses React, Go, and PostgreSQL, matching the SMS stack. S3 is not used by the current WMS workflows because they do not upload product images or AWB files. EasyParcel stores shipment documents and returns AWB/tracking references to WMS.

## Included

- React WMS screens and Go API source.
- PostgreSQL schema and additive migrations 001–003.
- Docker Compose deployment, Caddy HTTPS/reverse-proxy configuration, and non-secret environment examples.
- Authenticated order list, date filtering, fulfillment selection, manual real-order entry, live EasyParcel quotes, explicit booking confirmation, AWB/tracking retrieval, webhook receiver, Print AWB, and manual shipment updates to SMS.
- Inventory receipts and batch-level additions/reductions with product, exact SKU, batch, expiry, quantity, reason/PO reference, timestamp, and authenticated actor audit.
- Current-balance and monthly movement Excel exports.
- Retryable outbox for sending absolute SKU/product/quantity snapshots to an SMS receiver.
- SMS API contract, Go adapter, payload examples, deployment notes, and a role-based programmer handover.

## Live deployment state

The Hostinger VPS deployment is live at https://wms.dnmnexus.com. Its Go health endpoint, PostgreSQL, and Caddy were reported healthy after deployment. Migrations 001–003 are applied. EasyParcel production OAuth and the four shipment webhook subscriptions are configured. This confirms configuration, not a completed live shipment: no real order has yet been used to verify a quote, paid booking, AWB, or webhook round trip. No shipment has been created by this handover.

The current WMS login is the existing initial administrator account. Before routine stock operations, create a unique account for each warehouse staff member and rotate any shared/initial credentials. Each person's own login is what attributes stock changes in the audit trail.

## First operational use

1. Add a unique WMS staff login for each operator, following `HOSTINGER-DEPLOYMENT.md`.
2. Enter actual opening stock at **Inventory → Receive stock**, using the SMS product's variant-level product code as SKU. Enter its batch, expiry and real quantity. Do not use the East/West prices as stock counts.
3. To process an actual order before SMS is connected, use **Orders → Enter order** and enter the real order, recipient and parcel details. Request a current EasyParcel quote, select a service, and explicitly confirm booking. Production booking may incur postage charges.
4. Use **Print AWB** after EasyParcel has returned shipment data. Use **Send updates to SMS** once the SMS team has supplied and configured its receiver.

## SMS integration boundary

The WMS side is implemented. The SMS source and SMS-side endpoints were not supplied, so SMS-side changes are not in this archive. The Indonesia SMS developer must use `SMS-INTEGRATION-CONTRACT.md` to connect the SMS order feed, receive WMS stock snapshots, and accept shipment updates. Configure the real SMS endpoint URLs and tokens privately on the VPS; never put secrets in this ZIP or React variables.

The WMS can be used at its standalone URL now. Showing its screens inside the SMS portal and using SMS sign-in requires SMS-side embedding/proxy/auth changes. For inventory audit integrity, each staff member needs an individual authenticated WMS account.

## Migrations

The live VPS already has migrations 001–003 applied. For a fresh environment, apply `db/001_wms.sql`, `db/002_inventory_lots_and_sms_sync.sql`, and `db/003_manual_orders.sql` in order. Migration 002 preserves prior aggregate quantities in a legacy lot with unknown expiry/inputter; it does not invent those details.

## Secrets

This handoff deliberately excludes `.env`, OAuth tokens, webhook secrets, database passwords, and staff password hashes. Keep the existing VPS `/opt/wms/.env` and staff account file private. Never replace the live secrets with the `.env.example` files.
