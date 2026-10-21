# DNM Nexus WMS v7 — standalone real-order operation

## Changes in this source release

- Added authenticated `POST /api/orders` and **Orders → Enter order** for staff to enter actual orders directly into WMS while the SMS order feed is pending.
- Manual order records include the real order reference/date, customer and item summary, EasyParcel-ready sender/recipient addresses, parcel contents, weight/dimensions and declared value. The creating staff account is recorded.
- A manually entered order follows the same EasyParcel flow as an SMS order: request live rates, select a quoted service, and explicitly confirm shipment booking. Booking can incur postage charges.
- Duplicate order IDs are rejected rather than silently replaced.
- Added additive migration `db/003_manual_orders.sql`; no sample orders were added.
- Caddy is included in Docker Compose and does not require a separate download or paid service.

## Stack

React frontend, Go API, PostgreSQL. S3 is not used by current screens because no images or AWB files are uploaded.

## Deployment status

This release and migrations 001–003 have since been deployed to the live WMS. The standalone WMS can be used for real stock records and staff-entered real orders. SMS-side order, inventory receiver, and shipment callback changes are still required for automatic cross-system synchronization.

EasyParcel production OAuth and webhook topics are configured, but no real order has yet verified live rates, chargeable booking, AWB retrieval/printing, or event delivery. A production booking must use a genuine order and an authorized operator.
