import React, { useEffect, useMemo, useState } from 'react';
import {
  API,
  HAS_API,
  READ_ONLY_PREVIEW,
  COURIERS,
  apiFetch,
  monthNow
} from './utils/api';

import { Sidebar } from './components/Sidebar';
import { Topbar } from './components/Topbar';
import { ManualOrderDialog } from './components/ManualOrderDialog';
import { AdjustStockModal } from './components/AdjustStockModal';

import { OverviewView } from './views/OverviewView';
import { OrdersView } from './views/OrdersView';
import { DeliveryView } from './views/DeliveryView';
import { InventoryView } from './views/InventoryView';
import { MovementsView } from './views/MovementsView';
import { EMPTY_FILTERS, applyOrderFilters, isBooked } from './utils/orders.js';
// import { RatesView } from './views/RatesView';

export default function App() {
  const [view, setView] = useState('orders');
  const [orders, setOrders] = useState([]);
  const [products, setProducts] = useState([]);
  const [lots, setLots] = useState([]);
  const [movements, setMovements] = useState([]);
  const [selected, setSelected] = useState([]);
  const [filters, setFilters] = useState(EMPTY_FILTERS);
  const [month, setMonth] = useState(monthNow());
  const [notice, setNotice] = useState('');
  const [connection, setConnection] = useState(null);
  const [session, setSession] = useState(null);
  const [inventorySync, setInventorySync] = useState(null);
  const [menu, setMenu] = useState('');

  // Dialog & Form states
  const [productForm, setProductForm] = useState(false);
  const [orderForm, setOrderForm] = useState(false);
  const [orderSubmitting, setOrderSubmitting] = useState(false);
  const [adjust, setAdjust] = useState(null);
  const [newProduct, setNewProduct] = useState({
    sku: '', name: '', batch_number: '', expiry_date: '', quantity: '', reason: 'Goods received'
  });
  const [courierQuotes, setCourierQuotes] = useState({});
  const [quoteLoading, setQuoteLoading] = useState({});
  const [awbLoading, setAwbLoading] = useState({});

  // 1. Load Data
  const loadOrders = async () => {
    if (!HAS_API) return;
    try {
      const r = await apiFetch(`${API}/api/orders`);
      const d = await r.json();
      if (!r.ok) throw Error(d.error);
      setOrders((d.orders || []).map(o => ({ ...o, date: String(o.date).slice(0, 10) })));
    } catch {
      setNotice('Could not reach the order API. No sample orders are shown.');
    }
  };

  const loadInventory = async () => {
    if (!HAS_API) return;
    try {
      const r = await apiFetch(`${API}/api/inventory`);
      const d = await r.json();
      if (!r.ok) throw Error(d.error);
      setProducts(d.products || []);
      setLots(d.lots || []);
    } catch (e) {
      setNotice(e.message || 'Inventory service is not configured.');
    }
  };

  const loadInventorySync = async () => {
    if (!HAS_API) return;
    try {
      const r = await apiFetch(`${API}/api/inventory/sync-status`);
      if (r.ok) setInventorySync(await r.json());
    } catch {
      setInventorySync(null);
    }
  };

  const loadMovements = async () => {
    if (!HAS_API) return;
    try {
      const r = await apiFetch(`${API}/api/inventory/movements?month=${month}`);
      const d = await r.json();
      if (!r.ok) throw Error(d.error);
      setMovements(d.movements || []);
    } catch (e) {
      setNotice(e.message || 'Movement report is not configured.');
    }
  };

  useEffect(() => {
    if (!HAS_API) return;
    loadOrders();
    loadInventory();
    loadMovements();
    loadInventorySync();
    apiFetch(`${API}/api/session`, { credentials: 'include' })
      .then(r => r.json())
      .then(setSession)
      .catch(() => setSession(null));
    apiFetch(`${API}/api/easyparcel/status`)
      .then(r => r.ok ? r.json() : Promise.reject())
      .then(setConnection)
      .catch(() => setConnection({ connected: false }));
  }, []);

  useEffect(() => {
    if (!HAS_API) return;
    const timer = window.setInterval(loadOrders, 30000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (HAS_API) loadMovements();
  }, [month]);

  // Filters & Selection
  const filteredOrders = useMemo(() => applyOrderFilters(orders, filters), [orders, filters]);
  const chosenOrders = orders.filter(o => selected.includes(o.id));
  // Selected orders that already have an EasyParcel shipment (used by Print AWB).
  const bookedSelected = chosenOrders.filter(isBooked);
  // Selected delivery orders that still need booking (shown on the Delivery list).
  const bookableOrders = chosenOrders.filter(o => o.method === 'delivery' && !isBooked(o));
  const readyOrders = bookableOrders.filter(o => o.courier_code);

  const toggleSelect = id => setSelected(xs => xs.includes(id) ? xs.filter(x => x !== id) : [...xs, id]);
  const toggleAll = () => setSelected(xs => filteredOrders.length && filteredOrders.every(o => xs.includes(o.id)) ? xs.filter(id => !filteredOrders.some(o => o.id === id)) : [...new Set([...xs, ...filteredOrders.map(o => o.id)])]);

  // Mutation Handlers
  const updateOrder = (id, key, value) => setOrders(xs => xs.map(o => o.id === id ? { ...o, [key]: value } : o));

  const saveFulfilment = async (id, method, code = '') => {
    updateOrder(id, 'method', method);
    if (HAS_API) {
      try {
        const r = await apiFetch(`${API}/api/orders/${encodeURIComponent(id)}/fulfilment`, {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method, courier_code: code })
        });
        if (!r.ok) throw Error();
      } catch {
        setNotice('Could not save fulfilment choice.');
      }
    }
  };

  const setMethod = (o, method) => {
    if (method === 'self_collection') {
      updateOrder(o.id, 'courier', '');
      updateOrder(o.id, 'courier_code', '');
      saveFulfilment(o.id, method);
    } else {
      saveFulfilment(o.id, method, o.courier_code || '');
    }
  };

  const quoteCouriers = async o => {
    if (READ_ONLY_PREVIEW) return setNotice('Preview only: live EasyParcel rates cannot be requested in this read-only environment.');
    if (!HAS_API) return setNotice('Live courier rates require the connected WMS API.');
    setQuoteLoading(x => ({ ...x, [o.id]: true }));
    try {
      const r = await apiFetch(`${API}/api/orders/${encodeURIComponent(o.id)}/courier-quotes`, { method: 'POST' });
      const d = await r.json();
      if (!r.ok) throw Error(d.error || 'Could not load EasyParcel courier rates');
      const quotes = d.quotes || [];
      setCourierQuotes(x => ({ ...x, [o.id]: quotes }));
      setNotice(quotes.length ? `${quotes.length} live EasyParcel service(s) loaded for ${o.id}.` : `EasyParcel returned no service for ${o.id}.`);
    } catch (e) {
      setNotice(e.message || 'Could not load EasyParcel courier rates.');
    } finally {
      setQuoteLoading(x => ({ ...x, [o.id]: false }));
    }
  };

  const chooseCourier = (o, serviceId) => {
    const c = (courierQuotes[o.id] || COURIERS).find(x => x.service_id === serviceId);
    updateOrder(o.id, 'courier', c?.service_name || c?.courier_name || c?.name || serviceId);
    updateOrder(o.id, 'courier_code', serviceId);
    saveFulfilment(o.id, 'delivery', serviceId);
  };

  const createManualOrder = async form => {
    if (!HAS_API) return setNotice('Order entry requires the connected Go WMS API and PostgreSQL.');
    setOrderSubmitting(true);
    const id = form.id.trim(), weight = Number(form.weight), currency = form.currency_code.trim().toUpperCase();
    const address = prefix => ({
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
    const payload = {
      id,
      date: new Date(`${form.date}T12:00:00`).toISOString(),
      customer: form.receiver_name.trim(),
      items: form.items.trim(),
      status: 'Ready to fulfil',
      shipment: {
        order_id: id,
        reference: id,
        collection_date: form.date,
        sender: address('sender'),
        receiver: address('receiver'),
        weight,
        height: Number(form.height),
        length: Number(form.length),
        width: Number(form.width),
        parcel_value: Number(form.parcel_value),
        currency_code: currency,
        item: [{
          content: form.contents.trim(),
          weight,
          height: Number(form.height),
          length: Number(form.length),
          width: Number(form.width),
          currency_code: currency,
          value: Number(form.parcel_value),
          quantity: Number(form.quantity)
        }]
      }
    };
    try {
      const r = await apiFetch(`${API}/api/orders`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      const d = await r.json();
      if (!r.ok) throw Error(d.error || 'Could not save the order');
      setOrderForm(false);
      setSelected([id]);
      await loadOrders();
      setNotice(`Order ${id} saved. Select it and choose Get live rates to continue.`);
    } catch (e) {
      setNotice(e.message);
    } finally {
      setOrderSubmitting(false);
    }
  };

  const deleteOrder = async o => {
    if (READ_ONLY_PREVIEW) return setNotice('Preview only: orders cannot be deleted in this read-only environment.');
    if (!HAS_API) return setNotice('Deleting orders requires the connected WMS API.');
    if (o.shipment_number || o.tracking) return setNotice('This order already has a shipment, so it cannot be deleted.');
    if (!window.confirm(`Delete order ${o.id} for ${o.customer}? This cannot be undone.`)) return;
    try {
      const r = await apiFetch(`${API}/api/orders/${encodeURIComponent(o.id)}`, { method: 'DELETE' });
      if (!r.ok) {
        let msg = 'Could not delete the order';
        try { msg = (await r.json()).error || msg; } catch {}
        throw Error(msg);
      }
      setOrders(xs => xs.filter(x => x.id !== o.id));
      setSelected(xs => xs.filter(id => id !== o.id));
      setNotice(`Order ${o.id} deleted.`);
    } catch (e) {
      setNotice(e.message);
    }
  };

  const pushShipments = async () => {
    if (READ_ONLY_PREVIEW) return setNotice('Preview only: this click did not send anything to EasyParcel. Connect SMS orders and enable the operational WMS environment to create shipments.');
    if (!readyOrders.length) return setNotice('Choose a courier for each order first.');
    if (!HAS_API) return setNotice('Shipment submission needs the connected WMS API.');
    if (!window.confirm(`Confirm and send ${readyOrders.length} order(s) to EasyParcel? This creates shipment bookings and may charge your account.`)) return;
    try {
      const r = await apiFetch(`${API}/api/delivery-batches/push`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ orders: readyOrders.map(o => ({ order_id: o.id, service_id: o.courier_code })) })
      });
      const d = await r.json();
      if (!r.ok) throw Error(d.error || 'Shipment request failed');
      const results = d.results || [];
      setOrders(xs => xs.map(o => {
        const x = results.find(y => y.order_id === o.id);
        return x?.status === 'created' ? { ...o, tracking: x.tracking_number || o.tracking, shipment_number: x.shipment_number || o.shipment_number, awb_url: x.awb_url || o.awb_url, provider_status: 'created' } : o;
      }));
      const created = results.filter(x => x.status === 'created').length;
      const failed = results.filter(x => x.status === 'failed').length;
      const unknown = results.filter(x => x.status === 'outcome_unknown').length;
      const awbPending = (d.awb_pending_order_ids || []).length;
      setNotice(`${created} shipment(s) created; ${failed} failed; ${unknown} need reconciliation. ${awbPending ? `AWB is still processing for ${awbPending} order(s); use Get AWB on the order to retry.` : 'Use Print AWB or Open AWB on the Orders page.'}`);
      loadOrders();
      if (created > 0) setView('orders');
    } catch (e) {
      setNotice(e.message);
    }
  };

  // Opens the AWB for the given booked orders (one browser tab per order).
  const openAWB = async rows => {
    if (READ_ONLY_PREVIEW) return setNotice('Preview only: no EasyParcel request was sent. AWB printing is available after a shipment exists.');
    if (!HAS_API) return setNotice('The connected WMS API is required to open AWB documents.');
    if (!rows.length) return setNotice('Select orders with a booked EasyParcel shipment first.');
    setAwbLoading(x => ({ ...x, ...Object.fromEntries(rows.map(o => [o.id, true])) }));
    const tabs = rows.map(() => window.open('about:blank', '_blank'));
    try {
      const r = await apiFetch(`${API}/api/delivery-batches/awb`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ order_ids: rows.map(o => o.id) })
      });
      const d = await r.json();
      if (!r.ok) throw Error(d.error || 'AWB request failed');
      const awbs = d.awbs || [];
      setOrders(xs => xs.map(o => {
        const x = awbs.find(y => y.order_id === o.id);
        return x ? { ...o, tracking: x.tracking_number || o.tracking, awb_url: x.awb_url || o.awb_url } : o;
      }));
      let opened = 0;
      rows.forEach((o, i) => {
        const awb = awbs.find(x => x.order_id === o.id);
        const url = awb?.awb_url || o.awb_url;
        if (url && tabs[i]) {
          tabs[i].location.replace(url);
          opened++;
        } else if (tabs[i]) {
          tabs[i].close();
        }
      });
      setNotice(opened ? `${opened} AWB document(s) opened. Use your browser's print command to print them.` : 'EasyParcel has not returned AWB links yet; try again in a minute.');
    } catch (e) {
      tabs.forEach(t => t && t.close());
      setNotice(e.message);
    } finally {
      setAwbLoading(x => {
        const next = { ...x };
        rows.forEach(o => { delete next[o.id]; });
        return next;
      });
    }
  };

  const printAWB = () => openAWB(bookedSelected);
  const getAWB = o => openAWB([o]);

  const receiveInventory = async e => {
    e.preventDefault();
    try {
      const r = await apiFetch(`${API}/api/inventory/receipts`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...newProduct, quantity: Number(newProduct.quantity) })
      });
      const d = await r.json();
      if (!r.ok) throw Error(d.error || 'Could not record stock receipt');
      setProductForm(false);
      setNewProduct({ sku: '', name: '', batch_number: '', expiry_date: '', quantity: '', reason: 'Goods received' });
      loadInventory();
      loadMovements();
      loadInventorySync();
      setNotice(`Received ${d.movement.quantity} units of ${d.product.name}; recorded under ${d.movement.actor_name}. Current stock snapshot queued for SMS.`);
    } catch (e) {
      setNotice(e.message);
    }
  };

  const adjustStock = async (lot, adjustForm) => {
    try {
      const r = await apiFetch(`${API}/api/inventory/adjustments`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ lot_id: lot.id, ...adjustForm, quantity: Number(adjustForm.quantity) })
      });
      const d = await r.json();
      if (!r.ok) throw Error(d.error || 'Could not update stock');
      setAdjust(null);
      loadInventory();
      loadMovements();
      loadInventorySync();
      setNotice(`Stock ${d.movement.type === 'add' ? 'added' : 'reduced'} by ${d.movement.quantity}; change recorded under ${d.movement.actor_name}. Current stock snapshot queued for SMS.`);
    } catch (e) {
      setNotice(e.message);
    }
  };

  const downloadReport = async () => {
    if (!HAS_API) return setNotice('Connect the WMS API to download the stock movement report.');
    try {
      const r = await apiFetch(`${API}/api/inventory/movements?month=${month}&format=xlsx`, { credentials: 'include' });
      if (!r.ok) throw Error('Could not download the report');
      const blob = await r.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `stock-movement-${month}.xlsx`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setNotice(e.message);
    }
  };

  const downloadInventory = async () => {
    if (!HAS_API) return setNotice('Connect the WMS API to download the inventory report.');
    try {
      const r = await apiFetch(`${API}/api/inventory/report`, { credentials: 'include' });
      if (!r.ok) throw Error('Could not download the inventory report');
      const blob = await r.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'inventory-batches.xlsx';
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setNotice(e.message);
    }
  };

  const pageTitle = {
    overview: 'Dashboard',
    orders: 'Orders',
    delivery: 'Delivery list',
    inventory: 'Inventory',
    movements: 'Stock movement'
    // rates: 'Shipping courier'
  }[view];

  return (
    <div className="shell">
      <Sidebar
        view={view}
        setView={setView}
        menu={menu}
        setMenu={setMenu}
        ordersCount={orders.length}
        deliveryCount={bookableOrders.length}
        session={session}
        setNotice={setNotice}
      />

      <main className="main">
        <Topbar pageTitle={pageTitle} connection={connection} setNotice={setNotice} />

        <section className="content">
          <div className="heading-row">
            <div>
              <div className="eyebrow">DNM NEXUS WMS</div>
              <h1>{pageTitle}</h1>
              <p className="subtitle">
                {view === 'orders' ? 'Manage orders, fulfilment, shipment status and AWB.' :
                 view === 'inventory' ? 'Record product batches, expiry dates, quantities and the staff account receiving them.' :
                 view === 'movements' ? 'Review and export monthly stock movement.' :
                 view === 'overview' ? 'Warehouse activity at a glance.' :
                 view === 'rates' ? 'Check the supplied DHL rate sheet by region and chargeable weight.' :
                 'Book EasyParcel shipments for the orders you selected.'}
              </p>
            </div>
            <div className="heading-actions">
              {view === 'orders' && (
                <button className="button primary" disabled={!HAS_API || READ_ONLY_PREVIEW} onClick={() => setOrderForm(true)}>
                  ＋ Enter order
                </button>
              )}
              {view === 'delivery' && (
                <button className="button secondary" onClick={() => setView('orders')}>
                  ← Back to orders
                </button>
              )}
              {view === 'inventory' && (
                <>
                  <button className="button" disabled={!HAS_API} onClick={downloadInventory}>↓ Excel report</button>
                  <button
                    className="button primary"
                    disabled={!HAS_API || READ_ONLY_PREVIEW}
                    onClick={() => HAS_API && !READ_ONLY_PREVIEW ? setProductForm(!productForm) : setNotice('Warehouse edits are disabled until staff sign-in is enabled.')}
                  >
                    ＋ Receive stock
                  </button>
                </>
              )}
              {view === 'movements' && (
                <button className="button primary" disabled={!HAS_API} onClick={downloadReport}>
                  ↓ Download Excel report
                </button>
              )}
            </div>
          </div>

          {READ_ONLY_PREVIEW && (
            <div className="notice preview-notice" role="status">
              <b>Protected read-only preview.</b> No sample orders are shown. Delivery buttons are clickable for preview, but requests are held and cannot create shipments.
            </div>
          )}

          {notice && (
            <div className="notice" role="status">
              {notice}
              <button onClick={() => setNotice('')}>×</button>
            </div>
          )}

          {view === 'overview' && (
            <OverviewView orders={orders} products={products} movements={movements} setView={setView} />
          )}

          {view === 'orders' && (
            <OrdersView
              orders={orders}
              filteredOrders={filteredOrders}
              selected={selected}
              toggleSelect={toggleSelect}
              toggleAll={toggleAll}
              filters={filters}
              setFilters={setFilters}
              setMethod={setMethod}
              quoteCouriers={quoteCouriers}
              chooseCourier={chooseCourier}
              courierQuotes={courierQuotes}
              quoteLoading={quoteLoading}
              goToDelivery={() => setView('delivery')}
              deleteOrder={deleteOrder}
              printAWB={printAWB}
              getAWB={getAWB}
              awbLoading={awbLoading}
            />
          )}

          {view === 'delivery' && (
            <DeliveryView
              deliveryOrders={bookableOrders}
              readyOrders={readyOrders}
              pushShipments={pushShipments}
              goToOrders={() => setView('orders')}
            />
          )}

          {view === 'inventory' && (
            <InventoryView
              products={products}
              lots={lots}
              inventorySync={inventorySync}
              productForm={productForm}
              setProductForm={setProductForm}
              newProduct={newProduct}
              setNewProduct={setNewProduct}
              handleReceiveInventory={receiveInventory}
              onOpenAdjust={setAdjust}
            />
          )}

          {view === 'movements' && (
            <MovementsView
              month={month}
              setMonth={setMonth}
              movements={movements}
              downloadReport={downloadReport}
            />
          )}

          {/* {view === 'rates' && <RatesView />} */}

        </section>
      </main>

      {orderForm && (
        <ManualOrderDialog
          onClose={() => setOrderForm(false)}
          onCreate={createManualOrder}
          busy={orderSubmitting}
        />
      )}

      {adjust && (
        <AdjustStockModal
          lot={adjust}
          onClose={() => setAdjust(null)}
          onAdjust={adjustStock}
          session={session}
        />
      )}
    </div>
  );
}