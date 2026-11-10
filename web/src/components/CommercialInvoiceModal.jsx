import React, { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { buildInvoice, formatMoney } from '../utils/invoice.js';
import '../css/CommercialInvoiceModal.css';

function Party({ title, p }) {
  return (
    <div className="ci-party">
      <h2>{title}</h2>
      <b>{p.name || '—'}</b>
      {p.lines.map(line => <div key={line}>{line}</div>)}
      <div>{[p.cityLine, p.state].filter(Boolean).join(', ')}</div>
      <div>{p.country}</div>
      {p.phone && <div>Tel: {p.phone}</div>}
      {p.email && <div>Email: {p.email}</div>}
    </div>
  );
}

const hsKey = id => `wms-ci-hs-${id}`;
const loadHs = id => {
  try { return JSON.parse(localStorage.getItem(hsKey(id)) || '[]'); } catch { return []; }
};
const saveHs = (id, codes) => {
  try { localStorage.setItem(hsKey(id), JSON.stringify(codes)); } catch {}
};

// Shows a print preview of the commercial invoice. "Print" uses the browser print dialog,
// and the print stylesheet hides everything except the invoice paper.
export default function CommercialInvoiceModal({ order, draft, loading, error, onClose }) {
  useEffect(() => {
    document.body.classList.add('ci-printing');
    return () => document.body.classList.remove('ci-printing');
  }, []);

  useEffect(() => {
    const onKey = e => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const base = draft ? buildInvoice(order, draft) : null;

  // HS codes are typed here (per item) and remembered in this browser for the order.
  const [hs, setHs] = useState(() => loadHs(order.id));
  const setCode = (i, v) => {
    const next = [...hs];
    next[i] = v.replace(/[^0-9.\s-]/g, '').slice(0, 16);
    setHs(next);
    saveHs(order.id, next);
  };
  const inv = base && {
    ...base,
    items: base.items.map((it, i) => ({ ...it, hsCode: it.hsCode || hs[i] || '', fromDraft: !!it.hsCode }))
  };
  const missingHs = inv ? inv.items.some(it => !it.hsCode.trim()) : false;

  return createPortal(
    <div className="ci-overlay" role="dialog" aria-modal="true" aria-label="Commercial invoice">
      <div className="ci-toolbar">
        <strong>Commercial invoice · Order {order.id}</strong>
        <div className="ci-actions">
          <button type="button" className="button" onClick={onClose}>Close</button>
          <button type="button" className="button primary" disabled={!inv} onClick={() => window.print()}>Print</button>
        </div>
      </div>

      {loading && <div className="ci-state">Loading invoice details…</div>}
      {error && <div className="ci-state ci-error" role="alert">{error}</div>}
      {inv && missingHs && (
        <div className="ci-advisory" role="status">
          Enter an HS code for every item in the table below before printing. Customs use it to classify the goods.
        </div>
      )}
      {inv && inv.advisory && <div className="ci-advisory">{inv.advisory}</div>}

      {inv && (
        <div className="ci-paper">
          <div className="ci-head">
            <div>
              <h1>COMMERCIAL INVOICE</h1>
              <p className="ci-sub">Attach this invoice to the parcel together with the air waybill.</p>
            </div>
            <dl className="ci-meta">
              <div><dt>Invoice no.</dt><dd>{inv.number}</dd></div>
              <div><dt>Invoice date</dt><dd>{inv.date}</dd></div>
              <div><dt>Order reference</dt><dd>{inv.orderRef}</dd></div>
              {inv.tracking && <div><dt>Tracking no.</dt><dd>{inv.tracking}</dd></div>}
              {inv.courier && <div><dt>Courier</dt><dd>{inv.courier}</dd></div>}
            </dl>
          </div>

          <div className="ci-parties">
            <Party title="Shipper (sender)" p={inv.sender} />
            <Party title="Consignee (receiver)" p={inv.receiver} />
          </div>

          <div className="ci-route">
            <div><span>Shipped from</span><b>{[inv.sender.city || inv.sender.cityLine, inv.sender.state].filter(Boolean).join(', ') || '—'}</b></div>
            <div><span>Shipped to</span><b>{[inv.receiver.city || inv.receiver.cityLine, inv.receiver.state].filter(Boolean).join(', ') || '—'}</b></div>
            <div><span>Total weight</span><b>{inv.weight ? `${inv.weight} kg` : '—'}</b></div>
            <div><span>Currency</span><b>{inv.currency}</b></div>
          </div>

          <table className="ci-items">
            <thead>
              <tr>
                <th style={{ width: 36 }}>No.</th>
                <th>Description of goods</th>
                <th style={{ width: 130 }}>HS code</th>
                <th className="num" style={{ width: 70 }}>Qty</th>
                <th className="num" style={{ width: 130 }}>Value</th>
              </tr>
            </thead>
            <tbody>
              {inv.items.map((it, i) => (
                <tr key={`${i}-${it.description}`}>
                  <td>{i + 1}</td>
                  <td>{it.description || '—'}</td>
                  <td>
                    {it.fromDraft ? it.hsCode : (
                      <input
                        className="ci-hs"
                        value={it.hsCode}
                        placeholder="e.g. 6109.10"
                        inputMode="decimal"
                        aria-label={`HS code for item ${i + 1}`}
                        onChange={e => setCode(i, e.target.value)}
                      />
                    )}
                  </td>
                  <td className="num">{it.quantity || '—'}</td>
                  <td className="num">{formatMoney(inv.currency, it.value)}</td>
                </tr>
              ))}
            </tbody>
          </table>

          <div className="ci-total">
            <span>Total declared value</span>
            <span>{formatMoney(inv.currency, inv.total)}</span>
          </div>

          <p className="ci-declaration">
            I declare that the information on this invoice is true and correct, and that the goods described are as stated.
          </p>

          <div className="ci-sign">
            <div>Name</div>
            <div>Signature / company stamp</div>
            <div>Date</div>
          </div>
        </div>
      )}
    </div>,
    document.body
  );
}