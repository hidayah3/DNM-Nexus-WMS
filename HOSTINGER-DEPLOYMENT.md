# Hostinger deployment and maintenance guide

## Live environment status — 19 October 2026

- Live WMS: https://wms.dnmnexus.com
- Host: Hostinger KVM 2 VPS, Ubuntu, Docker and Docker Compose.
- Services: React static frontend, Go API, private PostgreSQL, and Caddy HTTPS/reverse proxy. Caddy is part of `compose.yaml`; it is not a separate purchase or manual download.
- WMS migrations 001–003 are applied on the live database. The current release includes manual real-order entry and inventory batches/adjustments/exports.
- EasyParcel production OAuth and four shipment webhook subscriptions are configured. This is configuration verification only. No real quote, paid shipment, AWB, or webhook delivery has yet been verified with an actual order.
- The live WMS works independently of SMS for warehouse inventory and staff-entered real orders. SMS order feed, stock receiver, and shipment-update receiver still require the SMS programmer's implementation and configuration.
- No real order or opening inventory was inserted for demonstration. Do not import example/screenshot quantities or prices as live records.

## WMS components and stack

- React frontend served by Nginx in the web container.
- Go API for fulfillment, EasyParcel integration, batch inventory, audit history, Excel exports, webhooks, and SMS outbox callbacks.
- PostgreSQL for WMS orders, shipments, stock batches/movements, audit records, and retryable outboxes.
- Caddy for HTTPS, authenticated staff access, reverse proxying, and webhook routing.
- S3 is not used by current workflows. If persistent product images or uploaded AWB files are added, integrate an S3-compatible store then.

## Staff access and inventory audit

Use a unique authenticated username per warehouse employee. Caddy passes the verified username to the Go API as the stock actor; shared accounts make it impossible to identify which employee made a change. Add users to the private `/opt/wms/wms-staff-users.caddy` file as bcrypt hashes, then reload Caddy. Generate a hash on the VPS with `docker compose exec caddy caddy hash-password --plaintext '<unique password>'`; do not store the plaintext password. Rotate the initial administrator credential before regular operations.

The authenticated employee can receive a lot with product, exact variant SKU, batch number, expiry date, quantity and PO/reason. They can add/reduce a specific lot with a reason. The WMS stores an immutable movement with actor and timestamp, recalculates SKU totals, queues an absolute SKU quantity snapshot for SMS, and provides current stock and monthly movement Excel exports.

## Order and EasyParcel workflow

1. Until SMS sends orders, a warehouse employee can use **Orders → Enter order** to create a WMS record from an actual customer order and enter complete sender/recipient and parcel details.
2. Request live courier rates and select a currently quoted service.
3. Review the delivery list and use **Confirm & send to EasyParcel**. Booking is manual and can incur a production wallet charge. WMS does not top up the EasyParcel wallet.
4. WMS saves shipment results and requests available AWB/tracking details. EasyParcel webhooks can update shipment, AWB, and tracking fields at `/api/easyparcel/webhook/<token>`.
5. Use **Print AWB** when the AWB is available. Use **Send updates to SMS** to send saved shipment status, tracking and AWB fields to the configured SMS receiver.

Use only real, authorized orders for any production transaction. Do not create a fabricated customer/order to test a chargeable booking. The EasyParcel connection and webhook subscriptions have been configured, but a complete live shipment round trip remains unverified until an authorized real order is processed.

## SMS developer work still required

The WMS APIs and callback worker are in this package. SMS code was not provided, so the Indonesia developer must implement the SMS-side endpoints and mapping:

1. Push actual SMS order snapshots to `POST /api/integrations/orders`, using the real SMS order ID and exact variant SKU, complete recipient address and parcel details. Authenticate with the WMS server-to-server bearer token.
2. Implement the authenticated `SMS_INVENTORY_SYNC_URL` receiver. Apply absolute SKU quantities idempotently and ignore older revisions. Configure its URL/token in the private WMS environment.
3. Implement the authenticated `SMS_SHIPMENT_UPDATE_URL` receiver so the WMS **Send updates to SMS** action can write back tracking, AWB, and status by SMS order ID. Configure its URL/token privately.
4. If the WMS screens must appear within SMS, embed/proxy the React UI and replace standalone Basic Auth with SMS session and role authorization. The trusted proxy must derive actor identity from a verified SMS session, never client-supplied headers.

Full request/response examples and field definitions are in `SMS-INTEGRATION-CONTRACT.md`. Use the shipped `sms-adapter/client.go` as a Go example. Adjust ID types and schema mappings to SMS after its source is available.

## Main routes

- `POST /api/integrations/orders` — ingest an SMS order snapshot.
- `POST /api/orders` — create a staff-entered real order.
- `GET /api/orders` — list/filter orders; `PATCH /api/orders/{id}/fulfilment` — save fulfillment/courier selection.
- `POST /api/orders/{id}/courier-quotes` — request current EasyParcel options/prices.
- `POST /api/delivery-batches/push` — explicitly book eligible shipments.
- `POST /api/delivery-batches/awb` — refresh AWB/tracking details.
- `POST /api/delivery-batches/sync-sms` — manually send shipment updates to the configured SMS callback.
- `GET /api/integrations/orders/{id}/shipment` — retrieve persisted shipment information for SMS.
- `POST /api/easyparcel/webhook/{token}` — receive registered shipment/AWB/tracking events.
- `GET /api/inventory`; `POST /api/inventory/receipts`; `POST /api/inventory/adjustments` — query and update stock.
- `GET /api/inventory/report`; `GET /api/inventory/movements?month=YYYY-MM&format=xlsx` — Excel exports.

## Deployment and recovery

The live VPS already has the current source release and migrations applied. For routine WMS debugging, review the Go API, React frontend, SQL migrations, `compose.yaml`, and Caddy config in this archive alongside live container logs. Hostinger account/VPS access is separate from this ZIP and must be granted by the owner to the Malaysia maintainer.

Before any code deployment: back up PostgreSQL and `/opt/wms/.env`; inspect the change; preserve the server's existing secrets; apply only additive migrations; rebuild/restart services with Docker Compose; then check `/health`, staff login, order/inventory screens, and logs. Do not replace `.env` with an example file. No production credentials, password hashes, or webhook secrets are included in this archive.

## Verification limits

Health, database and proxy status were reported healthy after the live deployment. EasyParcel production authentication/webhook configuration is present. The following require a real authorized order and SMS endpoint before they can be called end-to-end verified: live rates, booking, AWB retrieval/printing, delivery of webhooks, and SMS order/stock/shipment synchronization.
