export const API = import.meta.env.VITE_API_BASE_URL || '';
export const HAS_API = import.meta.env.VITE_WMS_API_ENABLED === 'true' || Boolean(API);
export const READ_ONLY_PREVIEW = import.meta.env.VITE_WMS_READ_ONLY_PREVIEW === 'true';
export const ALLOW_EASYPARCEL_CONNECT = import.meta.env.VITE_WMS_EASYPARCEL_CONNECT === 'true';

export const COURIERS = (() => {
  try {
    return JSON.parse(import.meta.env.VITE_WMS_COURIERS || '[]');
  } catch {
    return [];
  }
})();

export const apiFetch = (url, options = {}) =>
  fetch(url, { credentials: 'include', ...options });

export const monthNow = () => new Date().toISOString().slice(0, 7);

export const money = (n) => `RM${Number(n || 0).toFixed(2)}`;

export const localDate = () => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
};

