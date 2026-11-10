import React, { useEffect, useState } from 'react';
import { READ_ONLY_PREVIEW, HAS_API, COURIERS } from '../utils/api';
import OrderFilters from '../components/OrderFilters.jsx';
import StatusBadge from '../components/StatusBadge.jsx';
import ActionMenu from '../components/ActionMenu.jsx';
import {
  isBooked,
  isCancelled,
  isClosed,
  displayStatus,
  canCancel,
  canComplete,
  cancelShortReason,
  completeShortReason
} from '../utils/orders.js';
import '../css/OrdersView.css';

const PAGE_SIZE = 10;

// Synced orders have no delivery address until staff add one.
const needsDetails = o => Boolean(o.needs_details) && o.method === 'delivery' && !isBooked(o) && !isClosed(o);

// 1 2 3 … 9 10 style page list
const pageItems = (current, total) => {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1);
  const items = [1];
  const lo = Math.max(2, current - 1);
  const hi = Math.min(total - 1, current + 1);
  if (lo > 2) items.push('…');
  for (let i = lo; i <= hi; i++) items.push(i);
  if (hi < total - 1) items.push('…');
  items.push(total);
  return items;
};

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
  printAWB,
  getAWB,
  awbLoading,
  onViewOrder,
  onCompleteOrder,
  onCancelOrder,
  onPrintInvoice,
  onEditDetails
}) {
  const [page, setPage] = useState(1);

  const totalPages = Math.max(1, Math.ceil(filteredOrders.length / PAGE_SIZE));
  useEffect(() => { setPage(1); }, [filters]);
  useEffect(() => { if (page > totalPages) setPage(totalPages); }, [page, totalPages]);

  const start = (page - 1) * PAGE_SIZE;
  const pageOrders = filteredOrders.slice(start, start + PAGE_SIZE);
  const showFrom = filteredOrders.length ? start + 1 : 0;
  const showTo = Math.min(start + PAGE_SIZE, filteredOrders.length);

  const chosen = orders.filter(o => selected.includes(o.id));
  const bookedSelected = chosen.filter(isBooked).length;
  const bookableSelected = chosen.filter(o => o.method === 'delivery' && !isBooked(o) && !isClosed(o)).length;

  const canWrite = HAS_API && !READ_ONLY_PREVIEW;

  // Live rates are only kept in memory, so after a page refresh the saved courier
  // would have no matching option. Add it so the dropdown still shows the choice.
  const courierOptions = o => {
    const list = courierQuotes[o.id] || COURIERS;
    if (!o.courier_code || list.some(c => c.service_id === o.courier_code)) return list;
    return [{ service_id: o.courier_code, service_name: o.courier || o.courier_code }, ...list];
  };
  const previewReason = 'Not available in preview';

  const actionItems = o => [
    { key: 'view', label: 'View details', onSelect: () => onViewOrder(o.id) },
    {
      key: 'delivery-details',
      label: o.needs_details ? 'Add delivery details' : 'Edit delivery details',
      onSelect: () => onEditDetails(o),
      disabled: !canWrite || o.method !== 'delivery' || isBooked(o) || isClosed(o),
      reason: !canWrite ? previewReason
        : o.method !== 'delivery' ? 'Delivery orders only'
        : isBooked(o) ? 'Shipment already booked'
        : `Order is ${displayStatus(o).toLowerCase()}`
    },
    {
      key: 'invoice',
      label: 'Print commercial invoice',
      onSelect: () => onPrintInvoice(o),
      disabled: !HAS_API || o.method !== 'delivery',
      reason: !HAS_API ? previewReason : 'Delivery orders only'
    },
    {
      key: 'complete',
      label: 'Mark complete',
      onSelect: () => onCompleteOrder(o),
      disabled: !canWrite || !canComplete(o),
      reason: !canWrite ? previewReason : completeShortReason(o)
    },
    {
      key: 'cancel',
      label: 'Cancel order',
      tone: 'danger',
      onSelect: () => onCancelOrder(o),
      disabled: !canWrite || !canCancel(o),
      reason: !canWrite ? previewReason : cancelShortReason(o)
    }
  ];

  return (
    <>
      <div className="metrics">
        <div className="metric"><span>All orders</span><strong>{orders.length}</strong><small>Across all statuses</small></div>
        <div className="metric"><span>Pending</span><strong>{orders.filter(o => displayStatus(o) === 'Pending').length}</strong><small className="green-text">Ready for processing</small></div>
        <div className="metric"><span>Processing</span><strong>{orders.filter(o => displayStatus(o) === 'Processing').length}</strong><small>Awaiting courier handoff</small></div>
        <div className="metric"><span>Self-collection</span><strong>{orders.filter(o => o.method === 'self_collection').length}</strong><small>Customer pickup</small></div>
      </div>

      <div className="panel">
        <div className="panel-head">
          <div>
            <h2>All orders <span className="count">{filteredOrders.length}</span></h2>
            <p>Orders sync from the order system every few minutes. Add the delivery address before getting courier rates.</p>
          </div>
        </div>
        <OrderFilters filters={filters} onApply={setFilters} />

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
            Review selected{bookableSelected ? ` (${bookableSelected})` : ''} →
          </button>
        </div>

        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th className="check-cell">
                  <input
                    type="checkbox"
                    aria-label="Select all orders on this page"
                    checked={pageOrders.length > 0 && pageOrders.every(o => selected.includes(o.id))}
                    onChange={() => toggleAll(pageOrders)}
                  />
                </th>
                <th>ORDER</th>
                <th>DATE</th>
                <th>CUSTOMER</th>
                <th>STATUS</th>
                <th>FULFILMENT</th>
                <th>COURIER</th>
                <th>SHIPMENT STATUS</th>
                <th>ACTION</th>
              </tr>
            </thead>
            <tbody>
              {pageOrders.map(o => (
                <tr key={o.id}>
                  <td className="check-cell">
                    <input type="checkbox" checked={selected.includes(o.id)} onChange={() => toggleSelect(o.id)} />
                  </td>
                  <td><b className="order-id">{o.id}</b></td>
                  <td>{new Date(`${o.date}T00:00:00`).toLocaleDateString('en-SG', { day: '2-digit', month: 'short', year: 'numeric' })}</td>
                  <td>
                    <b>{o.customer}</b>
                    {needsDetails(o) && (
                      <button type="button" className="needs-details" disabled={!canWrite} onClick={() => onEditDetails(o)}>
                        Needs delivery details
                      </button>
                    )}
                  </td>
                  <td>
                    <StatusBadge order={o} />
                    {o.tracking && <small className="subcell">{o.tracking}</small>}
                    {isCancelled(o) && o.cancel_remark && (
                      <small className="subcell" title={o.cancel_remark}>Reason: {o.cancel_remark}</small>
                    )}
                  </td>
                  <td>
                    <select value={o.method} disabled={READ_ONLY_PREVIEW || isBooked(o) || isClosed(o)} onChange={e => setMethod(o, e.target.value)}>
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
                    ) : isClosed(o) ? '—' : (
                      <div className="courier-picker">
                        <button
                          className="quote-button"
                          type="button"
                          disabled={READ_ONLY_PREVIEW || !HAS_API || quoteLoading[o.id] || needsDetails(o)}
                          title={needsDetails(o) ? 'Add the delivery details first' : undefined}
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
                            {courierOptions(o).length ? 'Choose service' : 'Get EasyParcel rates'}
                          </option>
                          {courierOptions(o).map(c => (
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
                    <ActionMenu ariaLabel={`Actions for order ${o.id}`} items={actionItems(o)} />
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
                <b>No orders yet</b>
                <p>Paid orders appear here automatically from the order system. Choose Sync orders to check now.</p>
                <p>For delivery, WMS requests live EasyParcel services, then creates a shipment only after an admin confirms the booking.</p>
              </div>
            )
          )}
        </div>

        <div className="table-foot">
          <span>
            Showing <b>{showFrom}–{showTo}</b> of <b>{filteredOrders.length}</b> orders
            {filteredOrders.length !== orders.length && <> (filtered from <b>{orders.length}</b>)</>}
          </span>
          {totalPages > 1 && (
            <nav className="pagination" aria-label="Pagination">
              <button type="button" aria-label="Previous page" disabled={page === 1} onClick={() => setPage(p => p - 1)}>‹</button>
              {pageItems(page, totalPages).map((item, i) => (
                item === '…'
                  ? <span key={`gap-${i}`} className="pager-gap">…</span>
                  : (
                    <button
                      key={item}
                      type="button"
                      className={item === page ? 'page-current' : ''}
                      aria-current={item === page ? 'page' : undefined}
                      onClick={() => setPage(item)}
                    >
                      {item}
                    </button>
                  )
              ))}
              <button type="button" aria-label="Next page" disabled={page === totalPages} onClick={() => setPage(p => p + 1)}>›</button>
            </nav>
          )}
        </div>
      </div>
    </>
  );
}