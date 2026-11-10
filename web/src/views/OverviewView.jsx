import React, { useMemo, useState } from 'react';
import { displayStatus, isClosed } from '../utils/orders.js';
import '../css/Dashboard.css';

const LOW_STOCK = 10;        // a product at or below this many units counts as low stock
const EXPIRY_WARN_DAYS = 90; // batches expiring within this many days

const PERIODS = [
  { key: 'this-month', label: 'This month' },
  { key: 'last-month', label: 'Previous month' },
  { key: 'this-year', label: 'This year' },
  { key: 'last-year', label: 'Previous year' },
  { key: 'all', label: 'All time' }
];

const pad = n => String(n).padStart(2, '0');
const ymd = d => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

// Returns [from, to] as YYYY-MM-DD strings (inclusive), or null for all time.
const periodRange = key => {
  const now = new Date();
  const y = now.getFullYear(), m = now.getMonth();
  switch (key) {
    case 'this-month': return [ymd(new Date(y, m, 1)), ymd(new Date(y, m + 1, 0))];
    case 'last-month': return [ymd(new Date(y, m - 1, 1)), ymd(new Date(y, m, 0))];
    case 'this-year': return [`${y}-01-01`, `${y}-12-31`];
    case 'last-year': return [`${y - 1}-01-01`, `${y - 1}-12-31`];
    default: return null;
  }
};

const inPeriod = (date, key) => {
  const range = periodRange(key);
  return !range || (date >= range[0] && date <= range[1]);
};

// Synced orders store items like "NEXLIFE (NEXLIFE001) × 2, NEXCELL (HF001-I) × 1".
const parseItems = summary =>
  String(summary || '').split(',').map(part => {
    const m = part.trim().match(/^(.*?)\s*[×x]\s*(\d+)$/i);
    if (!m) return null;
    const label = m[1].trim();
    const code = label.match(/\(([^)]+)\)\s*$/);
    return { name: label.replace(/\s*\([^)]+\)\s*$/, ''), code: code ? code[1] : '', qty: Number(m[2]) };
  }).filter(Boolean);

const units = o => parseItems(o.items).reduce((sum, it) => sum + it.qty, 0) || 1;
const fmt = n => Number(n || 0).toLocaleString('en-MY');

function PeriodSelect({ value, onChange }) {
  return (
    <select className="db-period" value={value} onChange={e => onChange(e.target.value)} aria-label="Period">
      {PERIODS.map(p => <option key={p.key} value={p.key}>{p.label}</option>)}
    </select>
  );
}

function ActivityTile({ value, unit, label, tone, icon, onClick }) {
  return (
    <button type="button" className="db-tile" onClick={onClick}>
      <strong className={`db-tone-${tone}`}>{fmt(value)}</strong>
      <small>{unit}</small>
      <span className="db-tile-label"><i className={`db-dot db-tone-bg-${tone}`}>{icon}</i>{label}</span>
    </button>
  );
}

function Donut({ percent }) {
  const r = 38, c = 2 * Math.PI * r;
  return (
    <svg className="db-donut" viewBox="0 0 100 100" role="img" aria-label={`${percent}% active`}>
      <circle cx="50" cy="50" r={r} className="db-donut-track" />
      <circle
        cx="50" cy="50" r={r}
        className="db-donut-value"
        strokeDasharray={`${(percent / 100) * c} ${c}`}
        transform="rotate(-90 50 50)"
      />
      <text x="50" y="50" dominantBaseline="central" textAnchor="middle">{percent}%</text>
    </svg>
  );
}

