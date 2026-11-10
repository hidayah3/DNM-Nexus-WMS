import React, { useEffect, useState } from 'react';
import { API, apiFetch } from '../utils/api';

// Synced orders arrive without a delivery address (the order API does not send one).
// Staff fill in the customer address and parcel size here before getting courier rates.

const fromAddress = (prefix, a = {}) => ({
  [`${prefix}_name`]: a.name || '',
  [`${prefix}_phone_country`]: a.phone_number_country_code || '60',
  [`${prefix}_phone`]: a.phone_number || '',
  [`${prefix}_email`]: a.email || '',
  [`${prefix}_address1`]: a.address_1 || '',
  [`${prefix}_address2`]: a.address_2 || '',
  [`${prefix}_postcode`]: a.postcode || '',
  [`${prefix}_city`]: a.city || '',
  [`${prefix}_state`]: a.subdivision_code || ''
});

const toAddress = (prefix, form) => ({
  name: form[`${prefix}_name`].trim(),
  phone_number_country_code: form[`${prefix}_phone_country`].trim(),
  phone_number: form[`${prefix}_phone`].trim(),
  email: form[`${prefix}_email`].trim(),
  address_1: form[`${prefix}_address1`].trim(),
  address_2: form[`${prefix}_address2`].trim(),
  postcode: form[`${prefix}_postcode`].trim(),
  city: form[`${prefix}_city`].trim(),
  subdivision_code: form[`${prefix}_state`].trim(),
  country_code: 'MY'
});

const numberText = n => (n ? String(n) : '');

export function DeliveryDetailsDialog({ order, onClose, onSaved }) {
  const [draft, setDraft] = useState(null);
  const [form, setForm] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const r = await apiFetch(`${API}/api/orders/${encodeURIComponent(order.id)}/commercial-invoice`);
        const d = await r.json().catch(() => ({}));
        if (!r.ok) throw Error(d.error || 'Could not load the saved delivery details');
        if (!alive) return;
        const s = d.shipment || {};
        setDraft(s);
        setForm({
          ...fromAddress('receiver', { name: order.customer, ...s.receiver }),
          ...fromAddress('sender', s.sender),
          weight: numberText(s.weight),
          length: numberText(s.length),
          width: numberText(s.width),
          height: numberText(s.height),
          parcel_value: numberText(s.parcel_value),
          currency_code: s.currency_code || 'MYR'
        });
      } catch (e) {
        if (alive) setError(e.message);
      }
    })();
    return () => { alive = false; };
  }, [order.id]);

  const field = (key, value) => setForm(old => ({ ...old, [key]: value }));

  const save = async e => {
    e.preventDefault();
    setBusy(true);
    setError('');
    const weight = Number(form.weight), length = Number(form.length), width = Number(form.width), height = Number(form.height);
    const currency = form.currency_code.trim().toUpperCase();
    const value = Number(form.parcel_value);
    // Keep the products from the order API; give each one its share of the parcel weight.
    const lines = draft?.item?.length ? draft.item : [{ content: order.items, quantity: 1, value }];
    const totalQty = lines.reduce((sum, it) => sum + (Number(it.quantity) || 1), 0) || 1;
    const items = lines.map(it => ({
      content: it.content || order.items,
      quantity: Number(it.quantity) || 1,
      value: Number(it.value) || 0,
      weight: Math.max(0.001, Math.round((weight / totalQty) * 1000) / 1000),
      length, width, height,
      currency_code: currency
    }));
    const payload = {
      collection_date: draft?.collection_date || '',
      sender: toAddress('sender', form),
      receiver: toAddress('receiver', form),
      weight, length, width, height,
      parcel_value: value,
      currency_code: currency,
      item: items
    };
    try {
      const r = await apiFetch(`${API}/api/orders/${encodeURIComponent(order.id)}/shipment`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) throw Error(d.error || 'Could not save the delivery details');
      onSaved(order);
    } catch (e2) {
      setError(e2.message);
    } finally {
      setBusy(false);
    }
  };

  const addressFields = (prefix, label) => (
    <fieldset className="order-address">
      <legend>{label}</legend>
      <div className="manual-order-grid">
        <label>Contact name<input required value={form[`${prefix}_name`]} onChange={e => field(`${prefix}_name`, e.target.value)} /></label>
        <label>Phone country code<input required value={form[`${prefix}_phone_country`]} onChange={e => field(`${prefix}_phone_country`, e.target.value)} placeholder="60" /></label>
        <label>Phone number<input required value={form[`${prefix}_phone`]} onChange={e => field(`${prefix}_phone`, e.target.value)} /></label>
        <label>Email<input type="email" value={form[`${prefix}_email`]} onChange={e => field(`${prefix}_email`, e.target.value)} /></label>
        <label className="wide">Address line 1<input required value={form[`${prefix}_address1`]} onChange={e => field(`${prefix}_address1`, e.target.value)} /></label>
        <label className="wide">Address line 2<input value={form[`${prefix}_address2`]} onChange={e => field(`${prefix}_address2`, e.target.value)} /></label>
        <label>Postcode<input required value={form[`${prefix}_postcode`]} onChange={e => field(`${prefix}_postcode`, e.target.value)} /></label>
        <label>City<input required value={form[`${prefix}_city`]} onChange={e => field(`${prefix}_city`, e.target.value)} /></label>
        <label>EasyParcel state code<input required value={form[`${prefix}_state`]} onChange={e => field(`${prefix}_state`, e.target.value)} placeholder="As listed by EasyParcel" /></label>
      </div>
    </fieldset>
  );

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <form className="manual-order-modal" onSubmit={save} onClick={e => e.stopPropagation()}>
        <div className="manual-order-heading">
          <div>
            <h2>Delivery details · {order.id}</h2>
            <p><b>{order.customer}</b> · {order.items}</p>
            <p>The order system doesn't send delivery addresses. Fill them in here before getting courier rates.</p>
          </div>
          <button type="button" className="icon-button" onClick={onClose} aria-label="Close">×</button>
        </div>

        {!form && !error && <p>Loading…</p>}
        {error && <div className="notice" role="alert">{error}</div>}

        {form && (
          <>
            {addressFields('receiver', 'Customer recipient')}

            <fieldset className="order-address">
              <legend>Parcel</legend>
              <div className="manual-order-grid">
                <label>Parcel weight (kg)<input type="number" min="0.01" step="0.001" required value={form.weight} onChange={e => field('weight', e.target.value)} /></label>
                <label>Parcel value<input type="number" min="0.01" step="0.01" required value={form.parcel_value} onChange={e => field('parcel_value', e.target.value)} /></label>
                <label>Currency<input required value={form.currency_code} onChange={e => field('currency_code', e.target.value.toUpperCase())} /></label>
                <label>Length (cm)<input type="number" min="0.1" step="0.1" required value={form.length} onChange={e => field('length', e.target.value)} /></label>
                <label>Width (cm)<input type="number" min="0.1" step="0.1" required value={form.width} onChange={e => field('width', e.target.value)} /></label>
                <label>Height (cm)<input type="number" min="0.1" step="0.1" required value={form.height} onChange={e => field('height', e.target.value)} /></label>
              </div>
            </fieldset>

            {addressFields('sender', 'Warehouse sender (filled from the last order)')}

            <div className="manual-order-actions">
              <span>After saving: Get live rates → choose courier → review delivery list → confirm booking.</span>
              <div>
                <button type="button" className="button" onClick={onClose}>Cancel</button>
                <button className="button primary" disabled={busy}>{busy ? 'Saving…' : 'Save details'}</button>
              </div>
            </div>
          </>
        )}
      </form>
    </div>
  );
}
