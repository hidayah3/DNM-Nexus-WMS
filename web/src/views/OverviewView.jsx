import React from 'react';

export function OverviewView({ orders, products, movements, setView }) {
  const totalStockUnits = products.reduce((acc, p) => acc + (Number(p.quantity) || 0), 0);

  return (
    <>
      <div className="metrics">
        <div className="metric">
          <span>All orders</span>
          <strong>{orders.length}</strong>
          <small>From connected order source</small>
        </div>
        <div className="metric">
          <span>Products</span>
          <strong>{products.length}</strong>
          <small>Inventory catalogue</small>
        </div>
        <div className="metric">
          <span>Units in stock</span>
          <strong>{totalStockUnits}</strong>
          <small>Across listed products</small>
        </div>
        <div className="metric">
          <span>This month’s movements</span>
          <strong>{movements.length}</strong>
          <small>Audited stock adjustments</small>
        </div>
      </div>

      <div className="shortcut-grid">
        <button className="shortcut" onClick={() => setView('orders')}>
          <b>Orders</b>
          <span>View and fulfil customer orders →</span>
        </button>
        <button className="shortcut" onClick={() => setView('inventory')}>
          <b>Inventory</b>
          <span>Review balances and adjust stock →</span>
        </button>
        <button className="shortcut" onClick={() => setView('movements')}>
          <b>Stock movement</b>
          <span>Audit and export changes →</span>
        </button>
      </div>
    </>
  );
}