// Builds the data shown on the commercial invoice from the saved shipment draft.

const text = v => (v === undefined || v === null ? '' : String(v).trim());

const COUNTRIES = { MY: 'Malaysia', SG: 'Singapore', BN: 'Brunei', TH: 'Thailand', ID: 'Indonesia' };

// Areas where EasyParcel asks for a commercial invoice. Postcode first, state text as a fallback.
export const zoneOf = addr => {
  const state = text(addr?.subdivision_code).toLowerCase();
  const digits = text(addr?.postcode).replace(/\D/g, '');
  const pc = digits.length === 5 ? Number(digits) : NaN;
  if (pc >= 87000 && pc <= 87999) return 'Labuan';
  if (pc >= 88000 && pc <= 91999) return 'Sabah';
  if (pc >= 93000 && pc <= 98999) return 'Sarawak';
  if (pc >= 7000 && pc <= 7999) return 'Langkawi';
  if (/sabah|^sbh$|^my-?12$/.test(state)) return 'Sabah';
  if (/sarawak|^srw$|^swk$|^my-?13$/.test(state)) return 'Sarawak';
  if (/labuan|^lbn$|^my-?15$/.test(state)) return 'Labuan';
  return '';
};

const fmtDate = value => {
  const s = text(value).slice(0, 10);
  const d = new Date(`${s}T00:00:00`);
  return Number.isNaN(d.getTime())
    ? s
    : d.toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' });
};

const party = a => {
  const phone = text(a?.phone_number);
  const cc = text(a?.phone_number_country_code);
  return {
    name: text(a?.name),
    lines: [text(a?.address_1), text(a?.address_2)].filter(Boolean),
    cityLine: [text(a?.postcode), text(a?.city)].filter(Boolean).join(' '),
    state: text(a?.subdivision_code),
    country: COUNTRIES[text(a?.country_code).toUpperCase()] || text(a?.country_code) || 'Malaysia',
    phone: phone ? `${cc ? `+${cc} ` : ''}${phone}` : '',
    email: text(a?.email),
    zone: zoneOf(a)
  };
};

export const formatMoney = (currency, n) => `${currency} ${Number(n || 0).toFixed(2)}`;

export const buildInvoice = (order, draft) => {
  const currency = text(draft?.currency_code) || 'MYR';
  const rawItems = Array.isArray(draft?.item) ? draft.item : [];

  let items = rawItems.map(it => ({
    description: text(it.content),
    quantity: Number(it.quantity) || 0,
    hsCode: text(it.hs_code),
    value: Number(it.value) || 0
  }));
  if (!items.length) {
    items = [{ description: text(order?.items), quantity: 1, hsCode: '', value: Number(draft?.parcel_value) || 0 }];
  }

  let total = items.reduce((sum, it) => sum + it.value, 0);
  if (!total && Number(draft?.parcel_value)) total = Number(draft.parcel_value);

  const sender = party(draft?.sender);
  const receiver = party(draft?.receiver);

  const advisory = !sender.zone && !receiver.zone
    ? 'Neither address is in Sabah, Sarawak, Labuan or Langkawi. EasyParcel normally asks for a commercial invoice only when a parcel is sent from or to those areas. You can still print it.'
    : '';

  return {
    number: `CI-${order.id}`,
    date: fmtDate(draft?.collection_date || order?.date),
    orderRef: order.id,
    tracking: text(order?.tracking),
    courier: text(order?.courier),
    sender,
    receiver,
    items,
    total,
    currency,
    weight: Number(draft?.weight) || 0,
    advisory
  };
};