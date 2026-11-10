import React from 'react';

export function Sidebar({ view, setView, menu, setMenu, ordersCount, deliveryCount, session, setNotice, onSignOut }) {
  const actorName = session?.actor?.name || 'Warehouse admin';
  const actorInitials = actorName.slice(0, 2).toUpperCase();

  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand-mark">N</span>
        <span>DNM <span className="brand-light">Nexus WMS</span></span>
      </div>

      <div className="workspace-label">WORKSPACE</div>
      <div className="workspace">
        <span className="workspace-dot">N</span>
        <span>DNM Nexus Warehouse<small>WMS module</small></span>
      </div>

      <div className="nav-label">OPERATIONS</div>
      <button className={`nav-item ${view === 'overview' ? 'active' : ''}`} onClick={() => { setView('overview'); setMenu(''); }}>
        <span>◫</span> Dashboard
      </button>
      <button className={`nav-item ${view === 'orders' ? 'active' : ''}`} onClick={() => { setView('orders'); setMenu(''); }}>
        <span>▤</span> Orders <i>{ordersCount}</i>
      </button>
      <button className={`nav-item ${view === 'delivery' ? 'active' : ''}`} onClick={() => { setView('delivery'); setMenu(''); }}>
        <span>▱</span> Delivery list {deliveryCount > 0 && <i>{deliveryCount}</i>}
      </button>
      <button className={`nav-item ${view === 'inventory' ? 'active' : ''}`} onClick={() => { setView('inventory'); setMenu(''); }}>
        <span>▦</span> Inventory
      </button>
      <button className={`nav-item ${view === 'movements' ? 'active' : ''}`} onClick={() => { setView('movements'); setMenu(''); }}>
        <span>⇄</span> Stock movement
      </button>
      {/* <button className={`nav-item ${view === 'rates' ? 'active' : ''}`} onClick={() => { setView('rates'); setMenu(''); }}>
        <span>▱</span> Shipping courier
      </button> */}

      <div className="sidebar-bottom">
        <div className="profile menu-anchor">
          <div className="avatar">{actorInitials}</div>
          <span>
            {actorName}
            <small>{session?.authenticated ? 'Signed in' : 'Not signed in'}</small>
          </span>
          <button className="dots" aria-label="Account menu" onClick={() => setMenu(menu === 'profile' ? '' : 'profile')}>
            ···
          </button>
          {menu === 'profile' && (
            <div className="popover profile-pop">
              <b>{actorName}</b>
              <small>Username: {session?.actor?.id || '—'}</small>
              <button onClick={onSignOut}>
                Sign out
              </button>
            </div>
          )}
        </div>
      </div>
    </aside>
  );
}