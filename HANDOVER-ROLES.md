# Programmer handover by role

## Malaysia developer — WMS owner and support

Use this archive as the WMS source-of-truth handoff. It contains the React UI, Go API, PostgreSQL schema/migrations, Docker Compose, Caddy config, environment examples, deployment and recovery guidance, EasyParcel status notes, and API contracts. Read `README.md`, then `HOSTINGER-DEPLOYMENT.md` and `EASYPARCEL-READINESS.md` before changing the live service.

The live WMS is at https://wms.dnmnexus.com. The package does not include VPS credentials, EasyParcel secrets, database passwords, or staff password hashes. The owner must grant Hostinger/VPS access separately for live logs, recovery, deployment, and staff account setup. Back up the database and server secrets before deployment; do not overwrite `/opt/wms/.env` with an example.

Maintain individual WMS logins for warehouse staff. Inventory audit records the authenticated account; shared accounts defeat the per-person audit requirement. Coordinate any shared API changes with the Indonesia SMS developer using `SMS-INTEGRATION-CONTRACT.md`.

## Indonesia developer — SMS owner and integration

The WMS integration endpoints and payloads are documented in `SMS-INTEGRATION-CONTRACT.md`; `sms-adapter/client.go` is a Go example. SMS source code is not included because it was not provided. Implement the following in the SMS repository:

1. Send real order snapshots to WMS `POST /api/integrations/orders`, with a stable SMS order ID, actual customer/address/parcel fields, and exact variant SKU.
2. Receive WMS inventory snapshots at an authenticated SMS HTTPS endpoint; apply absolute SKU quantity idempotently and reject stale revisions.
3. Receive WMS shipment updates at an authenticated SMS HTTPS endpoint; map tracking number/URL, AWB number/URL, booking state, and latest courier status to the SMS order.
4. Provide the HTTPS callback URLs and tokens to the owner for private WMS server configuration. Never place tokens in frontend code or this archive.
5. If required, embed the WMS screen in SMS and connect it to SMS staff sessions/permissions. Otherwise staff can use the standalone WMS login.

## Joint handover

Agree on identifiers, SKU mapping, status vocabulary, retry/idempotency behavior, callback authentication, and ownership of the production credentials. Once SMS endpoints are available, configure them on the VPS, use a genuine SMS order for a controlled integration check, and verify stock and shipment updates in both systems. Do not run a chargeable production booking without an authorized real order and operator confirmation.

## Scope boundary

The WMS and its EasyParcel adapter are provided here; the SMS code changes cannot be completed without the SMS repository and its schema/authentication details. The package supports the integration but does not claim that SMS is already connected. EasyParcel production configuration is in place, while live quote/booking/AWB/webhook behavior still awaits verification with a genuine order.
