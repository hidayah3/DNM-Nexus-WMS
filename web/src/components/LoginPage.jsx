import React, { useState } from 'react';
import { API, apiFetch } from '../utils/api';
import '../css/LoginPage.css';

// Full-page sign-in. On success the API sets an HttpOnly session cookie and
// returns the signed-in account, which is passed to onSignedIn.
export function LoginPage({ onSignedIn, message }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async e => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const r = await apiFetch(`${API}/api/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: username.trim(), password })
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) throw Error(d.error || 'Could not sign in');
      setPassword('');
      onSignedIn(d);
    } catch (err) {
      const text = err.message || 'Could not sign in';
      setError(text.charAt(0).toUpperCase() + text.slice(1));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="login-shell">
      <form className="login-card" onSubmit={submit}>
        <div className="login-brand">
          <span className="brand-mark">N</span>
          <span>DNM <span className="brand-light">Nexus WMS</span></span>
        </div>
        <h1>Sign in</h1>
        <p className="login-sub">Use your warehouse staff account.</p>

        {message && !error && <div className="login-info" role="status">{message}</div>}
        {error && <div className="login-error" role="alert">{error}</div>}

        <label>
          Username
          <input
            autoFocus
            autoComplete="username"
            value={username}
            onChange={e => setUsername(e.target.value)}
            required
          />
        </label>
        <label>
          Password
          <input
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={e => setPassword(e.target.value)}
            required
          />
        </label>

        <button className="button primary login-submit" disabled={busy}>
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
        <p className="login-help">Forgot your password? Ask your WMS administrator to reset it.</p>
      </form>
    </div>
  );
}
