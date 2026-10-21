# SMS Integration Contract for DNM Nexus WMS

This contract describes the boundary between the standalone WMS and SMS using only fields visible in the Product Update page and WMS API. It does not assume SMS database table names or claim that the browser UI exposes an API.

## Product identifier mapping verified from SMS

The Product Update page shows a parent-level `Product Code` and a variant-level `Product Code`. Use the **variant-level product code as the WMS SKU**. Keep the parent code to group variants. Examples from the screenshots:

| Parent product code | Variant SKU | Product | Weight |
|---|---|---|---:|
| HD001 | HD001-I | NEXPRO CHLOROPHYLL | 700 g |
| HD003 | HD003-I | NEX TIGER MILK MUSHROOM COFFEE | 400 g |
| HD005 | HD005-I | NEXGEN CHLOROPHYLL | 350 g |
| HF001 | HF001-I | NEXCELL | 110 g |
| NP/HF002 | HF002-I | NEXLAX | 800 g |
| NEXLIFE001 | NEXLIFE001 | NEXLIFE | 2,000 g |

The `East` and `West` figures in these product details are **regional prices**, not regional stock. The separate `Stock` field is the stock quantity. The quantities/prices shown are test values and must not be imported as real data. The SMS programmer should use the variant code for matching. WMS maintains warehouse quantity by that SKU and sends each committed absolute balance back to SMS; SMS continues to own catalogue and regional-price fields.

## Required SMS adapter behavior

Implement these operations in the SMS Go backend or a small Go adapter package. Keep the WMS React UI and route payloads unless a thin translation layer is needed for SMS conventions.

1. **List orders** — return order ID, date, customer display name, item summary, status, and saved fulfillment/courier/shipment fields for an inclusive date range; enforce existing tenant/site and user permissions.
2. **Save fulfillment** — persist `delivery` or `self_collection` and the selected EasyParcel service ID for an accessible SMS order.
3. **Get shipment draft** — map the order plus warehouse sender to the fields represented by `ShipmentDraft`: contacts and addresses, Malaysian postcode/subdivision, parcel dimensions/weight/value/currency, item content/quantity, and collection date. Reject incomplete data before calling EasyParcel.
4. **Map products** — match each WMS inventory SKU to the SMS variant-level Product Code. SMS remains authoritative for catalogue details and regional prices; WMS becomes the warehouse stock ledger.
5. **Receive WMS stock snapshots** — implement the authenticated receiver below and update the SMS stock quantity by exact variant SKU. Apply the absolute quantity, never an increment/decrement, and ignore older revisions.
6. **List monthly movements** — use WMS's account-attributed history or synchronize it if SMS needs the audit in its own reporting screens.

The WMS stores order snapshots in its own `wms_order` table. SMS sends orders to WMS; WMS does not connect to or read the SMS database. SMS remains the source of truth for orders and product catalogue/price fields. WMS records warehouse stock by SKU and batch, then automatically sends absolute stock snapshots back to SMS after every receipt or adjustment.

### SMS-to-WMS order snapshot

SMS sends a server-to-server `POST /api/integrations/orders` with `Authorization: Bearer <SMS_WMS_INGEST_TOKEN>` over HTTPS. The token is server-only and must be stored in both services' secret managers. `date` must be an RFC3339 timestamp. `shipment` is the EasyParcel `ShipmentDraft` shape; order IDs are copied to the shipment reference by WMS.

```json
{
  "id": "<SMS order ID>",
  "date": "<RFC3339 order timestamp>",
  "customer": "<customer display name>",
  "items": "<order item summary>",
  "status": "<SMS order status>",
  "shipment": {
    "collection_date": "<YYYY-MM-DD>",
    "sender": { "name": "<warehouse>", "phone_number_country_code": "MY", "phone_number": "<number>", "address_1": "<address>", "postcode": "<postcode>", "city": "<city>", "subdivision_code": "<code>", "country_code": "MY" },
    "receiver": { "name": "<customer>", "phone_number_country_code": "MY", "phone_number": "<number>", "address_1": "<address>", "postcode": "<postcode>", "city": "<city>", "subdivision_code": "<code>", "country_code": "MY" },
    "weight": 1,
    "height": 10,
    "length": 20,
    "width": 15,
    "parcel_value": 100,
    "currency_code": "MYR",
    "item": [{ "content": "<contents>", "weight": 1, "height": 10, "length": 20, "width": 15, "currency_code": "MYR", "value": 100, "quantity": 1 }]
  }
}
```

