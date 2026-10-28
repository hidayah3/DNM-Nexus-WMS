import React, { useMemo } from 'react';
import { EMPTY_FILTERS, displayStatus } from '../utils/orders.js';
import '../css/OrderFilters.css';

const METHOD_LABEL = { delivery: 'Delivery', self_collection: 'Self-collection' };

export default function OrderFilters({ orders, filters, onApply }) {
  const statuses = useMemo(
    () => [...new Set(['Ready to fulfil', 'Ready to deliver', ...orders.map(displayStatus)])].filter(Boolean),
    [orders]
  );

  // Every change is applied immediately, so there is no Search button.
  const set = (key, value) => onApply({ ...filters, [key]: value });
  const reset = () => onApply(EMPTY_FILTERS);

  const hasFilters = Object.values(filters).some(Boolean);
  const invalidRange = Boolean(filters.from && filters.to && filters.from > filters.to);

  const chips = [
    filters.status && { key: 'status', label: `Status: ${filters.status}` },
    filters.method && { key: 'method', label: `Fulfilment: ${METHOD_LABEL[filters.method]}` },
    filters.from && { key: 'from', label: `From: ${filters.from}` },
    filters.to && { key: 'to', label: `To: ${filters.to}` }
  ].filter(Boolean);

  return (
    <div className="order-filters">
      <div className="of-fields">
        <label className="of-field">Status
          <select value={filters.status} onChange={e => set('status', e.target.value)}>
            <option value="">All statuses</option>
            {statuses.map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </label>
        <label className="of-field">Fulfilment
          <select value={filters.method} onChange={e => set('method', e.target.value)}>
            <option value="">All methods</option>
            <option value="delivery">Delivery</option>
            <option value="self_collection">Self-collection</option>
          </select>
        </label>
        <label className="of-field">From
          <input type="date" value={filters.from} max={filters.to || undefined} onChange={e => set('from', e.target.value)} />
        </label>
        <label className="of-field">To
          <input type="date" value={filters.to} min={filters.from || undefined} onChange={e => set('to', e.target.value)} />
        </label>
        <div className="of-actions">
          <button type="button" className="button small" disabled={!hasFilters} onClick={reset}>Reset</button>
        </div>
      </div>

      {invalidRange && <div className="of-error" role="alert">The “From” date is after the “To” date, so no orders can match.</div>}

      {chips.length > 0 && (
        <div className="of-chips" aria-label="Active filters">
          <span className="of-chips-label">Active filters:</span>
          {chips.map(c => (
            <span key={c.key} className="of-chip">
              {c.label}
              <button type="button" aria-label={`Remove filter ${c.label}`} onClick={() => set(c.key, '')}>×</button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}