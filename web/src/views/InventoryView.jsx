import React from 'react';
import { READ_ONLY_PREVIEW } from '../utils/api';

export function InventoryView({
  products,
  lots,
  inventorySync,
  productForm,
  setProductForm,
  newProduct,
  setNewProduct,
  handleReceiveInventory,
  onOpenAdjust
}) {
  return (
    <div className="panel">
      <div className="panel-head">
        <div>
          <h2>Product batches <span className="count">{products.length} products · {lots.length} batches</span></h2>
          <p>Each receipt records its batch, expiry date, quantity and authenticated staff account. Stock totals are calculated across batches.</p>
        </div>
        <span className={`sync-pill ${inventorySync?.configured ? 'sync-ready' : 'sync-pending'}`}>
          {inventorySync?.configured ? 'SMS sync endpoint configured' : 'SMS stock sync awaits SMS receiver'}
        </span>
      </div>

      {productForm && (
        <form className="inline-form inventory-receipt-form" onSubmit={handleReceiveInventory}>
          <label>Product name<input required value={newProduct.name} onChange={e => setNewProduct({ ...newProduct, name: e.target.value })} placeholder="Product name" /></label>
          <label>SKU<input required value={newProduct.sku} onChange={e => setNewProduct({ ...newProduct, sku: e.target.value })} placeholder="Exact SMS product SKU" /></label>
          <label>Batch number<input required value={newProduct.batch_number} onChange={e => setNewProduct({ ...newProduct, batch_number: e.target.value })} /></label>
          <label>Expiry date<input type="date" required value={newProduct.expiry_date} onChange={e => setNewProduct({ ...newProduct, expiry_date: e.target.value })} /></label>
          <label>Total quantity received<input type="number" min="1" required value={newProduct.quantity} onChange={e => setNewProduct({ ...newProduct, quantity: e.target.value })} /></label>
          <label>Reason / PO reference<input required value={newProduct.reason} onChange={e => setNewProduct({ ...newProduct, reason: e.target.value })} placeholder="PO number or receipt reason" /></label>
          <button className="button primary">Save receipt</button>
          <button type="button" className="button" onClick={() => setProductForm(false)}>Cancel</button>
        </form>
      )}

      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>PRODUCT / SKU</th>
              <th>BATCH NUMBER</th>
              <th>EXPIRY</th>
              <th>QUANTITY IN BATCH</th>
              <th>TOTAL SKU STOCK</th>
              <th>INPUT ACCOUNT</th>
              <th>ACTIONS</th>
            </tr>
          </thead>
          <tbody>
            {lots.map(lot => {
              const p = products.find(x => x.sku === lot.sku);
              return (
                <tr key={lot.id}>
                  <td><b>{lot.product_name}</b><small className="subcell">SKU {lot.sku}</small></td>
                  <td>{lot.batch_number}</td>
                  <td>{lot.expiry_date ? new Date(`${String(lot.expiry_date).slice(0, 10)}T00:00:00`).toLocaleDateString() : <span className="muted">Unknown legacy date</span>}</td>
                  <td>{lot.quantity}</td>
                  <td><b>{p?.quantity ?? lot.quantity}</b></td>
                  <td title={lot.created_by}>{lot.created_name}<small className="subcell">{lot.created_by}</small></td>
                  <td>
                    <button className="button small" disabled={READ_ONLY_PREVIEW} onClick={() => onOpenAdjust(lot)}>
                      Adjust batch
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>

        {lots.length === 0 && (
          <div className="empty">
            <b>No stock batches recorded</b>
            <p>Choose Receive stock to enter a real product, SKU, batch, expiry date and quantity. No sample inventory is included.</p>
          </div>
        )}
      </div>

      {!inventorySync?.configured && (
        <div className="inventory-sync-note">
          Every saved receipt or adjustment is stored in WMS and queued for SMS. Automatic delivery begins when the SMS developer supplies the authenticated stock receiver URL and token.
        </div>
      )}
    </div>
  );
}