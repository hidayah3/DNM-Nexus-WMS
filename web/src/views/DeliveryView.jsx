import React from 'react';
import { READ_ONLY_PREVIEW } from '../utils/api';

// This page only books EasyParcel shipments. Booked orders, their AWB and their
// shipment status are managed on the Orders page.
export function DeliveryView({ deliveryOrders, readyOrders, pushShipments, goToOrders }) {
  return (
    <div className="panel">
      <div className="panel-head">
        <div>
          <h2>Orders to book <span className="count">{deliveryOrders.length}</span></h2>
          <p>Selected delivery orders that have not been booked with EasyParcel yet.</p>
        </div>
      </div>

      {deliveryOrders.length === 0 ? (
        <div className="empty">
          <b>No orders to book</b>
          <p>Tick delivery orders that are not booked yet on the Orders page, then choose Review selected.</p>
          <p>Booked orders do not appear here. Print their AWB from the Orders page.</p>
          {goToOrders && (
            <button className="button primary small" type="button" onClick={goToOrders}>Go to Orders</button>
          )}
        </div>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>ORDER</th>
                <th>DATE</th>
                <th>CUSTOMER &amp; ITEMS</th>
                <th>COURIER</th>
                <th>READY?</th>
              </tr>
            </thead>
            <tbody>
              {deliveryOrders.map(o => (
                <tr key={o.id}>
                  <td>{o.id}</td>
                  <td>{o.date}</td>
                  <td>{o.customer}<small className="subcell">{o.items}</small></td>
                  <td>{o.courier || 'Choose courier in Orders'}</td>
                  <td>{o.courier_code ? 'Ready to book' : 'Courier needed'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="delivery-actions">
        <div>
          <b>{readyOrders.length} of {deliveryOrders.length} ready to send</b>
          <small>
            {READ_ONLY_PREVIEW
              ? 'Preview mode: no shipment will be created until an operational environment is connected.'
              : 'Confirm & send creates the EasyParcel shipment and may charge your wallet. You will return to Orders, where you can print the AWB.'}
          </small>
        </div>
        <div className="action-buttons">
          <button className="button primary" onClick={pushShipments}>
            Confirm &amp; send to EasyParcel
          </button>
        </div>
      </div>
    </div>
  );
}