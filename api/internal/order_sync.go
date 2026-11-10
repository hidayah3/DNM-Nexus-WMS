package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Orders are pulled from the K-Link order API:
//   POST <ORDER_SYNC_URL> with Authorization: <ORDER_SYNC_AUTHORIZATION>
// The response looks like {"success": true, "data": [ ...orders... ]}.
// The API has no delivery address, so staff add it in the WMS before booking a courier.

var ErrOrderSyncRunning = errors.New("an order sync is already running")
var ErrShipmentAlreadyBooked = errors.New("a shipment is already booked for this order, so its delivery details can no longer be changed")

// The order ID carries the order date, e.g. MYR-SMNIV-260929-5JPXN is 29 Sep 2026.
var orderIDDate = regexp.MustCompile(`-(\d{6})-`)

var malaysiaTime = time.FixedZone("MYT", 8*60*60)

// klinkOrder is one order as the K-Link API returns it. Only the fields the WMS uses are listed.
type klinkOrder struct {
	OrderID    string   `json:"orderId"`
	OrderCode  string   `json:"orderCode"`
	TotalPrice float64  `json:"totalPrice"`
	ClaimAt    *string  `json:"claimAt"`
	Remark     string   `json:"remark"`
	Status     string   `json:"status"`
	Currency   string   `json:"currency"`
	CreatedBy  klinkWho `json:"createdBy"`
	Member     klinkWho `json:"member"`
	Stockist   struct {
		StockistCode string `json:"stockistCode"`
	} `json:"stockist"`
	Transaction []struct {
		TransactionDetail []struct {
			Quantity      int     `json:"quantity"`
			ProductWeight float64 `json:"productWeight"`
			ProductCode   string  `json:"productCode"`
			CodeProduct   string  `json:"codeProduct"`
			ProductName   string  `json:"productName"`
			Name          string  `json:"name"`
		} `json:"transactionDetail"`
	} `json:"transaction"`
}

type klinkWho struct {
	MemberID   string `json:"memberId"`
	MemberName string `json:"memberName"`
}

type klinkResponse struct {
	Code    string            `json:"code"`
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Data    []json.RawMessage `json:"data"`
}

// SyncedOrder is an order from the API, already translated into WMS fields.
type SyncedOrder struct {
	ID          string
	Date        time.Time
	Customer    string
	Items       string
	Status      string
	ParcelValue float64
	Currency    string
	Lines       []ParcelItem
	Raw         json.RawMessage
	// Sender is the configured warehouse address (WAREHOUSE_SENDER_*); empty = copy from the last order.
	Sender Address
	// AllowCreate is false for orders whose status is not ready for the warehouse
	// (e.g. not yet paid). Those only update orders the WMS already has.
	AllowCreate bool
}

type SyncOutcome string

const (
	SyncCreated SyncOutcome = "created"
	SyncUpdated SyncOutcome = "updated"
	SyncSkipped SyncOutcome = "skipped"
)

type OrderSyncResult struct {
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Received   int        `json:"received"`
	Created    int        `json:"created"`
	Updated    int        `json:"updated"`
	Skipped    int        `json:"skipped"`
	Failed     int        `json:"failed"`
	Error      string     `json:"error,omitempty"`
}

type OrderSyncConfig struct {
	URL           string
	Authorization string
	// Statuses that create new orders in the WMS. Empty means "PAID".
	ImportStatuses []string
	// Only keep orders for this stockist (warehouse) code. Empty keeps all.
	Stockist string
	// Sender is put on every new order's shipment. Leave Address1 empty to copy it from the last order instead.
	Sender Address
}

type OrderSyncer struct {
	cfg      OrderSyncConfig
	statuses map[string]bool
	store    Store
	client   *http.Client

	running sync.Mutex
	mu      sync.Mutex
	last    *OrderSyncResult
}

// NewOrderSyncer returns nil, nil when the URL is not set (sync turned off).
func NewOrderSyncer(cfg OrderSyncConfig, store Store) (*OrderSyncer, error) {
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.Authorization = strings.TrimSpace(cfg.Authorization)
	cfg.Stockist = strings.TrimSpace(cfg.Stockist)
	if cfg.URL == "" {
		return nil, nil
	}
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" {
		return nil, errors.New("ORDER_SYNC_URL is not a valid URL")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
	if u.Scheme != "https" && !local {
		return nil, errors.New("ORDER_SYNC_URL must use https")
	}
	if cfg.Authorization == "" {
		return nil, errors.New("ORDER_SYNC_AUTHORIZATION is required when ORDER_SYNC_URL is set")
	}
	// Accept either "Basic abc123" or just "abc123".
	if !strings.Contains(cfg.Authorization, " ") {
		cfg.Authorization = "Basic " + cfg.Authorization
	}
	statuses := map[string]bool{}
	for _, s := range cfg.ImportStatuses {
		if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
			statuses[s] = true
		}
	}
	if len(statuses) == 0 {
		statuses["PAID"] = true
	}
	return &OrderSyncer{cfg: cfg, statuses: statuses, store: store, client: &http.Client{Timeout: 45 * time.Second}}, nil
}

