# DNM Nexus WMS

Warehouse management system for DNM Nexus: orders, delivery booking through EasyParcel, and batch-level stock. Built with React, Go and PostgreSQL, matching the SMS stack. It contains no demo orders or demo inventory.

## What it does

- **Orders:** paid orders sync automatically from the K-Link order API every few minutes, and staff can also sync on demand. The API does not send delivery addresses, so staff add the address and parcel size in the WMS before booking.
- **Delivery:** live EasyParcel rates, explicit booking confirmation, AWB and tracking, EasyParcel webhooks, Print AWB, commercial invoice, and shipment updates to SMS.
- **Order status:** cancel (with a reason) or mark complete.
- **Inventory:** stock receipts and batch-level additions/reductions with SKU, batch, expiry, quantity, reason, timestamp and the staff account that made the change.
- **Reports:** current-balance and monthly movement Excel exports.
- **SMS stock sync:** a retryable outbox sends absolute SKU quantities to an SMS receiver.

## Repository layout

| Path | Contents |
|---|---|
| `api/` | Go API (`cmd/server` entry point, `internal/` handlers, store, integrations) |
| `web/` | React frontend (Vite) |
| `db/` | PostgreSQL migrations `001`–`007`, applied in order |
| `compose.yaml`, `Caddyfile` | Docker Compose deployment and Caddy HTTPS / routing |
| `.env.example`, `.env.deploy.example` | Settings templates (no secrets) |
| `sms-adapter/` | Go example client for the SMS side |

## Documentation

- **WMS Technical Documentation** (shared separately): the full handbook with architecture, flowcharts, ERD, API, integrations, operations and known issues. **Start there.**
- **`DEPLOYMENT.md`**: how to deploy and update the WMS on a server.
- **`SMS-INTEGRATION-CONTRACT.md`**: payloads exchanged between SMS and the WMS.
- **`EASYPARCEL-READINESS.md`**: EasyParcel setup notes.
- **`HANDOVER-ROLES.md`**: who owns what between the Malaysia and Indonesia teams.

## First operational use

1. Give each warehouse staff member their own account (see `DEPLOYMENT.md`, step 4). Stock changes are recorded against the signed-in account.
2. Enter actual opening stock at **Inventory → Receive stock**, using the SMS variant-level product code as the SKU, with batch, expiry and real quantity. Don't use the East/West prices as stock counts.
3. Orders appear under **Orders** automatically. For each delivery order, click **Needs delivery details**, enter the address and parcel size, then **Get live rates**, choose a courier and confirm the booking. Production bookings may incur postage charges.
4. Use **Print AWB** once EasyParcel has returned shipment data.

## Migrations

For a fresh database, apply `db/001_wms.sql` through `db/007_users_sessions.sql` in order (commands in `DEPLOYMENT.md`). Migrations are additive and safe to re-run. Migration 002 keeps earlier stock totals in a legacy lot with unknown expiry and inputter; it does not invent those details.

## Secrets

`.env`, OAuth tokens, webhook secrets, database passwords and staff password hashes are never committed. Keep the server's `.env` private, and never replace them with the example files.

## Verification status

EasyParcel production OAuth and webhook configuration were set up, but a full round trip has not been verified yet: live rate, paid booking, AWB and webhook. Verify it with a genuine, authorized order.
