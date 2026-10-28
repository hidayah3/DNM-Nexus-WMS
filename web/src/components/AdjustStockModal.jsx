import React, { useState } from 'react';

export function AdjustStockModal({ lot, onClose, onAdjust, session }) {
  const [form, setForm] = useState({ type: 'reduce', quantity: '', reason: '' });

  const handleSubmit = (e) => {
    e.preventDefault();
    onAdjust(lot, form);
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <form className="modal" onSubmit={handleSubmit} onClick={e => e.stopPropagation()}>
        <h2>Adjust stock batch</h2>
        <p>
          {lot.product_name} <span>· SKU {lot.sku} · Batch {lot.batch_number} · On hand: {lot.quantity}</span>
        </p>
        <label>
          Action
          <select value={form.type} onChange={e => setForm({ ...form, type: e.target.value })}>
            <option value="add">Add to this batch</option>
            <option value="reduce">Reduce this batch</option>
          </select>
        </label>
        <label>
          Quantity
          <input
            type="number"
            min="1"
            required
            value={form.quantity}
            onChange={e => setForm({ ...form, quantity: e.target.value })}
          />
        </label>
        <label>
          Reason
          <input
            required
            value={form.reason}
            onChange={e => setForm({ ...form, reason: e.target.value })}
            placeholder="e.g. Stock count correction or damaged goods"
          />
        </label>
        <div className="modal-actions">
          <button type="button" className="button" onClick={onClose}>Cancel</button>
          <button className="button primary">Save audited adjustment</button>
        </div>
        {!session?.authenticated && (
          <small className="form-hint">
            A unique signed-in account is required so this action is attributed to the right staff member.
          </small>
        )}
      </form>
    </div>
  );
}