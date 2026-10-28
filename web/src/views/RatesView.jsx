import React, { useEffect, useState } from 'react';
import { dhlRates } from '../utils/rates';
import { API, HAS_API, READ_ONLY_PREVIEW, apiFetch, money } from '../utils/api';

export function RatesView() {
  const [region, setRegion] = useState('west');
  const [kg, setKg] = useState(1);
  const [rates, setRates] = useState(() => {
    try {
      return JSON.parse(localStorage.getItem('dnm-dhl-rates')) || JSON.parse(JSON.stringify(dhlRates));
    } catch {
      return JSON.parse(JSON.stringify(dhlRates));
    }
  });
  const [drafts, setDrafts] = useState({});
  const [rateStatus, setRateStatus] = useState('');

  useEffect(() => {
    if (!HAS_API) return;
    apiFetch(`${API}/api/shipping-rates`)
      .then(async r => {
        const data = await r.json();
        if (!r.ok) throw Error(data.error || 'Could not load saved shipping rates');
        const saved = { west: [], east: [] };
        (data.rates || []).forEach(x => {
          if (saved[x.region]) {
            saved[x.region].push({
              kg: x.weight_kg,
              charges: Number(x.courier_charge),
              tax: Number(x.courier_tax),
              packaging: Number(x.packaging_cost)
            });
          }
        });
        if (saved.west.length === 10 && saved.east.length === 10) {
          setRates(saved);
          localStorage.setItem('dnm-dhl-rates', JSON.stringify(saved));
          setRateStatus('Shared rates loaded.');
        } else {
          setRateStatus('Shared rates are not initialized. Apply the WMS database migration.');
        }
      })
      .catch(e => setRateStatus(e.message || 'Could not load shared shipping rates.'));
  }, []);

  const selected = rates[region].find(x => x.kg === Number(kg));

  const edit = (weight, key, value) => {
    setRates(old => {
      const next = {
        ...old,
        [region]: old[region].map(r => r.kg === weight ? { ...r, [key]: Math.max(0, Number(value) || 0) } : r)
      };
      localStorage.setItem('dnm-dhl-rates', JSON.stringify(next));
      return next;
    });
  };

  const amount = (r, key) => Math.max(0, Number(drafts[`${region}-${r.kg}-${key}`] ?? r[key]) || 0);

  const setDraft = (r, key, value) => setDrafts(old => ({ ...old, [`${region}-${r.kg}-${key}`]: value }));

  const commit = async (r, key) => {
    const k = `${region}-${r.kg}-${key}`;
    if (drafts[k] !== undefined) {
      const nextValue = Math.max(0, Number(drafts[k]) || 0);
      edit(r.kg, key, nextValue);
      setDrafts(old => {
        const next = { ...old };
        delete next[k];
        return next;
      });
      if (HAS_API) {
        const row = { ...r, [key]: nextValue };
        const body = {
          region,
          weight_kg: r.kg,
          courier_charge: row.charges,
          courier_tax: row.tax,
          packaging_cost: row.packaging
        };
        try {
          const response = await apiFetch(`${API}/api/shipping-rates`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
          });
          const data = await response.json();
          if (!response.ok) throw Error(data.error || 'Could not save shipping rate');
          setRateStatus(`Saved ${region === 'west' ? 'West' : 'East'} Malaysia ${r.kg} kg rate to shared settings.`);
        } catch (e) {
          setRateStatus(e.message || 'Could not save shared rate.');
        }
      } else {
        setRateStatus('Saved in this browser only.');
      }
    }
  };

  return (
    <div className="panel rate-panel">
      <div className="panel-head">
        <div>
          <h2>DHL price guide <span className="count">Malaysia</span></h2>
          <p>{READ_ONLY_PREVIEW ? 'Review DHL courier charge, courier tax, and packaging cost by weight.' : 'Edit DHL charge, courier tax, and packaging cost by weight.'}</p>
        </div>
        <span className="courier-tag">DHL eCommerce</span>
      </div>
      <div className="rate-controls">
        <label>
          Destination region
          <select value={region} onChange={e => setRegion(e.target.value)}>
            <option value="west">West Malaysia</option>
            <option value="east">East Malaysia</option>
          </select>
        </label>
        <label>
          Chargeable weight
          <select value={kg} onChange={e => setKg(e.target.value)}>
            {rates[region].map(r => <option key={r.kg} value={r.kg}>{r.kg} kg</option>)}
          </select>
        </label>
        <div className="rate-total">
          <small>Selected weight total</small>
          <strong>{money(Math.round((amount(selected, 'charges') + amount(selected, 'tax') + amount(selected, 'packaging')) * 100) / 100)}</strong>
        </div>
      </div>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>WEIGHT</th>
              <th>DHL CHARGES (RM)</th>
              <th>COURIER TAX (RM)</th>
              <th>PACKAGING (RM)</th>
              <th>TOTAL (RM)</th>
            </tr>
          </thead>
          <tbody>
            {rates[region].map(r => (
              <tr key={r.kg} className={r.kg === Number(kg) ? 'rate-selected' : ''}>
                <td><b>{r.kg} kg</b></td>
                {['charges', 'tax', 'packaging'].map((key, i) => (
                  <td key={key}>
                    <input
                      className="rate-input"
                      aria-label={`${['DHL charge', 'Courier tax', 'Packaging cost'][i]} ${r.kg} kg`}
                      type="number"
                      min="0"
                      step="0.01"
                      disabled={READ_ONLY_PREVIEW}
                      value={drafts[`${region}-${r.kg}-${key}`] ?? r[key].toFixed(2)}
                      onChange={e => setDraft(r, key, e.target.value)}
                      onBlur={() => commit(r, key)}
                    />
                  </td>
                ))}
                <td><b>{money(Math.round((amount(r, 'charges') + amount(r, 'tax') + amount(r, 'packaging')) * 100) / 100)}</b></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="rate-note">
        {READ_ONLY_PREVIEW
          ? 'Protected preview: saved courier rates are read-only.'
          : rateStatus || (HAS_API ? 'Changes are saved to shared PostgreSQL settings.' : 'Changes are saved in this browser only; connect PostgreSQL to share admin edits.')}
      </div>
    </div>
  );
}