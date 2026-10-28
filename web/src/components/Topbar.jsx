import React from 'react';
import { API, HAS_API, READ_ONLY_PREVIEW, ALLOW_EASYPARCEL_CONNECT } from '../utils/api';

export function Topbar({ pageTitle, connection, setNotice }) {
  const getConnectionText = () => {
    if (!HAS_API) return 'WMS API not configured';
    if (connection?.connected && !connection?.webhook_configured) return 'EasyParcel webhook setup needed';
    if (connection?.connected) {
      if (connection.environment === 'production') return 'EasyParcel live connected';
      if (connection.environment === 'sandbox') return 'EasyParcel sandbox connected';
      return 'EasyParcel connected · environment unverified';
    }
    if (connection?.configured) return 'EasyParcel setup incomplete';
    return 'EasyParcel credentials needed';
  };

  const handleConnectionClick = () => {
    if (!HAS_API) {
      setNotice('This preview is not connected to the Go WMS API, PostgreSQL database, or SMS order source. Live orders and stock changes will be unavailable until your programmers connect them. EasyParcel authorization is separate.');
    } else if (connection?.connected && !connection?.webhook_configured) {
      setNotice('EasyParcel is authorized, but automatic AWB/tracking updates need the EasyParcel webhook token and Developer Hub subscription.');
    } else if (connection?.connected) {
      setNotice(connection.environment === 'production'
        ? 'The EasyParcel production account is authorized; confirming a shipment can charge your wallet.'
        : connection.environment === 'sandbox'
        ? 'The connected EasyParcel account is sandbox-only; it cannot create live customer shipments.'
        : 'The EasyParcel environment is unverified; do not create shipments until the operator confirms it.');
    } else if (connection?.configured) {
      setNotice('The EasyParcel app is configured, but this WMS has not been authorized yet.');
    } else {
      setNotice('EasyParcel client credentials are not configured on the WMS server.');
    }
  };

  return (
    <header className="topbar">
      <div className="crumb">
        DNM Nexus WMS<span>/</span> <b>{pageTitle}</b>
      </div>
      <div className="top-right">
        <span className={`live-dot ${connection?.connected ? '' : 'offline'}`} />
        <button className="connection-label" title="Connection status" onClick={handleConnectionClick}>
          {getConnectionText()}
        </button>
        {HAS_API && connection?.configured && !connection?.connected && (!READ_ONLY_PREVIEW || ALLOW_EASYPARCEL_CONNECT) && (
          <a className="connect-link" href={`${API}/api/easyparcel/connect`}>Connect account</a>
        )}
        <button className="icon-button" onClick={() => setNotice('Use the sidebar to open orders, inventory, and stock movement.')}>?</button>
      </div>
    </header>
  );
}