// Last returns the result of the most recent sync, or nil if none has run yet.
func (s *OrderSyncer) Last() *OrderSyncResult {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return nil
	}
	last := *s.last
	return &last
}

// Run pulls orders from the API once and saves them. Only one sync runs at a time.
func (s *OrderSyncer) Run(ctx context.Context) (OrderSyncResult, error) {
	if !s.running.TryLock() {
		return OrderSyncResult{}, ErrOrderSyncRunning
	}
	defer s.running.Unlock()

	result := OrderSyncResult{StartedAt: time.Now().UTC()}
	err := s.run(ctx, &result)
	if err != nil {
		result.Error = err.Error()
	}
	finished := time.Now().UTC()
	result.FinishedAt = &finished
	s.mu.Lock()
	s.last = &result
	s.mu.Unlock()
	return result, err
}

func (s *OrderSyncer) run(ctx context.Context, result *OrderSyncResult) error {
	raw, err := s.fetch(ctx)
	if err != nil {
		return err
	}
	// Save the raw response before anything else. If the API only returns each
	// order once, this is the copy we recover from if saving an order fails.
	runID, err := s.store.StartOrderSyncRun(ctx, raw)
	if err != nil {
		return fmt.Errorf("could not record the order sync: %w", err)
	}
	defer func() {
		if err := s.store.FinishOrderSyncRun(context.WithoutCancel(ctx), runID, *result); err != nil {
			log.Printf("order sync %d: could not save result: %v", runID, err)
		}
	}()

	var envelope klinkResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return errors.New("the order API returned something that is not valid JSON")
	}
	if !envelope.Success {
		msg := strings.TrimSpace(envelope.Message)
		if msg == "" {
			msg = "no message"
		}
		return fmt.Errorf("the order API reported a failure: %s", msg)
	}
	result.Received = len(envelope.Data)
	for _, item := range envelope.Data {
		order, err := s.translate(item)
		if err != nil {
			result.Failed++
			log.Printf("order sync %d: %v", runID, err)
			continue
		}
		if order == nil {
			result.Skipped++
			continue
		}
		outcome, err := s.store.UpsertSyncedOrder(ctx, *order)
		if err != nil {
			result.Failed++
			log.Printf("order sync %d: could not save order %s: %v", runID, order.ID, err)
			continue
		}
		switch outcome {
		case SyncCreated:
			result.Created++
		case SyncUpdated:
			result.Updated++
		default:
			result.Skipped++
		}
	}
	if result.Failed > 0 {
		return fmt.Errorf("%d order(s) could not be saved; see the API log (sync run %d)", result.Failed, runID)
	}
	return nil
}

func (s *OrderSyncer) fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.URL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", s.cfg.Authorization)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, errors.New("could not reach the order API")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, errors.New("could not read the order API response")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("the order API refused the login (HTTP %d); check ORDER_SYNC_AUTHORIZATION", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("the order API returned HTTP %d", resp.StatusCode)
	}
	return raw, nil
}

