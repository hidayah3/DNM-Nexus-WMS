# Programmer handover by role

## Malaysia developer: WMS owner and support

This repository is the WMS source of truth: React UI, Go API, PostgreSQL migrations, Docker Compose, Caddy config, settings templates, and API contracts. Read `README.md`, then `DEPLOYMENT.md` and `EASYPARCEL-READINESS.md` before changing the live service.

Server access, EasyParcel secrets, database passwords and staff password hashes are not in this repository. The owner grants them separately. Back up the database and the server's `.env` before every deployment, and never overwrite `.env` with an example file.

Keep individual WMS logins for warehouse staff. The inventory audit records the logged-in account, so shared accounts break it. Coordinate shared API changes with the Indonesia team using `SMS-INTEGRATION-CONTRACT.md`.

## Indonesia team: deployment and SMS integration

**Deployment.** Follow `DEPLOYMENT.md`. Before the server is public, turn staff login on in `Caddyfile` (step 4). It is switched off in the repository for local development.

**Order feed.** The WMS pulls paid orders from the K-Link order API (`ORDER_SYNC_URL`). Please confirm:

1. The production URL and credentials for `/api/v1/webhook/wms/order/sync`.
2. Which orders the endpoint returns (e.g. only new/unsent orders, or a date window), and whether calling it marks orders as sent.
3. Whether the response can include the **delivery address, recipient phone, order date and product weight**. Today staff must type these in the WMS before booking a courier.

**SMS endpoints.** Implement these in the SMS repository. Payloads are in `SMS-INTEGRATION-CONTRACT.md`, and `sms-adapter/client.go` is a Go example:

1. Receive WMS inventory snapshots at an authenticated HTTPS endpoint. Apply the absolute SKU quantity idempotently and reject stale revisions.
2. Receive WMS shipment updates at an authenticated HTTPS endpoint. Map tracking number/URL, AWB number/URL, booking state and latest courier status to the SMS order.
3. Provide those callback URLs and tokens to the owner for the server `.env`. Never put tokens in frontend code or the repository.
4. If required, embed the WMS screen in SMS and connect it to SMS staff sessions and permissions. Otherwise staff use the standalone WMS login.

## Joint handover

Agree on identifiers, SKU mapping, status vocabulary, retry/idempotency behaviour, callback authentication and ownership of the production credentials. Once the endpoints are available, configure them on the server, run a controlled check with a genuine order, and verify that stock and shipment updates appear in both systems. Do not run a chargeable production booking without an authorized real order and operator confirmation.
