import React from 'react';

export function MovementsView({ month, setMonth, movements, downloadReport }) {
  return (
    <div className="panel">
      <div className="panel-head">
        <div>
          <h2>Monthly product movement <span className="count">{movements.length}</span></h2>
          <p>Each stock change is tied to the authenticated account that performed it.</p>
        </div>
        <div className="report-actions">
          <label>Month<input type="month" value={month} onChange={e => setMonth(e.target.value)} /></label>
          <button className="button primary" onClick={downloadReport}>↓ Download Excel report</button>
        </div>
      </div>

      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>DATE / TIME</th>
              <th>SKU</th>
              <th>PRODUCT</th>
              <th>BATCH</th>
              <th>EXPIRY</th>
              <th>MOVEMENT</th>
              <th>QUANTITY</th>
              <th>STOCK AFTER</th>
              <th>REASON</th>
              <th>ACCOUNT</th>
            </tr>
          </thead>
          <tbody>
            {movements.map(m => (
              <tr key={m.id}>
                <td>{new Date(m.created_at).toLocaleString()}</td>
                <td>{m.sku}</td>
                <td>{m.product_name}</td>
                <td>{m.batch_number || '—'}</td>
                <td>{m.expiry_date || '—'}</td>
                <td><span className={`movement ${m.type}`}>{m.type}</span></td>
                <td>{m.quantity}</td>
                <td>{m.stock_after}</td>
                <td>{m.reason}</td>
                <td title={m.actor_id}>{m.actor_name}<small className="subcell">{m.actor_id}</small></td>
              </tr>
            ))}
          </tbody>
        </table>

        {movements.length === 0 && (
          <div className="empty">
            <b>No movements for {month}</b>
            <p>Stock additions and reductions will appear here with the account that made each change.</p>
          </div>
        )}
      </div>
    </div>
  );
}