### WMS-to-SMS inventory snapshots

After a warehouse receipt or batch adjustment commits in PostgreSQL, WMS creates an outbox event in the same transaction. A Go worker POSTs each current SKU balance to `SMS_INVENTORY_SYNC_URL` with `Authorization: Bearer <SMS_INVENTORY_SYNC_TOKEN>`. Delivery retries automatically after network or non-2xx failures. Configure both variables privately on the WMS server; they are never exposed to React. Return HTTP 2xx only after SMS has durably applied the snapshot.

Example request:

```json
{
  "event": "wms.inventory.updated",
  "sent_at": "<RFC3339 UTC timestamp>",
  "inventory": [
    {
      "sku": "<SMS variant-level Product Code>",
      "product_name": "<product name>",
      "quantity": 123,
      "revision": 482
    }
  ]
}
```

The request also carries `Idempotency-Key: wms-inventory-<revision>`. The revision increases across WMS outbox events; SMS must persist the latest applied revision per SKU and ignore any older revision that arrives after a retry. Apply `quantity` as the SKU's absolute on-hand balance. The WMS does not send regional price fields. The SMS programmer must implement this receiver and share its HTTPS URL/token with the WMS maintainer; until then, stock changes remain safely queued in WMS.

WMS responds `202` after storing/updating the snapshot. This patch includes `sms-adapter/client.go`, a Go standard-library client for these two calls. Copy that package into the SMS Go repository, map SMS fields to `OrderSnapshot`, and call `SendOrder` from the SMS order-save/outbox worker. Call `GetShipment` after a WMS shipment action or from SMS's existing order refresh worker, then map `tracking_number`, `tracking_url`, `awb_number`, `awb_url`, `shipment_number`, and `status` onto the SMS order. Keep SMS's current authentication and retry queue; do not call WMS from the browser.

Minimal SMS-side usage:

```go
client, err := wmsadapter.New(os.Getenv("WMS_API_URL"), os.Getenv("SMS_WMS_INGEST_TOKEN"), nil)
if err != nil {
	return err
}

// Call from an outbox job after the SMS order transaction commits.
err = client.SendOrder(ctx, wmsadapter.OrderSnapshot{
	ID: smsOrder.ID, Date: smsOrder.CreatedAt, Customer: smsOrder.CustomerName,
	Items: itemSummary, Status: smsOrder.Status,
	Shipment: wmsadapter.ShipmentDraft{
		Sender: warehouseAddress, Receiver: customerAddress,
		CollectionDate: collectionDate, Weight: totalWeight,
		Height: height, Length: length, Width: width,
		ParcelValue: orderValue, CurrencyCode: "MYR", Items: parcelItems,
	},
})

// After staff pushes the order in WMS, sync the saved result into the SMS row.
shipment, err := client.GetShipment(ctx, smsOrder.ID)
if err != nil {
	return err // retry through the existing SMS job queue
}
// Persist shipment.TrackingNumber, shipment.TrackingURL, shipment.AWBNumber,
// shipment.AWBURL, shipment.ShipmentNumber, and shipment.Status in SMS.
```

After EasyParcel accepts a shipment, WMS immediately asks EasyParcel for shipment details and persists any tracking/AWB fields returned. If the AWB URL is not ready yet, staff can retry with **Print AWB**. The delivery page opens the EasyParcel AWB URL for printing; the PDF remains hosted by EasyParcel.

