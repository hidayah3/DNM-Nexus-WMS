import React, { useEffect, useState } from 'react';
import '../css/OrderModals.css';

const MAX_REMARK = 500;

// onConfirm(remark) must return a promise that resolves to '' on success or an error message.
export default function CancelOrderDialog({ order, onClose, onConfirm }) {
  const [remark, setRemark] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    const onKey = e => { if (e.key === 'Escape' && !busy) onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose, busy]);

  const submit = async e => {
    e.preventDefault();
    const text = remark.trim();
    if (text.length < 3) {
      setError('Please enter the reason for cancelling this order (at least 3 characters).');
      return;
    }
    setBusy(true);
    setError('');
    const message = await onConfirm(text);
    if (message) {
      setError(message);
      setBusy(false);
    }
  };

  return (
    <div className="modal-backdrop" onClick={() => { if (!busy) onClose(); }}>
      <form className="modal" onSubmit={submit} onClick={e => e.stopPropagation()}>
        <h2>Cancel order {order.id}</h2>
        <p>
          {order.customer}
          <span> · {order.items}</span>
        </p>

        <label>
          Reason for cancelling <b>*</b>
          <textarea
            className="cancel-remark"
            autoFocus
            required
            maxLength={MAX_REMARK}
            value={remark}
            onChange={e => { setRemark(e.target.value); setError(''); }}
            placeholder="e.g. Customer asked to cancel, wrong address, out of stock"
          />
        </label>
        <div className="cancel-meta">
          <span>The reason is saved with the order and shown in its details.</span>
          <span>{remark.length}/{MAX_REMARK}</span>
        </div>

        {error && <div className="cancel-error" role="alert">{error}</div>}

        <div className="modal-actions">
          <button type="button" className="button" disabled={busy} onClick={onClose}>Keep order</button>
          <button className="button danger-solid" disabled={busy}>{busy ? 'Cancelling…' : 'Cancel order'}</button>
        </div>
      </form>
    </div>
  );
}