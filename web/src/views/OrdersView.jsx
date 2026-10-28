import React from 'react';
import { READ_ONLY_PREVIEW, HAS_API, COURIERS } from '../utils/api';
import OrderFilters from '../components/OrderFilters.jsx';
import { isBooked, displayStatus } from '../utils/orders.js';
import '../css/OrdersView.css';

export function OrdersView({
  orders,
  filteredOrders,
  selected,
  toggleSelect,
  toggleAll,
  filters,
  setFilters,
  setMethod,
  quoteCouriers,
  chooseCourier,
  courierQuotes,
  quoteLoading,
  goToDelivery,
  deleteOrder,
  printAWB,
  getAWB,
  awbLoading
}) {
  const chosen = orders.filter(o => selected.includes(o.id));
  const bookedSelected = chosen.filter(isBooked).length;
  const bookableSelected = chosen.filter(o => o.method === 'delivery' && !isBooked(o)).length;

  return (
    <>
      <div className="metrics">
        <div className="metric"><span>All orders</span><strong>{orders.length}</strong><small>Across all statuses</small></div>
        <div className="metric"><span>Ready to fulfil</span><strong>{orders.filter(o => !isBooked(o) && o.status === 'Ready to fulfil').length}</strong><small className="green-text">Ready for processing</small></div>
        <div className="metric"><span>To be shipped</span><strong>{orders.filter(isBooked).length}</strong><small>Awaiting courier handoff</small></div>
        <div className="metric"><span>Self-collection</span><strong>{orders.filter(o => o.method === 'self_collection').length}</strong><small>Customer pickup</small></div>
      </div>

      <div className="panel">
        <div className="panel-head">
          <div>
            <h2>All orders <span className="count">{filteredOrders.length}</span></h2>
            <p>Manage SMS-fed orders or enter a real order directly into WMS.</p>
          </div>
        </div>
        <OrderFilters orders={orders} filters={filters} onApply={setFilters} />

        <div className="selection-bar">
          <span><b>{selected.length}</b> selected</span>
          <span className="selection-spacer" />
          <button
            className="button small"
            type="button"
            disabled={!bookedSelected}
            title="Open the airway bill for the selected booked orders"
            onClick={printAWB}
          >
            Print AWB{bookedSelected ? ` (${bookedSelected})` : ''}
          </button>
          <button
            className="button secondary small"
            type="button"
            disabled={!bookableSelected}
            title="Book EasyParcel shipments for the selected orders that are not booked yet"
            onClick={goToDelivery}
          >
            Generate AWB{bookableSelected ? ` (${bookableSelected})` : ''} →
          </button>
        </div>

        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th className="check-cell">
                  <input
                    type="checkbox"
                    aria-label="Select all visible orders"
                    checked={filteredOrders.length > 0 && filteredOrders.every(o => selected.includes(o.id))}
                    onChange={toggleAll}
                  />
                </th>
                <th>ORDER</th>
                <th>DATE</th>
                <th>CUSTOMER &amp; ITEMS</th>
                <th>STATUS</th>
                <th>FULFILMENT</th>
                <th>COURIER</th>
                <th>SHIPMENT STATUS</th>
                <th>ACTION</th>
              </tr>
            </thead>
            <tbody>
              {filteredOrders.map(o => (
                <tr key={o.id}>
                  <td className="check-cell">
                    <input type="checkbox" checked={selected.includes(o.id)} onChange={() => toggleSelect(o.id)} />
                  </td>
                  <td><b className="order-id">{o.id}</b></td>
                  <td>{new Date(`${o.date}T00:00:00`).toLocaleDateString('en-SG', { day: '2-digit', month: 'short', year: 'numeric' })}</td>
                  <td><b>{o.customer}</b><small className="subcell">{o.items}</small></td>
                  <td>
                    <span className={`status ${isBooked(o) ? 'shipped' : ''}`}><i />{displayStatus(o)}</span>
                    {o.tracking && <small className="subcell">{o.tracking}</small>}
                  </td>
                  <td>
                    <select value={o.method} disabled={READ_ONLY_PREVIEW || isBooked(o)} onChange={e => setMethod(o, e.target.value)}>
                      <option value="delivery">Delivery</option>
                      <option value="self_collection">Self-collection</option>
                    </select>
                  </td>
                  <td>
                    {o.method !== 'delivery' ? '—' : isBooked(o) ? (
                      <div className="courier-booked">
                        <b>{o.courier || o.courier_code || 'Courier booked'}</b>
                        {o.awb_url ? (
                          <a className="awb-link" href={o.awb_url} target="_blank" rel="noreferrer">Open AWB ↗</a>
                        ) : (
                          <button
                            className="awb-button"
                            type="button"
                            disabled={READ_ONLY_PREVIEW || awbLoading[o.id]}
                            title="AWB is not saved yet. Click to ask EasyParcel for it."
                            onClick={() => getAWB(o)}
                          >
                            {awbLoading[o.id] ? 'Checking…' : 'Get AWB'}
                          </button>
                        )}
                      </div>
                    ) : (
                      <div className="courier-picker">
                        <button
                          className="quote-button"
                          type="button"
                          disabled={READ_ONLY_PREVIEW || !HAS_API || quoteLoading[o.id]}
                          onClick={() => quoteCouriers(o)}
                        >
                          {quoteLoading[o.id] ? 'Loading…' : 'Get live rates'}
                        </button>
                        <select
                          value={o.courier_code || ''}
                          disabled={READ_ONLY_PREVIEW}
                          onChange={e => chooseCourier(o, e.target.value)}
                        >
                          <option value="">
                            {(courierQuotes[o.id] || COURIERS).length ? 'Choose service' : 'Get EasyParcel rates'}
                          </option>
                          {(courierQuotes[o.id] || COURIERS).map(c => (
                            <option key={c.service_id} value={c.service_id}>
                              {c.service_name || c.courier_name || c.name || c.service_id}
                              {c.total_amount !== undefined ? ` · ${c.currency || 'RM'} ${Number(c.total_amount).toFixed(2)}` : ''}
                            </option>
                          ))}
                        </select>
                      </div>
                    )}
                  </td>
                  <td>
                    {isBooked(o) ? (
                      <>
                        {o.provider_status || 'Booked'}
                        <small className="subcell">{o.tracking_status || 'Waiting for EasyParcel tracking update'}</small>
                      </>
                    ) : '—'}
                  </td>
                  <td>
                    <button
                      className="icon-delete"
                      type="button"
                      aria-label={`Delete order ${o.id}`}
                      disabled={READ_ONLY_PREVIEW || !HAS_API || Boolean(o.shipment_number || o.tracking)}
                      title={o.shipment_number || o.tracking ? 'Shipment already created; cannot delete' : 'Delete this order'}
                      onClick={() => deleteOrder(o)}
                    >
                      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                        strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                        <polyline points="3 6 5 6 21 6" />
                        <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                        <path d="M10 11v6" />
                        <path d="M14 11v6" />
                        <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
                      </svg>
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>

          {!filteredOrders.length && (
            orders.length > 0 ? (
              <div className="empty">
                <b>No orders match these filters</b>
                <p>Try a different status, method or date range, or click Reset.</p>
              </div>
            ) : (
              <div className="empty">
                <b>No real orders have been entered yet</b>
                <p>Choose Enter order to record an actual order here, or connect the SMS order feed to populate this list automatically.</p>
                <p>For delivery, WMS requests live EasyParcel services, then creates a shipment only after an admin confirms the booking.</p>
              </div>
            )
          )}
        </div>
        <div className="table-foot">Showing <b>{filteredOrders.length}</b> of <b>{orders.length}</b> orders</div>
      </div>
    </>
  );
}