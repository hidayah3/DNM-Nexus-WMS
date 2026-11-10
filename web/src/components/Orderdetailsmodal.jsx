import React, { useEffect } from 'react';
import { HAS_API, READ_ONLY_PREVIEW } from '../utils/api';
import StatusBadge from './StatusBadge.jsx';
import {
  isBooked,
  isCancelled,
  isComplete,
  canCancel,
  canComplete,
  cancelBlockedReason,
  completeBlockedReason
} from '../utils/orders.js';
import '../css/OrderModals.css';

const fmtDateTime = value => (value ? new Date(value).toLocaleString() : '—');

function Field({ label, wide, children }) {
  return (
    <div className={`od-field ${wide ? 'wide' : ''}`}>
      <span>{label}</span>
      <div>{children || '—'}</div>
    </div>
  );
}

export default function OrderDetailsModal({ order, onClose, onComplete, onCancel }) {
  useEffect(() => {
    const onKey = e => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const o = order;
  const booked = isBooked(o);
  const writable = HAS_API && !READ_ONLY_PREVIEW;
  const hints = [...new Set([
    !canCancel(o) ? cancelBlockedReason(o) : '',
    !canComplete(o) ? completeBlockedReason(o) : ''
  ].filter(Boolean))];

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="od-modal" role="dialog" aria-modal="true" aria-label={`Order ${o.id} details`} onClick={e => e.stopPropagation()}>
        <div className="od-head">
          <div>
            <h2>Order {o.id} <StatusBadge order={o} /></h2>
            <p>Order details</p>
          </div>
          <button type="button" className="icon-button" onClick={onClose} aria-label="Close">×</button>
        </div>

        <div className="od-grid">
          <Field label="Order date">{new Date(`${o.date}T00:00:00`).toLocaleDateString('en-SG', { day: '2-digit', month: 'short', year: 'numeric' })}</Field>
          <Field label="Customer">{o.customer}</Field>
          <Field label="Items" wide>{o.items}</Field>
          <Field label="Fulfilment">{o.method === 'self_collection' ? 'Self-collection' : 'Delivery'}</Field>
        </div>

        {o.method === 'delivery' && (
          <div className="od-section">
            <h3>Shipment</h3>
            {booked ? (
              <div className="od-grid">
                <Field label="Courier">{o.courier || o.courier_code}</Field>
                <Field label="Shipment number">{o.shipment_number}</Field>
                <Field label="Tracking number">{o.tracking}</Field>
                <Field label="AWB">
                  {o.awb_url ? <a href={o.awb_url} target="_blank" rel="noreferrer">Open AWB ↗</a> : 'Not available yet'}
                </Field>
                <Field label="EasyParcel status">{o.provider_status || 'Booked'}</Field>
                <Field label="Tracking status">{o.tracking_status || 'Waiting for EasyParcel tracking update'}</Field>
              </div>
            ) : (
              <div className="od-grid">
                <Field label="Courier">{o.courier || (o.courier_code ? o.courier_code : 'Not chosen yet')}</Field>
                <Field label="Shipment">Not booked yet</Field>
              </div>
            )}
          </div>
        )}

        {isCancelled(o) && (
          <div className="od-alert">
            <h3>Order cancelled</h3>
            <div className="od-grid">
              <Field label="Reason" wide>{o.cancel_remark}</Field>
              <Field label="Cancelled by">{o.cancelled_name}</Field>
              <Field label="Cancelled on">{fmtDateTime(o.cancelled_at)}</Field>
            </div>
          </div>
        )}

        {isComplete(o) && (
          <div className="od-section">
            <h3>Completed</h3>
            <div className="od-grid">
              <Field label="Completed on">{fmtDateTime(o.completed_at)}</Field>
            </div>
          </div>
        )}

        <div className="od-actions">
          <div className="od-hints">{hints.map(h => <small key={h}>{h}</small>)}</div>
          <div className="od-buttons">
            <button type="button" className="button" onClick={onClose}>Close</button>
            <button type="button" className="button" disabled={!writable || !canComplete(o)} onClick={() => onComplete(o)}>Mark complete</button>
            <button type="button" className="button danger-solid" disabled={!writable || !canCancel(o)} onClick={() => onCancel(o)}>Cancel order</button>
          </div>
        </div>
      </div>
    </div>
  );
}