// translate turns one API order into WMS fields. It returns nil when the order
// belongs to another stockist.
func (s *OrderSyncer) translate(raw json.RawMessage) (*SyncedOrder, error) {
	var in klinkOrder
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, errors.New("an order in the response could not be read")
	}
	id := strings.TrimSpace(in.OrderID)
	if id == "" {
		return nil, errors.New("an order in the response has no orderId")
	}
	if s.cfg.Stockist != "" && !strings.EqualFold(strings.TrimSpace(in.Stockist.StockistCode), s.cfg.Stockist) {
		return nil, nil
	}

	status := strings.ToUpper(strings.TrimSpace(in.Status))
	if status == "" {
		return nil, fmt.Errorf("order %s has no status", id)
	}
	wmsStatus := status
	if strings.Contains(status, "CANCEL") || strings.Contains(status, "VOID") || strings.Contains(status, "REFUND") {
		wmsStatus = OrderStatusCancelled
	}

	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "MYR"
	}

	var summary []string
	var lines []ParcelItem
	totalQty := 0
	for _, t := range in.Transaction {
		for _, d := range t.TransactionDetail {
			if d.Quantity <= 0 {
				continue
			}
			code := firstFilled(d.ProductCode, d.CodeProduct)
			name := firstFilled(d.ProductName, d.Name, code)
			label := name
			if code != "" && code != name {
				label = fmt.Sprintf("%s (%s)", name, code)
			}
			summary = append(summary, fmt.Sprintf("%s × %d", label, d.Quantity))
			lines = append(lines, ParcelItem{Content: name, Weight: d.ProductWeight, Quantity: d.Quantity, CurrencyCode: currency})
			totalQty += d.Quantity
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("order %s has no products", id)
	}
	// The API gives one total price; share it across the items by quantity.
	for i := range lines {
		lines[i].Value = math.Round(in.TotalPrice*float64(lines[i].Quantity)/float64(totalQty)*100) / 100
	}

	customer := firstFilled(in.Member.MemberName, in.CreatedBy.MemberName, in.Member.MemberID, in.CreatedBy.MemberID, "Member order")

	return &SyncedOrder{
		ID:          id,
		Date:        orderDate(id, in.ClaimAt),
		Customer:    customer,
		Items:       strings.Join(summary, ", "),
		Status:      wmsStatus,
		ParcelValue: in.TotalPrice,
		Currency:    currency,
		Lines:       lines,
		Raw:         raw,
		AllowCreate: s.statuses[status],
		Sender:      s.cfg.Sender,
	}, nil
}

// orderDate reads the date from the order ID, then falls back to claimAt, then today.
// Dates are set to midday Malaysia time so they never slip to the day before in UTC.
func orderDate(id string, claimAt *string) time.Time {
	if m := orderIDDate.FindStringSubmatch(id); m != nil {
		if d, err := time.ParseInLocation("060102", m[1], malaysiaTime); err == nil {
			return d.Add(12 * time.Hour)
		}
	}
	if claimAt != nil {
		if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*claimAt)); err == nil {
			return t
		}
	}
	return time.Now()
}

func firstFilled(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// RunOrderSyncWorker pulls orders straight away and then on every interval.
func RunOrderSyncWorker(ctx context.Context, syncer *OrderSyncer, interval time.Duration) {
	if syncer == nil {
		return
	}
	if interval < time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		result, err := syncer.Run(runCtx)
		cancel()
		switch {
		case errors.Is(err, ErrOrderSyncRunning):
		case err != nil && ctx.Err() == nil:
			log.Printf("order sync failed: %v", err)
		case err == nil && (result.Created > 0 || result.Updated > 0):
			log.Printf("order sync: %d received, %d new, %d updated, %d skipped", result.Received, result.Created, result.Updated, result.Skipped)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// POST /api/orders/sync runs a sync now (the "Sync orders" button).
func (h *Handler) syncOrdersNow(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	if h.orderSyncer == nil {
		writeError(w, http.StatusServiceUnavailable, "order sync is not set up; set ORDER_SYNC_URL and ORDER_SYNC_AUTHORIZATION on the API server")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := h.orderSyncer.Run(ctx)
	if errors.Is(err, ErrOrderSyncRunning) {
		writeError(w, http.StatusConflict, "a sync is already running; try again in a moment")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err != nil && result.Created == 0 && result.Updated == 0 {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "result": result})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

// GET /api/orders/sync-status says whether sync is set up and how the last run went.
func (h *Handler) orderSyncStatus(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"configured": h.orderSyncer != nil, "last": h.orderSyncer.Last()})
}

// PUT /api/orders/{id}/shipment saves the delivery address and parcel details staff enter.
func (h *Handler) saveShipmentDetails(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "order id is required")
		return
	}
	var draft ShipmentDraft
	if !decode(w, r, &draft) {
		return
	}
	draft.OrderID = id
	draft.Reference = id
	if draft.CollectionDate == "" {
		draft.CollectionDate = time.Now().In(malaysiaTime).Format("2006-01-02")
	}
	if err := validateDraft(draft); err != nil {
		writeError(w, http.StatusBadRequest, "complete the delivery details: "+err.Error())
		return
	}
	if err := h.store.SaveShipmentDetails(r.Context(), id, draft); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "order not found")
		case errors.Is(err, ErrShipmentAlreadyBooked), errors.Is(err, ErrOrderClosed):
			writeError(w, http.StatusConflict, err.Error())
		default:
			log.Printf("save shipment details %s: %v", id, err)
			writeError(w, http.StatusInternalServerError, "could not save the delivery details")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "order_id": id})
}
