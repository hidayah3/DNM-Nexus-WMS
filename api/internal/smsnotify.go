package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SMSShipmentUpdate is the compact shipment state sent back to the SMS order
// system. SMS can map shipment_status to its own order-status vocabulary.
type SMSShipmentUpdate struct {
	OrderID            string     `json:"order_id"`
	ServiceID          string     `json:"service_id,omitempty"`
	ShipmentStatus     string     `json:"shipment_status"`
	ProviderStatus     string     `json:"provider_status,omitempty"`
	ProviderStatusCode *int       `json:"provider_status_code,omitempty"`
	TrackingStatus     string     `json:"tracking_status,omitempty"`
	TrackingStatusCode *int       `json:"tracking_status_code,omitempty"`
	ProviderEventAt    *time.Time `json:"provider_event_at,omitempty"`
	ShipmentNumber     string     `json:"shipment_number,omitempty"`
	TrackingNumber     string     `json:"tracking_number,omitempty"`
	TrackingURL        string     `json:"tracking_url,omitempty"`
	AWBNumber          string     `json:"awb_number,omitempty"`
	AWBURL             string     `json:"awb_url,omitempty"`
	AWBStatus          string     `json:"awb_status"`
}

type SMSShipmentUpdateEvent struct {
	Event     string              `json:"event"`
	SentAt    time.Time           `json:"sent_at"`
	Shipments []SMSShipmentUpdate `json:"shipments"`
}

// SMSShipmentNotifier calls a server-side SMS endpoint configured by the
// operator. It never exposes the callback secret to the React application.
type SMSShipmentNotifier struct {
	endpoint string
	token    string
	client   *http.Client
}

type SMSInventorySnapshot struct {
	SKU         string `json:"sku"`
	ProductName string `json:"product_name"`
	Quantity    int64  `json:"quantity"`
	Revision    int64  `json:"revision"`
}

type SMSInventoryEvent struct {
	Event     string                 `json:"event"`
	SentAt    time.Time              `json:"sent_at"`
	Inventory []SMSInventorySnapshot `json:"inventory"`
}

// SMSInventoryNotifier sends committed stock snapshots. The transactional
// outbox worker retries failures and the idempotency key prevents duplicate
// application at the SMS receiver.
type SMSInventoryNotifier struct {
	endpoint string
	token    string
	client   *http.Client
}

func NewSMSInventoryNotifier(endpoint, token string) (*SMSInventoryNotifier, error) {
	endpoint, token = strings.TrimSpace(endpoint), strings.TrimSpace(token)
	if endpoint == "" && token == "" {
		return nil, nil
	}
	if endpoint == "" || token == "" {
		return nil, errors.New("both SMS_INVENTORY_SYNC_URL and SMS_INVENTORY_SYNC_TOKEN are required")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("SMS inventory sync URL is invalid")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		local := strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())
		if u.Scheme != "http" || !local {
			return nil, errors.New("SMS inventory sync URL must use HTTPS (HTTP is allowed only for localhost)")
		}
	}
	return &SMSInventoryNotifier{endpoint: u.String(), token: token, client: &http.Client{Timeout: 15 * time.Second}}, nil
}

func (n *SMSInventoryNotifier) Send(ctx context.Context, event InventorySyncEvent) error {
	if n == nil {
		return errors.New("SMS inventory sync endpoint is not configured")
	}
	payload := SMSInventoryEvent{Event: "wms.inventory.updated", SentAt: time.Now().UTC(), Inventory: []SMSInventorySnapshot{{SKU: event.SKU, ProductName: event.Name, Quantity: event.Quantity, Revision: event.ID}}}
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("could not encode inventory snapshot")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("could not prepare SMS inventory sync")
	}
	req.Header.Set("Authorization", "Bearer "+n.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("wms-inventory-%d", event.ID))
	req.Header.Set("User-Agent", "DNM-Nexus-WMS/1.0")
	resp, err := n.client.Do(req)
	if err != nil {
		return errors.New("could not reach the configured SMS inventory endpoint")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("SMS inventory endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func NewSMSShipmentNotifier(endpoint, token string) (*SMSShipmentNotifier, error) {
	endpoint, token = strings.TrimSpace(endpoint), strings.TrimSpace(token)
	if endpoint == "" && token == "" {
		return nil, nil
	}
	if endpoint == "" || token == "" {
		return nil, errors.New("both SMS_SHIPMENT_UPDATE_URL and SMS_SHIPMENT_UPDATE_TOKEN are required")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("SMS shipment update URL is invalid")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		local := strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())
		if u.Scheme != "http" || !local {
			return nil, errors.New("SMS shipment update URL must use HTTPS (HTTP is allowed only for localhost)")
		}
	}
	return &SMSShipmentNotifier{endpoint: u.String(), token: token, client: &http.Client{Timeout: 15 * time.Second}}, nil
}

func (n *SMSShipmentNotifier) Send(ctx context.Context, shipments []SMSShipmentUpdate) (int, error) {
	if n == nil {
		return 0, errors.New("SMS shipment update endpoint is not configured; set SMS_SHIPMENT_UPDATE_URL and SMS_SHIPMENT_UPDATE_TOKEN")
	}
	if len(shipments) == 0 {
		return 0, errors.New("there are no EasyParcel shipments to send to SMS")
	}
	payload := SMSShipmentUpdateEvent{Event: "wms.shipment.updated", SentAt: time.Now().UTC(), Shipments: shipments}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, errors.New("could not encode shipment updates")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, errors.New("could not prepare SMS shipment update")
	}
	req.Header.Set("Authorization", "Bearer "+n.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "DNM-Nexus-WMS/1.0")
	resp, err := n.client.Do(req)
	if err != nil {
		return 0, errors.New("could not reach the configured SMS shipment update endpoint")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("SMS shipment update endpoint returned HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}