For automatic updates, the Go API exposes `POST /api/easyparcel/webhook/<secret>`. The secret is a random 32-byte value stored only as `EASYPARCEL_WEBHOOK_TOKEN` on the WMS server and included in the endpoint path registered in EasyParcel Developer Hub. Caddy forwards that path directly to Go without the preview's human password; Go compares the token in constant time, accepts only the four shipment topics, persists each event idempotently, and links it by EasyParcel `shipment_number`. Events arriving before the initial EasyParcel response are held and applied after WMS saves that shipment number. Provider status/tracking status are kept separate from WMS's booking state, and older events cannot replace newer values. The Orders list refreshes every 30 seconds.

Subscribe the same HTTPS endpoint to `shipment.created`, `shipment.status.update`, `shipment.awb.update`, and `shipment.tracking.update`. EasyParcel's OpenAPI documentation says the endpoint must accept public POST requests, receive JSON, and return HTTP 200; it may retry failed delivery. The WMS receiver returns 200 only after the event has been durably saved. The webhook topics and payloads are documented in the [EasyParcel OpenAPI](https://easyparcel.github.io/OpenAPI/?shell#version-2026-09). The docs shown for these topics do not specify a signature header, so this WMS uses the high-entropy secret URL path as the receiver credential. Rotate it by changing the server secret and updating the Developer Hub endpoint together.

The Delivery list has an explicit **Send updates to SMS** button. After staff prints the AWB, they can click it to POST each selected saved shipment to `SMS_SHIPMENT_UPDATE_URL`, using `Authorization: Bearer <SMS_SHIPMENT_UPDATE_TOKEN>`. Configure both values on the WMS server; never put the callback token in React. SMS must provide a receiving API that authenticates this token and updates its order by `order_id` idempotently. The payload includes WMS booking status, EasyParcel provider status, tracking status/code and timestamp, tracking number/URL, and AWB number/URL. A successful HTTP 2xx response means SMS accepted the update.

Example callback body:

```json
{
  "event": "wms.shipment.updated",
  "sent_at": "<RFC3339 UTC timestamp>",
  "shipments": [
    {
      "order_id": "<SMS order ID>",
      "service_id": "<EasyParcel service ID>",
      "shipment_status": "created",
      "provider_status": "<latest EasyParcel shipment status>",
      "tracking_status": "<latest courier event, such as collected or delivered>",
      "tracking_status_code": 5,
      "provider_event_at": "<RFC3339 timestamp>",
      "shipment_number": "<EasyParcel shipment number>",
      "tracking_number": "<tracking number when available>",
      "tracking_url": "<tracking URL when available>",
      "awb_number": "<AWB number when available>",
      "awb_url": "<AWB URL when available>",
      "awb_status": "ready"
    }
  ]
}
```

`shipment_status` is the WMS booking state (normally `created` after the shipment is booked). `provider_status` and `tracking_status` contain EasyParcel's later shipment/tracking events. The SMS receiver should map those provider values to SMS's status vocabulary and should only mark an order delivered when a delivered event is actually received.

## Existing WMS routes to connect

