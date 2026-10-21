package internal

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type OAuthTokens struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_token_expires_at,omitempty"`
	TokenType        string    `json:"token_type"`
}

type OAuthTokenStore interface {
	Load(context.Context) (OAuthTokens, error)
	Save(context.Context, OAuthTokens) error
	Delete(context.Context) error
}

type EasyParcelOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	LoginURL     string
	TokenURL     string
	CookieSecure bool
}

type EasyParcelOAuth struct {
	cfg    EasyParcelOAuthConfig
	store  OAuthTokenStore
	client *http.Client
	mu     sync.Mutex
}

func NewEasyParcelOAuth(cfg EasyParcelOAuthConfig, store OAuthTokenStore) (*EasyParcelOAuth, error) {
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" || strings.TrimSpace(cfg.RedirectURI) == "" || store == nil {
		return nil, ErrProviderNotConfigured
	}
	redirect, err := url.Parse(cfg.RedirectURI)
	if err != nil || redirect.Scheme != "https" || redirect.Host == "" {
		return nil, errors.New("EasyParcel redirect URI must be HTTPS")
	}
	if cfg.LoginURL == "" {
		cfg.LoginURL = "https://api.easyparcel.com/oauth/login"
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://api.easyparcel.com/oauth/token"
	}
	for _, endpoint := range []string{cfg.LoginURL, cfg.TokenURL} {
		u, parseErr := url.Parse(endpoint)
		if parseErr != nil || u.Scheme != "https" || u.Host != "api.easyparcel.com" {
			return nil, errors.New("EasyParcel OAuth endpoints must use api.easyparcel.com over HTTPS")
		}
	}
	return &EasyParcelOAuth{cfg: cfg, store: store, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (o *EasyParcelOAuth) Connect(w http.ResponseWriter, r *http.Request) {
	state := make([]byte, 32)
	if _, err := rand.Read(state); err != nil {
		writeError(w, http.StatusInternalServerError, "could not start EasyParcel connection")
		return
	}
	stateValue := fmt.Sprintf("%x", state)
	http.SetCookie(w, &http.Cookie{Name: "wms_easyparcel_oauth_state", Value: stateValue, Path: "/api/easyparcel", HttpOnly: true, Secure: o.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	login, _ := url.Parse(o.cfg.LoginURL)
	query := login.Query()
	query.Set("client_id", o.cfg.ClientID)
	query.Set("redirect_uri", o.cfg.RedirectURI)
	query.Set("state", stateValue)
	login.RawQuery = query.Encode()
	http.Redirect(w, r, login.String(), http.StatusFound)
}

func (o *EasyParcelOAuth) Callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if providerErr := r.URL.Query().Get("error"); providerErr != "" {
		clearStateCookie(w, o.cfg.CookieSecure)
		writeError(w, http.StatusBadRequest, "EasyParcel authorization was not completed")
		return
	}
	cookie, err := r.Cookie("wms_easyparcel_oauth_state")
	state := r.URL.Query().Get("state")
	if err != nil || state == "" || len(state) != len(cookie.Value) || subtle.ConstantTimeCompare([]byte(state), []byte(cookie.Value)) != 1 {
		clearStateCookie(w, o.cfg.CookieSecure)
		writeError(w, http.StatusBadRequest, "EasyParcel authorization state did not match; start the connection again")
		return
	}
	clearStateCookie(w, o.cfg.CookieSecure)
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "EasyParcel did not return an authorization code")
		return
	}
	tokens, err := o.exchange(r.Context(), url.Values{"grant_type": {"authorization_code"}, "redirect_uri": {o.cfg.RedirectURI}, "code": {code}})
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not complete EasyParcel authorization; check the developer app settings")
		return
	}
	if err = o.store.Save(r.Context(), tokens); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the encrypted EasyParcel connection")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "<!doctype html><title>EasyParcel connected</title><main style='font:16px system-ui;margin:3rem auto;max-width:36rem'><h1>EasyParcel connected</h1><p>You can close this page and return to the WMS.</p></main>")
}

func (o *EasyParcelOAuth) Status(w http.ResponseWriter, r *http.Request) {
	tokens, err := o.store.Load(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "connected": tokens.AccessToken != "" || tokens.RefreshToken != "", "expires_at": tokens.ExpiresAt})
}

func (o *EasyParcelOAuth) Disconnect(w http.ResponseWriter, r *http.Request) {
	if err := o.store.Delete(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not disconnect EasyParcel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"connected": false})
}

func (o *EasyParcelOAuth) AccessToken(ctx context.Context) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	tokens, err := o.store.Load(ctx)
	if err != nil {
		return "", ErrNoEasyParcelConnection
	}
	if tokens.AccessToken != "" && time.Until(tokens.ExpiresAt) > 90*time.Second {
		return tokens.AccessToken, nil
	}
	if tokens.RefreshToken == "" {
		return "", ErrNoEasyParcelConnection
	}
	refreshed, err := o.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "redirect_uri": {o.cfg.RedirectURI}, "refresh_token": {tokens.RefreshToken}})
	if err != nil {
		return "", errors.New("EasyParcel authorization expired; reconnect the account")
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = tokens.RefreshToken
	}
	if err := o.store.Save(ctx, refreshed); err != nil {
		return "", errors.New("could not save renewed EasyParcel authorization")
	}
	return refreshed.AccessToken, nil
}

func (o *EasyParcelOAuth) exchange(ctx context.Context, form url.Values) (OAuthTokens, error) {
	var tokens OAuthTokens
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokens, err
	}
	request.SetBasicAuth(o.cfg.ClientID, o.cfg.ClientSecret)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := o.client.Do(request)
	if err != nil {
		return tokens, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return tokens, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return tokens, fmt.Errorf("EasyParcel OAuth returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		AccessToken      string    `json:"access_token"`
		RefreshToken     string    `json:"refresh_token"`
		TokenType        string    `json:"token_type"`
		ExpiresIn        int64     `json:"expires_in"`
		ExpiresAt        time.Time `json:"expires_at"`
		RefreshExpiresAt time.Time `json:"refresh_token_expires_at"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return tokens, err
	}
	if payload.AccessToken == "" {
		return tokens, errors.New("EasyParcel OAuth response did not include an access token")
	}
	if payload.ExpiresAt.IsZero() {
		payload.ExpiresAt = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	return OAuthTokens{AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, TokenType: payload.TokenType, ExpiresAt: payload.ExpiresAt, RefreshExpiresAt: payload.RefreshExpiresAt}, nil
}

func clearStateCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: "wms_easyparcel_oauth_state", Value: "", Path: "/api/easyparcel", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

// Used by tests and adapters that marshal token payloads without exposing secrets in logs.
func marshalOAuthTokens(tokens OAuthTokens) ([]byte, error) { return json.Marshal(tokens) }
func unmarshalOAuthTokens(data []byte) (OAuthTokens, error) {
	var t OAuthTokens
	err := json.NewDecoder(bytes.NewReader(data)).Decode(&t)
	return t, err
}
