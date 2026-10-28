import React, { useState } from 'react';
import { blankOrder } from '../utils/api';

export function ManualOrderDialog({ onClose, onCreate, busy }) {
  const [form, setForm] = useState(blankOrder);

  const field = (key, value) => setForm(old => ({ ...old, [key]: value }));

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
        <label>EasyParcel state / subdivision code<input required value={form[`${prefix}_state`]} onChange={e => field(`${prefix}_state`, e.target.value)} placeholder="As listed by EasyParcel" /></label>
        <label>Country code<input required value="MY" disabled /></label>
      </div>
    </fieldset>
  );

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <form className="manual-order-modal" onSubmit={e => { e.preventDefault(); onCreate(form); }} onClick={e => e.stopPropagation()}>
        <div className="manual-order-heading">
          <div>
            <h2>Enter a real order</h2>
            <p>Use the actual SMS order reference and customer/parcel details. Saving here creates an order in WMS; it does not book postage.</p>
          </div>
          <button type="button" className="icon-button" onClick={onClose} aria-label="Close">×</button>
        </div>
        <div className="manual-order-grid">
          <label>Order ID / SMS reference<input required value={form.id} onChange={e => field('id', e.target.value)} placeholder="Actual order reference" /></label>
          <label>Order date<input type="date" required value={form.date} onChange={e => field('date', e.target.value)} /></label>
          <label className="wide">Order items<input required value={form.items} onChange={e => field('items', e.target.value)} placeholder="Product name / SKU and quantity" /></label>
          <label className="wide">Parcel contents<input required value={form.contents} onChange={e => field('contents', e.target.value)} placeholder="Contents for the courier declaration" /></label>
          <label>Item quantity<input type="number" min="1" required value={form.quantity} onChange={e => field('quantity', e.target.value)} /></label>
          <label>Parcel value<input type="number" min="0.01" step="0.01" required value={form.parcel_value} onChange={e => field('parcel_value', e.target.value)} /></label>
          <label>Currency<input required value={form.currency_code} onChange={e => field('currency_code', e.target.value.toUpperCase())} /></label>
          <label>Parcel weight (kg)<input type="number" min="0.01" step="0.001" required value={form.weight} onChange={e => field('weight', e.target.value)} /></label>
          <label>Length (cm)<input type="number" min="0.1" step="0.1" required value={form.length} onChange={e => field('length', e.target.value)} /></label>
          <label>Width (cm)<input type="number" min="0.1" step="0.1" required value={form.width} onChange={e => field('width', e.target.value)} /></label>
          <label>Height (cm)<input type="number" min="0.1" step="0.1" required value={form.height} onChange={e => field('height', e.target.value)} /></label>
        </div>

        {addressFields('sender', 'Warehouse sender')}
        {addressFields('receiver', 'Customer recipient')}

        <div className="manual-order-actions">
          <span>After saving: request live rates → choose courier → review delivery list → confirm shipment booking.</span>
          <div>
            <button type="button" className="button" onClick={onClose}>Cancel</button>
            <button className="button primary" disabled={busy}>{busy ? 'Saving…' : 'Save order'}</button>
          </div>
        </div>
      </form>
    </div>
  );
}