export function OverviewView({ orders, products, lots = [], movements, month, setView }) {
  const [topPeriod, setTopPeriod] = useState('this-month');
  const [salesPeriod, setSalesPeriod] = useState('this-month');

  // Sales activity: order counts by the same status shown in the Orders page's Status column.
  const activity = useMemo(() => {
    const count = status => orders.filter(o => displayStatus(o) === status).length;
    return {
      pending: count('Pending'),
      processing: count('Processing'),
      complete: count('Complete'),
      cancelled: count('Cancelled'),
      committed: orders.filter(o => !isClosed(o)).reduce((s, o) => s + units(o), 0)
    };
  }, [orders]);

  // Product details.
  const product = useMemo(() => {
    const today = new Date();
    const warnBy = ymd(new Date(today.getFullYear(), today.getMonth(), today.getDate() + EXPIRY_WARN_DAYS));
    const liveLots = lots.filter(l => Number(l.quantity) > 0);
    const active = products.filter(p => Number(p.quantity) > 0).length;
    return {
      inHand: products.reduce((s, p) => s + (Number(p.quantity) || 0), 0),
      low: products.filter(p => Number(p.quantity) <= LOW_STOCK).length,
      batches: liveLots.length,
      all: products.length,
      expiring: liveLots.filter(l => l.expiry_date && String(l.expiry_date).slice(0, 10) <= warnBy).length,
      activePct: products.length ? Math.round((active / products.length) * 100) : 0
    };
  }, [products, lots]);

  // Top selling items in the chosen period (cancelled orders excluded).
  const topItems = useMemo(() => {
    const totals = new Map();
    orders
      .filter(o => displayStatus(o) !== 'Cancelled' && inPeriod(o.date, topPeriod))
      .forEach(o => parseItems(o.items).forEach(it => {
        const key = it.code || it.name;
        const row = totals.get(key) || { name: it.name, code: it.code, qty: 0 };
        row.qty += it.qty;
        totals.set(key, row);
      }));
    return [...totals.values()].sort((a, b) => b.qty - a.qty).slice(0, 4);
  }, [orders, topPeriod]);

  // Stock received this month (from the loaded movement report).
  const received = useMemo(() =>
    movements.filter(m => m.type === 'add').reduce((s, m) => s + (Number(m.quantity) || 0), 0),
  [movements]);

  // Sales order table: channel × status.
  const STATUSES = ['Pending', 'Processing', 'Complete', 'Cancelled'];
  const salesRows = useMemo(() => {
    const scoped = orders.filter(o => inPeriod(o.date, salesPeriod));
    return [
      { key: 'delivery', label: 'Delivery' },
      { key: 'self_collection', label: 'Self-collection' }
    ].map(ch => ({
      ...ch,
      counts: STATUSES.map(s => scoped.filter(o => o.method === ch.key && displayStatus(o) === s).length)
    }));
  }, [orders, salesPeriod]);

  const monthLabel = month
    ? new Date(`${month}-01T00:00:00`).toLocaleDateString('en-MY', { month: 'long', year: 'numeric' })
    : 'This month';

  return (
    <div className="db">
      {/* Row 1 */}
      <section className="db-card db-span-2">
        <h2 className="db-title">Sales Activity</h2>
        <div className="db-tiles">
          <ActivityTile value={activity.pending} unit="Orders" label="PENDING" tone="orange" icon="…" onClick={() => setView('orders')} />
          <ActivityTile value={activity.processing} unit="Orders" label="PROCESSING" tone="blue" icon="➜" onClick={() => setView('orders')} />
          <ActivityTile value={activity.complete} unit="Orders" label="COMPLETE" tone="green" icon="✓" onClick={() => setView('orders')} />
          <ActivityTile value={activity.cancelled} unit="Orders" label="CANCELLED" tone="red" icon="✕" onClick={() => setView('orders')} />
        </div>
      </section>

      <section className="db-card">
        <h2 className="db-title">Inventory Summary</h2>
        <div className="db-summary-row">
          <span>QUANTITY IN HAND</span>
          <strong>{fmt(product.inHand)}</strong>
        </div>
        <div className="db-summary-row" title="Units in orders that are still Pending or Processing">
          <span>RESERVED FOR OPEN ORDERS</span>
          <strong>{fmt(activity.committed)}</strong>
        </div>
        {/* Available = in hand minus reserved. Negative means more is ordered than is on the shelf. */}
        <div className={`db-summary-row${product.inHand - activity.committed < 0 ? ' db-short' : ''}`}>
          <span>{product.inHand - activity.committed < 0 ? 'SHORT FOR OPEN ORDERS' : 'AVAILABLE'}</span>
          <strong>{fmt(Math.abs(product.inHand - activity.committed))}</strong>
        </div>
      </section>

      {/* Row 2 */}
      <section className="db-card db-span-2 db-product">
        <div className="db-product-list">
          <h3 className="db-label">PRODUCT DETAILS</h3>
          <button type="button" className="db-line db-danger" onClick={() => setView('inventory')}>
            <span>Low Stock Items</span><b>{fmt(product.low)}</b>
          </button>
          <button type="button" className="db-line" onClick={() => setView('inventory')}>
            <span>All Batches</span><b>{fmt(product.batches)}</b>
          </button>
          <button type="button" className="db-line" onClick={() => setView('inventory')}>
            <span>All Items</span><b>{fmt(product.all)}</b>
          </button>
          <button
            type="button"
            className="db-line"
            title={`Batches in stock that expire within ${EXPIRY_WARN_DAYS} days`}
            onClick={() => setView('inventory')}
          >
            <span>Expiring Soon <i className="db-info">i</i></span><b>{fmt(product.expiring)}</b>
          </button>
        </div>
        <div className="db-product-chart">
          <h3 className="db-chart-title">Active Items</h3>
          <Donut percent={product.activePct} />
          <small>Items with stock on hand</small>
        </div>
      </section>

      <section className="db-card">
        <div className="db-card-head">
          <h3 className="db-label">TOP SELLING ITEMS</h3>
          <PeriodSelect value={topPeriod} onChange={setTopPeriod} />
        </div>
        {topItems.length ? (
          <div className="db-top">
            {topItems.map(it => (
              <div className="db-top-item" key={it.code || it.name}>
                <span className="db-top-mark">{it.name.slice(0, 2).toUpperCase()}</span>
                <span className="db-top-name" title={it.code ? `${it.name} (${it.code})` : it.name}>{it.name}</span>
                <strong>{fmt(it.qty)} <small>units</small></strong>
              </div>
            ))}
          </div>
        ) : (
          <p className="db-empty">No sales in this period.</p>
        )}
      </section>

      {/* Row 3 */}
      <section className="db-card">
        <div className="db-card-head">
          <h3 className="db-label">STOCK RECEIVED</h3>
          <span className="db-muted">{monthLabel}</span>
        </div>
        <div className="db-big">
          <span>Quantity Received</span>
          <strong>{fmt(received)}</strong>
        </div>
      </section>

      <section className="db-card db-span-2">
        <div className="db-card-head">
          <h3 className="db-label">SALES ORDER</h3>
          <PeriodSelect value={salesPeriod} onChange={setSalesPeriod} />
        </div>
        <div className="db-table-wrap">
          <table className="db-table">
            <thead>
              <tr><th>Channel</th>{STATUSES.map(s => <th key={s}>{s}</th>)}</tr>
            </thead>
            <tbody>
              {salesRows.map(row => (
                <tr key={row.key}>
                  <td>{row.label}</td>
                  {row.counts.map((n, i) => <td key={STATUSES[i]}>{fmt(n)}</td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