| Route | SMS adapter behavior |
|---|---|
| `GET /api/orders?from=YYYY-MM-DD&to=YYYY-MM-DD` | Read WMS order snapshots previously pushed by SMS, plus fulfillment/shipment state. |
| `POST /api/integrations/orders` | Accept an SMS order snapshot into the WMS-owned order store; requires the server-to-server bearer token. |
| `GET /api/integrations/orders/{id}/shipment` | Return WMS-persisted EasyParcel tracking/AWB state to SMS; requires the same server-to-server token. |
| `POST /api/orders/{id}/courier-quotes` | Ask EasyParcel for current courier options and prices using the saved parcel/address draft; no shipment is booked. Requires a signed-in warehouse actor. |
| `POST /api/easyparcel/webhook/<secret>` | Accept EasyParcel shipment, AWB, and tracking events; the random path token is verified by Go. Configure the full URL in EasyParcel Developer Hub. |
| `PATCH /api/orders/{id}/fulfilment` | Save fulfillment metadata for an accessible SMS order. |
| `GET /api/inventory` | Read WMS aggregate product balances and batch rows. The exact SKU maps to the SMS variant-level Product Code. |
| `POST /api/inventory/receipts` | Receive product, SKU, batch number, expiry date, quantity, and reason/PO reference; records the authenticated account, adds the stock, writes immutable movement history, and queues an SMS snapshot. |
| `POST /api/inventory/adjustments` | Add/reduce quantity in a specific `lot_id` with a reason; update aggregate SKU balance, write account-attributed history, and queue the new SMS snapshot in one transaction. |
| `GET /api/inventory/report` | Download the current product/SKU/batch/expiry/quantity/input-account report as `.xlsx`. |
| `GET /api/inventory/movements?month=YYYY-MM` | Read monthly WMS movement history, including batch, expiry, reason, actor, and resulting product balance. |
| `GET /api/inventory/movements?month=YYYY-MM&format=xlsx` | Download monthly movement history as `.xlsx`. |
| `POST SMS_INVENTORY_SYNC_URL` | SMS-owned HTTPS receiver for automatic WMS product/SKU/quantity snapshots; authenticate bearer token and apply idempotent, revision-ordered absolute quantities. |
| `POST /api/delivery-batches/push` | Verify each order is accessible and marked Delivery, load the current shipment draft, submit with the configured EasyParcel service ID, persist the response, and attempt to retrieve ready AWB details immediately. |
| `POST /api/delivery-batches/awb` | Retry retrieving AWBs for successfully persisted shipments. |
| `POST /api/delivery-batches/sync-sms` | Admin-triggered callback of saved shipment status, tracking, and AWB data to the configured SMS endpoint; requires an authenticated WMS admin and `SMS_SHIPMENT_UPDATE_URL` plus `SMS_SHIPMENT_UPDATE_TOKEN`. |

## Authentication and deployment

- The standalone WMS can use its own HTTPS Basic Auth staff accounts. The current Caddy config derives the actor ID/name from `{http.auth.user.id}` only after a successful login; use one unique username per employee. When mounting in SMS, replace this gate with SMS sign-in, menu permissions, and logout behavior.
- Never trust actor identity headers supplied by a browser. Caddy must set the actor headers from its authenticated login or the trusted SMS proxy must strip client copies and populate them from its verified session.
- Keep EasyParcel credentials and token-encryption key in the server secret manager, never React build variables or the ZIP.
- Adapt the migration to actual SMS ID types and schema. Preserve existing data and do not import screenshot values.
- Route the EasyParcel OAuth callback to the deployed API over HTTPS and register the same URI in EasyParcel Developer Hub.
- Generate a new 32-byte webhook token for each environment, store it in the Go server environment, and register `https://<wms-domain>/api/easyparcel/webhook/<token>` with the four shipment topics in EasyParcel Developer Hub.
- Verify with disposable SMS test records and the EasyParcel sandbox. Do not use production customer details for sandbox shipments.

## Current WMS status and SMS-side work

The WMS source package supplies the React inventory UI, Go receipt/adjustment APIs, PostgreSQL audit/outbox schema, Excel exports, retrying stock-snapshot worker, and EasyParcel fulfilment flow. The hosted WMS at https://wms.dnmnexus.com is deployed with migrations 001–003. SMS-side work remains: send the SMS order outbox to WMS, implement authenticated inventory and shipment-update receivers, map SMS SKUs/order identifiers/statuses, and embed the WMS screens under SMS staff authentication if a single portal is required.

The SMS source was not provided and is not part of this archive. This means the contract and WMS client are ready for the Indonesia developer, but the SMS repository must be updated separately. Configure endpoint URLs and tokens privately on the WMS server. The production EasyParcel configuration is in place; live rates, booking, AWB, webhook and SMS round-trip still need verification using a genuine authorized order. Do not create a fabricated production order or shipment.
