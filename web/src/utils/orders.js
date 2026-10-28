// Shared order helpers used by App.jsx, OrdersView.jsx and OrderFilters.jsx

// A delivery order is "booked" once EasyParcel has given it a shipment reference or tracking number.
export const isBooked = o => o.method === 'delivery' && Boolean(o.shipment_number || o.tracking);

// What the Status column shows. Booked orders read "Ready to deliver".
export const displayStatus = o => (isBooked(o) ? 'Ready to deliver' : o.status);

export const EMPTY_FILTERS = { status: '', method: '', from: '', to: '' };

export const applyOrderFilters = (orders, f) =>
  orders.filter(o =>
    (!f.status || displayStatus(o) === f.status) &&
    (!f.method || o.method === f.method) &&
    (!f.from || o.date >= f.from) &&
    (!f.to || o.date <= f.to)
  );