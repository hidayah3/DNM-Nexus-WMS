# EasyParcel connection readiness — 19 October 2026

## Current status

The live WMS is deployed at https://wms.dnmnexus.com. The Go health endpoint, PostgreSQL and Caddy were reported healthy after deployment. The WMS can be used independently for real warehouse stock and for staff-entered real orders; it is not a demo-only screen.

EasyParcel production OAuth is configured for the DNM NEXUS INTERNATIONAL Malaysia account, and the OAuth callback plus four shipment webhook topics are registered. The WMS has the live-rate, manual **Confirm & send to EasyParcel**, shipment update, AWB retrieval/refresh, **Print AWB**, and manual **Send updates to SMS** flows. Production credentials and webhook secrets stay on the VPS and are excluded from this source archive.

Configuration does not prove a complete real shipment round trip. No real order has yet been used to request a quote, create a paid shipment, receive/print an AWB, or confirm webhook delivery. No shipment was created for this handover. First production booking must be explicitly confirmed by an authorized warehouse user using a genuine order; EasyParcel may charge the account.

## Operational flow

1. SMS can send an actual order snapshot to WMS; until its developer implements that feed, staff can enter a real order in WMS with actual address and parcel details.
2. A warehouse employee requests current rates, chooses a service, and explicitly confirms the shipment in WMS.
3. WMS saves EasyParcel's response. Registered EasyParcel webhooks and WMS refresh requests update available shipment, tracking, and AWB information.
4. Staff print the AWB from WMS and manually send tracking/status/AWB fields to SMS using **Send updates to SMS**, once SMS implements its receiver.

## SMS integration boundary

The EasyParcel connection belongs to WMS. SMS does not need EasyParcel credentials. SMS-side source and callback URLs were not supplied, so the SMS developer must implement order push, stock snapshot receiver, and shipment update receiver as specified in `SMS-INTEGRATION-CONTRACT.md`. Live shipment-to-SMS write-back is possible after the callback URL/token are configured on the VPS.

## Verification still needed

With one actual authorized order and the SMS callback available, verify live rates, service selection, explicit booking, persisted tracking/AWB details, a real webhook event, printing, and manual SMS write-back. Do not use a made-up order or customer for a chargeable production booking.
