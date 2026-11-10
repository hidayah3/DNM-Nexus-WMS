// Shared order helpers used by App.jsx, OrdersView.jsx and the order dialogs.

export const STATUS_OPTIONS = ['Pending', 'Processing', 'Cancelled', 'Complete'];

const norm = value => String(value || '').trim().toLowerCase();

export const isCancelled = o => norm(o.status) === 'cancelled';
export const isComplete = o => norm(o.status) === 'complete';
export const isClosed = o => isCancelled(o) || isComplete(o);

// A delivery order is "booked" once EasyParcel has given it a shipment reference or tracking number.
export const isBooked = o => o.method === 'delivery' && Boolean(o.shipment_number || o.tracking);

// What staff see in the Status column and the Status filter.
export const displayStatus = o => {
  if (isCancelled(o)) return 'Cancelled';
  if (isComplete(o)) return 'Complete';
  return isBooked(o) ? 'Processing' : 'Pending';
};

// Cancel: only orders with no EasyParcel shipment yet.
export const canCancel = o => !isClosed(o) && !isBooked(o);
// Complete: booked delivery orders, or self-collection orders.
export const canComplete = o => !isClosed(o) && (isBooked(o) || o.method === 'self_collection');

// Long explanations (used in the order details window).
export const cancelBlockedReason = o => {
  if (isClosed(o)) return `This order is already ${displayStatus(o).toLowerCase()}.`;
  if (isBooked(o)) return 'A shipment is already booked with EasyParcel. Cancel the shipment in EasyParcel first.';
  return '';
};

export const completeBlockedReason = o => {
  if (isClosed(o)) return `This order is already ${displayStatus(o).toLowerCase()}.`;
  if (!canComplete(o)) return 'Book the shipment first. Only booked delivery orders or self-collection orders can be completed.';
  return '';
};

// Short reasons (used under greyed-out items in the Action menu).
export const cancelShortReason = o => {
  if (isClosed(o)) return `Order is ${displayStatus(o).toLowerCase()}`;
  if (isBooked(o)) return 'Shipment already booked';
  return '';
};

export const completeShortReason = o => {
  if (isClosed(o)) return `Order is ${displayStatus(o).toLowerCase()}`;
  if (!canComplete(o)) return 'Book the shipment first';
  return '';
};

export const EMPTY_FILTERS = { status: '', method: '', from: '', to: '' };

export const applyOrderFilters = (orders, f) =>
  orders.filter(o =>
    (!f.status || displayStatus(o) === f.status) &&
    (!f.method || o.method === f.method) &&
    (!f.from || o.date >= f.from) &&
    (!f.to || o.date <= f.to)
  );