// Package wmsadapter is a standard-library Go client SMS can copy into its
// backend to send order snapshots to WMS and read shipment results back.
package wmsadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New creates an authenticated SMS-to-WMS client. Production URLs must use
// HTTPS; plain HTTP is accepted only for localhost development.
func New(baseURL, token string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	token = strings.TrimSpace(token)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("WMS API base URL must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLocalHost(parsed.Hostname())) {
		return nil, errors.New("WMS API must use HTTPS outside localhost development")
	}
	if token == "" {
		return nil, errors.New("SMS-to-WMS bearer token is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: baseURL, token: token, http: httpClient}, nil
}

type OrderSnapshot struct {
	ID       string        `json:"id"`
	Date     time.Time     `json:"date"`
	Customer string        `json:"customer"`
	Items    string        `json:"items"`
	Status   string        `json:"status"`
	Shipment ShipmentDraft `json:"shipment"`
}

type ShipmentDraft struct {
	CollectionDate string       `json:"collection_date"`
	Sender         Address      `json:"sender"`
	Receiver       Address      `json:"receiver"`
	Weight         float64      `json:"weight"`
	Height         float64      `json:"height"`
	Length         float64      `json:"length"`
	Width          float64      `json:"width"`
	ParcelValue    float64      `json:"parcel_value"`
	CurrencyCode   string       `json:"currency_code"`
	Items          []ParcelItem `json:"item"`
}

type Address struct {
	Name                   string `json:"name"`
	Company                string `json:"company,omitempty"`
	PhoneNumberCountryCode string `json:"phone_number_country_code"`
	PhoneNumber            string `json:"phone_number"`
	AlternatePhoneCountry  string `json:"alternate_phone_number_country_code,omitempty"`
	AlternatePhoneNumber   string `json:"alternate_phone_number,omitempty"`
	Email                  string `json:"email,omitempty"`
	Address1               string `json:"address_1"`
	Address2               string `json:"address_2,omitempty"`
	Address3               string `json:"address_3,omitempty"`
	Address4               string `json:"address_4,omitempty"`
	Postcode               string `json:"postcode"`
	City                   string `json:"city"`
	SubdivisionCode        string `json:"subdivision_code"`
	CountryCode            string `json:"country_code"`
}

type ParcelItem struct {
	Content      string  `json:"content"`
	Weight       float64 `json:"weight"`
	Height       float64 `json:"height"`
	Length       float64 `json:"length"`
	Width        float64 `json:"width"`
	CurrencyCode string  `json:"currency_code"`
	Value        float64 `json:"value"`
	Quantity     int     `json:"quantity"`
}

type Shipment struct {
	OrderID        string  `json:"order_id"`
	ServiceID      string  `json:"service_id"`
	Status         string  `json:"status"`
	ProviderStatus string `json:"provider_status,omitempty"`
	ProviderStatusCode *int `json:"provider_status_code,omitempty"`
	TrackingStatus string `json:"tracking_status,omitempty"`
	TrackingStatusCode *int `json:"tracking_status_code,omitempty"`
	ProviderEventAt *time.Time `json:"provider_event_at,omitempty"`
	ShipmentNumber string  `json:"shipment_number,omitempty"`
	OrderNumber    string  `json:"order_number,omitempty"`
	TrackingNumber string  `json:"tracking_number,omitempty"`
	TrackingURL    string  `json:"tracking_url,omitempty"`
	AWBNumber      string  `json:"awb_number,omitempty"`
	AWBURL         string  `json:"awb_url,omitempty"`
	Currency       string  `json:"currency,omitempty"`
	Amount         float64 `json:"amount,omitempty"`
	Error          string  `json:"error,omitempty"`
}

// SendOrder upserts one committed SMS order snapshot into the WMS order store.
func (c *Client) SendOrder(ctx context.Context, order OrderSnapshot) error {
	var result struct {
		Accepted bool   `json:"accepted"`
		OrderID  string `json:"order_id"`
	}
	if err := c.request(ctx, http.MethodPost, "/api/integrations/orders", order, &result); err != nil {
		return err
	}
	if !result.Accepted {
		return errors.New("WMS did not confirm the order snapshot")
	}
	return nil
}

// GetShipment reads the EasyParcel result after a WMS admin pushes the order.
// SMS can call it after shipment actions or from its existing order refresh job.
func (c *Client) GetShipment(ctx context.Context, orderID string) (Shipment, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return Shipment{}, errors.New("SMS order ID is required")
	}
	var result struct {
		Shipment Shipment `json:"shipment"`
	}
	path := "/api/integrations/orders/" + url.PathEscape(orderID) + "/shipment"
	if err := c.request(ctx, http.MethodGet, path, nil, &result); err != nil {
		return Shipment{}, err
	}
	return result.Shipment, nil
}

func (c *Client) request(ctx context.Context, method, path string, payload, output any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode WMS request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create WMS request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call WMS API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var detail struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&detail)
		if detail.Error != "" {
			return fmt.Errorf("WMS API returned HTTP %d: %s", resp.StatusCode, detail.Error)
		}
		return fmt.Errorf("WMS API returned HTTP %d", resp.StatusCode)
	}
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(output); err != nil {
		return fmt.Errorf("decode WMS response: %w", err)
	}
	return nil
}

func isLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
