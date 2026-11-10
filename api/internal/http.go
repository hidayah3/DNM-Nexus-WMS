package internal

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

type Handler struct {
	store                  Store
	parcel                 EasyParcel
	oauth                  *EasyParcelOAuth
	smsNotifier            *SMSShipmentNotifier
	smsInventoryNotifier   *SMSInventoryNotifier
	easyParcelWebhookToken string
	mux                    *http.ServeMux
}

func NewHandler(store Store, parcel EasyParcel, oauth *EasyParcelOAuth, smsNotifier *SMSShipmentNotifier, easyParcelWebhookToken string, inventoryNotifiers ...*SMSInventoryNotifier) http.Handler {
	if parcel == nil {
		parcel = EasyParcelUnavailable{}
	}
	h := &Handler{store: store, parcel: parcel, oauth: oauth, smsNotifier: smsNotifier, easyParcelWebhookToken: strings.TrimSpace(easyParcelWebhookToken), mux: http.NewServeMux()}
	if len(inventoryNotifiers) > 0 {
		h.smsInventoryNotifier = inventoryNotifiers[0]
	}
	h.mux.HandleFunc("GET /health", h.health)
	h.mux.HandleFunc("GET /api/orders", h.listOrders)
	h.mux.HandleFunc("POST /api/orders", h.createManualOrder)
	h.mux.HandleFunc("POST /api/orders/{id}/courier-quotes", h.courierQuotes)
	h.mux.HandleFunc("POST /api/integrations/orders", h.receiveSMSOrder)
	h.mux.HandleFunc("GET /api/integrations/orders/{id}/shipment", h.smsShipmentStatus)
	h.mux.HandleFunc("GET /api/orders/{id}/commercial-invoice", h.commercialInvoiceData)
	h.mux.HandleFunc("GET /api/session", h.session)
	h.mux.HandleFunc("GET /api/inventory", h.inventory)
	h.mux.HandleFunc("POST /api/inventory/products", h.createInventoryProduct)
	h.mux.HandleFunc("POST /api/inventory/receipts", h.receiveInventoryStock)
	h.mux.HandleFunc("POST /api/inventory/adjustments", h.adjustStock)
	h.mux.HandleFunc("GET /api/inventory/movements", h.stockMovements)
	h.mux.HandleFunc("GET /api/inventory/report", h.inventoryReport)
	h.mux.HandleFunc("GET /api/inventory/sync-status", h.inventorySyncStatus)
	h.mux.HandleFunc("GET /api/shipping-rates", h.shippingRates)
	h.mux.HandleFunc("PUT /api/shipping-rates", h.saveShippingRate)
	h.mux.HandleFunc("PATCH /api/orders/{id}/fulfilment", h.fulfilment)
	h.mux.HandleFunc("PATCH /api/orders/{id}/status", h.updateOrderStatus)
	h.mux.HandleFunc("POST /api/delivery-batches/push", h.push)
	h.mux.HandleFunc("POST /api/delivery-batches/awb", h.awb)
	h.mux.HandleFunc("POST /api/delivery-batches/sync-sms", h.syncSMSShipmentUpdates)
	h.mux.HandleFunc("GET /api/easyparcel/connect", h.easyParcelConnect)
	h.mux.HandleFunc("GET /api/easyparcel/callback", h.easyParcelCallback)
	h.mux.HandleFunc("GET /api/easyparcel/status", h.easyParcelStatus)
	h.mux.HandleFunc("DELETE /api/easyparcel/connection", h.easyParcelDisconnect)
	h.mux.HandleFunc("POST /api/easyparcel/webhook/{token}", h.easyParcelWebhook)
	h.mux.HandleFunc("DELETE /api/orders/{id}", h.deleteOrder)
	return h.cors(h.mux)
}

func (h *Handler) easyParcelWebhook(w http.ResponseWriter, r *http.Request) {
	if len(h.easyParcelWebhookToken) < 32 || len(r.PathValue("token")) != len(h.easyParcelWebhookToken) ||
		subtle.ConstantTimeCompare([]byte(r.PathValue("token")), []byte(h.easyParcelWebhookToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid EasyParcel webhook endpoint token")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "webhook payload is too large")
		return
	}
	var input struct {
		Topic                    string `json:"topic"`
		ShipmentNumber           string `json:"shipment_number"`
		UUID                     string `json:"uuid"`
		EventDate                string `json:"event_date"`
		Timestamp                string `json:"timestamp"`
		ShipmentStatus           string `json:"shipment_status"`
		ShipmentStatusCode       *int   `json:"shipment_status_code"`
		LatestShipmentStatusCode *int   `json:"latest_shipment_status_code"`
		AWBNumber                string `json:"awb_number"`
		AWBURL                   string `json:"awb_url"`
		TrackingNumber           string `json:"tracking_number"`
		TrackingURL              string `json:"tracking_url"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid EasyParcel webhook JSON")
		return
	}
	input.Topic = strings.TrimSpace(input.Topic)
	input.ShipmentNumber = strings.TrimSpace(input.ShipmentNumber)
	if input.ShipmentNumber == "" {
		writeError(w, http.StatusBadRequest, "shipment_number is required")
		return
	}
	switch input.Topic {
	case "shipment.status.update", "shipment.awb.update", "shipment.tracking.update", "shipment.created":
	default:
		writeError(w, http.StatusBadRequest, "unsupported EasyParcel webhook topic")
		return
	}
	trackingStatus, trackingCode, trackingAt := extractTrackingStatus(raw)
	if input.LatestShipmentStatusCode != nil {
		trackingCode = input.LatestShipmentStatusCode
	}
	if input.Topic == "shipment.tracking.update" && input.ShipmentStatusCode != nil && trackingCode == nil {
		trackingCode = input.ShipmentStatusCode
	}
	occurredAt := parseEasyParcelEventTime(input.Timestamp, input.EventDate)
	if !trackingAt.IsZero() && trackingAt.After(occurredAt) {
		occurredAt = trackingAt
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	providerStatus := ""
	var providerCode *int
	if input.Topic == "shipment.status.update" {
		providerStatus, providerCode = strings.TrimSpace(input.ShipmentStatus), input.ShipmentStatusCode
	}
	if input.Topic == "shipment.created" {
		providerStatus = "created"
	}
	eventKey := strings.TrimSpace(input.UUID)
	if eventKey == "" {
		digest := sha256.Sum256(raw)
		eventKey = hex.EncodeToString(digest[:])
	}
	eventKey = input.Topic + ":" + eventKey
	matched, err := h.store.SaveEasyParcelWebhook(r.Context(), EasyParcelWebhook{
		EventKey: eventKey, Topic: input.Topic, ShipmentNumber: input.ShipmentNumber,
		ProviderStatus: providerStatus, ProviderStatusCode: providerCode,
		TrackingStatus: trackingStatus, TrackingStatusCode: trackingCode,
		TrackingNumber: firstNonEmpty(strings.TrimSpace(input.TrackingNumber), strings.TrimSpace(input.AWBNumber)),
		TrackingURL:    strings.TrimSpace(input.TrackingURL), AWBNumber: strings.TrimSpace(input.AWBNumber),
		AWBURL: strings.TrimSpace(input.AWBURL), OccurredAt: occurredAt,
	}, raw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save EasyParcel webhook; provider may retry")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "shipment_matched": matched})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func parseEasyParcelEventTime(values ...string) time.Time {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Time{}
}

func extractTrackingStatus(raw []byte) (string, *int, time.Time) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var root any
	if decoder.Decode(&root) != nil {
		return "", nil, time.Time{}
	}
	type candidate struct {
		status string
		code   *int
		at     time.Time
	}
	var candidates []candidate
	var visit func(any, time.Time, *int)
	visit = func(value any, inheritedAt time.Time, inheritedCode *int) {
		switch node := value.(type) {
		case map[string]any:
			at := inheritedAt
			for _, key := range []string{"timestamp", "event_date", "date"} {
				if value, ok := node[key].(string); ok {
					if parsed := parseEasyParcelEventTime(value); !parsed.IsZero() {
						at = parsed
						break
					}
				}
			}
			code := inheritedCode
			for _, key := range []string{"latest_shipment_status_code", "shipment_status_code", "status_code"} {
				switch value := node[key].(type) {
				case json.Number:
					if parsed, err := value.Int64(); err == nil {
						v := int(parsed)
						code = &v
					}
				case float64:
					v := int(value)
					code = &v
				}
			}
			for key, child := range node {
				if key == "tracking_status" {
					if status, ok := child.(string); ok && strings.TrimSpace(status) != "" {
						candidates = append(candidates, candidate{strings.TrimSpace(status), code, at})
					}
				}
				visit(child, at, code)
			}
		case []any:
			for _, child := range node {
				visit(child, inheritedAt, inheritedCode)
			}
		}
	}
	visit(root, time.Time{}, nil)
	if len(candidates) == 0 {
		return "", nil, time.Time{}
	}
	latest := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.at.After(latest.at) || (latest.at.IsZero() && !candidate.at.IsZero()) {
			latest = candidate
		}
	}
	return latest.status, latest.code, latest.at
}

func integrationAuthorized(r *http.Request) bool {
	secret := strings.TrimSpace(os.Getenv("SMS_WMS_INGEST_TOKEN"))
	if secret == "" {
		return false
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) < 8 || !strings.EqualFold(auth[:7], "Bearer ") {
		return false
	}
	provided := strings.TrimSpace(auth[7:])
	return len(provided) == len(secret) && subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}

// SMS pushes order snapshots to WMS; this service never reads the SMS database directly.
func (h *Handler) receiveSMSOrder(w http.ResponseWriter, r *http.Request) {
	if !integrationAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "valid SMS-to-WMS integration authorization is required")
		return
	}
	var order IncomingOrder
	if !decode(w, r, &order) {
		return
	}
	order.ID = strings.TrimSpace(order.ID)
	order.Customer = strings.TrimSpace(order.Customer)
	order.Items = strings.TrimSpace(order.Items)
	order.Status = strings.TrimSpace(order.Status)
	if order.ID == "" || order.Date.IsZero() || order.Customer == "" || order.Items == "" || order.Status == "" {
		writeError(w, http.StatusBadRequest, "id, date, customer, items, and status are required")
		return
	}
	order.Shipment.OrderID = order.ID
	if order.Shipment.Reference == "" {
		order.Shipment.Reference = order.ID
	}
	if err := h.store.UpsertIncomingOrder(r.Context(), order); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the SMS order snapshot")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "order_id": order.ID})
}

// SMS polls this endpoint to retrieve the shipment state WMS saved from EasyParcel.
func (h *Handler) smsShipmentStatus(w http.ResponseWriter, r *http.Request) {
	if !integrationAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "valid SMS-to-WMS integration authorization is required")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	records, err := h.store.GetShipmentRecords(r.Context(), []string{id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load shipment state")
		return
	}
	if len(records) == 0 {
		writeError(w, http.StatusNotFound, "shipment has not been created for this order")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"shipment": records[0]})
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request) {
	actor := actorFromRequest(r)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": actor.ID != "" && actor.Name != "" && (actor.Role == "admin" || actor.Role == "warehouse_admin"), "actor": actor})
}

func (h *Handler) inventory(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	items, err := h.store.ListInventory(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "inventory storage is not configured")
		return
	}
	lots, err := h.store.ListInventoryLots(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "inventory batch storage is not configured; apply migration 002")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"products": items, "lots": lots})
}

func (h *Handler) inventorySyncStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"configured": h.smsInventoryNotifier != nil})
}

func (h *Handler) shippingRates(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	rates, err := h.store.ListShippingRates(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "shipping rate storage is not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rates": rates})
}

func (h *Handler) saveShippingRate(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	var rate ShippingRate
	if !decode(w, r, &rate) {
		return
	}
	rate.Region = strings.ToLower(strings.TrimSpace(rate.Region))
	if (rate.Region != "west" && rate.Region != "east") || rate.WeightKG < 1 || rate.WeightKG > 10 || rate.CourierCharge < 0 || rate.CourierTax < 0 || rate.PackagingCost < 0 || math.IsNaN(rate.CourierCharge) || math.IsInf(rate.CourierCharge, 0) || math.IsNaN(rate.CourierTax) || math.IsInf(rate.CourierTax, 0) || math.IsNaN(rate.PackagingCost) || math.IsInf(rate.PackagingCost, 0) {
		writeError(w, http.StatusBadRequest, "region, weight, and non-negative finite charges are required")
		return
	}
	saved, err := h.store.SaveShippingRate(r.Context(), rate, actorFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the shipping rate")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rate": saved})
}

func (h *Handler) createInventoryProduct(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	var p InventoryProduct
	if !decode(w, r, &p) {
		return
	}
	if strings.TrimSpace(p.SKU) == "" || strings.TrimSpace(p.Name) == "" {
		writeError(w, http.StatusBadRequest, "SKU and product name are required")
		return
	}
	if err := h.store.CreateInventoryProduct(r.Context(), p); err != nil {
		writeError(w, http.StatusConflict, "could not create product; the SKU may already exist")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"product": p})
}

func (h *Handler) receiveInventoryStock(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	var receipt InventoryReceipt
	if !decode(w, r, &receipt) {
		return
	}
	receipt.SKU = strings.TrimSpace(receipt.SKU)
	receipt.Name = strings.TrimSpace(receipt.Name)
	receipt.BatchNumber = strings.TrimSpace(receipt.BatchNumber)
	receipt.Reason = strings.TrimSpace(receipt.Reason)
	date, err := time.Parse("2006-01-02", strings.TrimSpace(receipt.ExpiryDate))
	if receipt.SKU == "" || receipt.Name == "" || receipt.BatchNumber == "" || receipt.Reason == "" || receipt.Quantity <= 0 || err != nil {
		writeError(w, http.StatusBadRequest, "product, SKU, batch number, valid expiry date, positive quantity and reason are required")
		return
	}
	receipt.ExpiryDate = date.Format("2006-01-02")
	product, lot, movement, err := h.store.ReceiveInventoryStock(r.Context(), receipt, actorFromRequest(r))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"product": product, "lot": lot, "movement": movement, "sms_sync": "queued"})
}

func (h *Handler) adjustStock(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	actor := actorFromRequest(r)
	var a StockAdjustment
	if !decode(w, r, &a) {
		return
	}
	a.Type = strings.ToLower(strings.TrimSpace(a.Type))
	a.Reason = strings.TrimSpace(a.Reason)
	if a.LotID <= 0 || (a.Type != "add" && a.Type != "reduce") || a.Quantity <= 0 || a.Reason == "" {
		writeError(w, http.StatusBadRequest, "select a stock batch, add or reduce, a positive quantity, and a reason")
		return
	}
	p, lot, m, err := h.store.AdjustStock(r.Context(), a, actor)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "product not found")
		} else {
			writeError(w, http.StatusConflict, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"product": p, "lot": lot, "movement": m, "sms_sync": "queued"})
}

func (h *Handler) inventoryReport(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	lots, err := h.store.InventoryWorkbookRows(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "inventory report storage is not configured")
		return
	}
	data, err := inventoryWorkbook(lots)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the Excel report")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=inventory-batches.xlsx")
	_, _ = w.Write(data)
}

func (h *Handler) stockMovements(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	from, err := time.Parse("2006-01", month)
	if err != nil {
		writeError(w, http.StatusBadRequest, "month must use YYYY-MM")
		return
	}
	to := from.AddDate(0, 1, 0)
	rows, err := h.store.ListStockMovements(r.Context(), from, to)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "inventory movement storage is not configured")
		return
	}
	switch r.URL.Query().Get("format") {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=stock-movement-%s.csv", month))
		w.Write([]byte{0xef, 0xbb, 0xbf})
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"Date and time", "SKU", "Product", "Batch number", "Expiry date", "Movement", "Quantity changed", "Stock after", "Reason", "Account ID", "Account name"})
		for _, m := range rows {
			_ = cw.Write([]string{m.CreatedAt.Format(time.RFC3339), m.SKU, m.ProductName, m.BatchNumber, m.ExpiryDate, m.Type, fmt.Sprint(m.Quantity), fmt.Sprint(m.StockAfter), m.Reason, m.ActorID, m.ActorName})
		}
		cw.Flush()
		return
	case "xlsx":
		data, err := movementWorkbook(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not create the Excel report")
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=stock-movement-%s.xlsx", month))
		_, _ = w.Write(data)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"month": month, "movements": rows})
}

func actorFromRequest(r *http.Request) AuthenticatedActor {
	return AuthenticatedActor{ID: strings.TrimSpace(r.Header.Get("X-WMS-Actor-ID")), Name: strings.TrimSpace(r.Header.Get("X-WMS-Actor-Name")), Role: strings.ToLower(strings.TrimSpace(r.Header.Get("X-WMS-Actor-Role")))}
}
func requireActor(w http.ResponseWriter, r *http.Request) bool {
	a := actorFromRequest(r)
	if a.ID == "" || a.Name == "" {
		writeError(w, http.StatusUnauthorized, "a signed-in account is required to record this action")
		return false
	}
	if a.Role != "admin" && a.Role != "warehouse_admin" {
		writeError(w, http.StatusForbidden, "a warehouse administrator account is required for this action")
		return false
	}
	return true
}

func (h *Handler) listOrders(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	from, err := queryDate(r, "from")
	if err != nil {
		writeError(w, http.StatusBadRequest, "from must use YYYY-MM-DD")
		return
	}
	to, err := queryDate(r, "to")
	if err != nil {
		writeError(w, http.StatusBadRequest, "to must use YYYY-MM-DD")
		return
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		writeError(w, http.StatusBadRequest, "from must be on or before to")
		return
	}
	orders, err := h.store.ListOrders(r.Context(), from, to)
	if err != nil {
		log.Printf("list orders: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load orders")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": orders})
}

// createManualOrder lets an authenticated warehouse user enter an actual order
// directly when the SMS order feed has not yet been connected.
func (h *Handler) createManualOrder(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var order IncomingOrder
	if !decode(w, r, &order) {
		return
	}
	order.ID = strings.TrimSpace(order.ID)
	order.Customer = strings.TrimSpace(order.Customer)
	order.Items = strings.TrimSpace(order.Items)
	order.Status = strings.TrimSpace(order.Status)
	if order.ID == "" || order.Date.IsZero() || order.Customer == "" || order.Items == "" {
		writeError(w, http.StatusBadRequest, "order ID, order date, customer name and order items are required")
		return
	}
	if order.Status == "" {
		order.Status = "Ready to fulfil"
	}
	order.Shipment.OrderID = order.ID
	order.Shipment.Reference = order.ID
	if order.Shipment.CollectionDate == "" {
		order.Shipment.CollectionDate = time.Now().Format("2006-01-02")
	}
	if err := validateDraft(order.Shipment); err != nil {
		writeError(w, http.StatusBadRequest, "complete the EasyParcel sender, recipient and parcel details: "+err.Error())
		return
	}
	if err := h.store.CreateManualOrder(r.Context(), order, actorFromRequest(r)); err != nil {
		if errors.Is(err, ErrDuplicateOrder) {
			writeError(w, http.StatusConflict, "that order ID is already in WMS; check the order list before trying again")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create order")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"created": true, "source": "manual", "order_id": order.ID})
}

// courierQuotes asks EasyParcel for currently available services and prices
// using the order's saved sender, receiver and parcel details. It does not
// create a shipment or debit the EasyParcel wallet.
func (h *Handler) courierQuotes(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "order id is required")
		return
	}
	draft, err := h.store.GetShipmentDraft(r.Context(), id)
	if err != nil {
		writeOrderDataError(w, err)
		return
	}
	quotes, err := h.parcel.Quotes(r.Context(), draft)
	if err != nil {
		writeProviderError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"order_id": id, "quotes": quotes})
}

func (h *Handler) deleteOrder(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "order id is required")
		return
	}
	if err := h.store.DeleteOrder(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "order not found")
		case errors.Is(err, ErrShipmentAlreadySubmitted):
			writeError(w, http.StatusConflict, "this order already has an EasyParcel shipment and cannot be deleted")
		default:
			writeError(w, http.StatusInternalServerError, "could not delete the order")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "order_id": id})
}

func (h *Handler) fulfilment(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var f Fulfilment
	if !decode(w, r, &f) {
		return
	}
	f.Method = strings.ToLower(f.Method)
	if f.Method != "delivery" && f.Method != "self_collection" {
		writeError(w, http.StatusBadRequest, "method must be delivery or self_collection")
		return
	}
	if f.Method == "self_collection" {
		f.CourierCode = ""
	}
	if err := h.store.SaveFulfilment(r.Context(), id, f); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "order not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not save fulfilment")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"order_id": id, "fulfilment": f})
}

func (h *Handler) push(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	var input struct {
		Orders []PushOrder `json:"orders"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.Orders) == 0 {
		writeError(w, http.StatusBadRequest, "at least one delivery order is required")
		return
	}
	seen := map[string]bool{}
	drafts := make([]ShipmentDraft, 0, len(input.Orders))
	for _, order := range input.Orders {
		order.OrderID, order.ServiceID = strings.TrimSpace(order.OrderID), strings.TrimSpace(order.ServiceID)
		if order.OrderID == "" || order.ServiceID == "" {
			writeError(w, http.StatusBadRequest, "each order needs an order_id and configured courier service_id")
			return
		}
		if seen[order.OrderID] {
			writeError(w, http.StatusBadRequest, "an order may only be submitted once per batch")
			return
		}
		seen[order.OrderID] = true
		draft, err := h.store.GetShipmentDraft(r.Context(), order.OrderID)
		if err != nil {
			writeOrderDataError(w, err)
			return
		}
		if err := h.store.SaveFulfilment(r.Context(), order.OrderID, Fulfilment{Method: "delivery", CourierCode: order.ServiceID}); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save the selected courier service")
			return
		}
		drafts = append(drafts, draft)
	}
	if err := h.store.ReserveShipmentPush(r.Context(), input.Orders); err != nil {
		if errors.Is(err, ErrShipmentAlreadySubmitted) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "could not reserve this shipment batch")
		}
		return
	}
	submissions := make([]ShipmentSubmission, len(input.Orders))
	for i, order := range input.Orders {
		submissions[i] = ShipmentSubmission{Draft: drafts[i], ServiceID: order.ServiceID}
	}
	results, err := h.parcel.PushOrders(r.Context(), submissions)
	if err != nil {
		if errors.Is(err, ErrNoEasyParcelConnection) || errors.Is(err, ErrProviderNotConfigured) {
			for _, order := range input.Orders {
				_ = h.store.SaveShipmentResult(r.Context(), PushResult{OrderID: order.OrderID, Status: "failed", Error: err.Error()}, order.ServiceID)
			}
			writeProviderError(w, err)
			return
		}
		if errors.Is(err, ErrShipmentOutcomeUnknown) {
			_ = h.store.MarkShipmentUnknown(r.Context(), orderIDs(input.Orders))
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		for _, order := range input.Orders {
			_ = h.store.SaveShipmentResult(r.Context(), PushResult{OrderID: order.OrderID, Status: "failed", Error: safeProviderError(err.Error(), "EasyParcel rejected this request")}, order.ServiceID)
		}
		writeProviderError(w, err)
		return
	}
	awbPending := make([]ShipmentRecord, 0, len(results))
	for i, result := range results {
		serviceID := ""
		if i < len(input.Orders) {
			serviceID = input.Orders[i].ServiceID
		}
		if result.Status == "outcome_unknown" {
			_ = h.store.MarkShipmentUnknown(r.Context(), []string{result.OrderID})
			continue
		}
		if result.Status != "created" {
			result.Status = "failed"
		}
		if err := h.store.SaveShipmentResult(r.Context(), result, serviceID); err != nil {
			_ = h.store.MarkShipmentUnknown(r.Context(), orderIDs(input.Orders))
			writeError(w, http.StatusInternalServerError, "EasyParcel responded, but WMS could not save every shipment result; reconcile the EasyParcel account before retrying")
			return
		}
		if result.Status == "created" && result.ShipmentNumber != "" && result.AWBURL == "" {
			awbPending = append(awbPending, ShipmentRecord{OrderID: result.OrderID, ShipmentNumber: result.ShipmentNumber})
		}
	}

	// EasyParcel may create the shipment before its AWB document is ready. Try
	// to retrieve it immediately; the Print AWB route remains available to retry.
	if len(awbPending) > 0 {
		if awbs, err := h.parcel.AWBDetails(r.Context(), awbPending); err == nil {
			for _, awb := range awbs {
				if awb.AWBURL != "" || awb.AWBNumber != "" || awb.TrackingURL != "" {
					if err := h.store.SaveAWBDetails(r.Context(), awb); err != nil {
						continue
					}
				}
				for i := range results {
					if results[i].OrderID != awb.OrderID {
						continue
					}
					if awb.TrackingNumber != "" {
						results[i].TrackingNumber = awb.TrackingNumber
					}
					if awb.TrackingURL != "" {
						results[i].TrackingURL = awb.TrackingURL
					}
					if awb.AWBNumber != "" {
						results[i].AWBNumber = awb.AWBNumber
					}
					if awb.AWBURL != "" {
						results[i].AWBURL = awb.AWBURL
					}
				}
			}
		}
	}

	awbPendingOrderIDs := make([]string, 0, len(awbPending))
	for _, candidate := range awbPending {
		for _, result := range results {
			if result.OrderID == candidate.OrderID && result.AWBURL == "" {
				awbPendingOrderIDs = append(awbPendingOrderIDs, candidate.OrderID)
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "awb_pending_order_ids": awbPendingOrderIDs})
}

func (h *Handler) awb(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	var input struct {
		OrderIDs []string `json:"order_ids"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.OrderIDs) == 0 {
		writeError(w, http.StatusBadRequest, "at least one order_id is required")
		return
	}
	for i := range input.OrderIDs {
		input.OrderIDs[i] = strings.TrimSpace(input.OrderIDs[i])
	}
	records, err := h.store.GetShipmentRecords(r.Context(), input.OrderIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load saved EasyParcel shipments")
		return
	}
	results, err := h.parcel.AWBDetails(r.Context(), records)
	if err != nil {
		writeProviderError(w, err)
		return
	}
	for _, result := range results {
		if result.AWBURL != "" || result.AWBNumber != "" || result.TrackingURL != "" {
			if err := h.store.SaveAWBDetails(r.Context(), result); err != nil {
				writeError(w, http.StatusInternalServerError, "could not save the Air Waybill link")
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"awbs": results})
}

// syncSMSShipmentUpdates sends the selected, successfully booked shipments to
// SMS through its configured server-to-server callback. It refreshes AWB data
// first when possible so SMS receives the newest saved tracking information.
func (h *Handler) syncSMSShipmentUpdates(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	if h.smsNotifier == nil {
		writeError(w, http.StatusServiceUnavailable, "SMS shipment update endpoint is not configured; set SMS_SHIPMENT_UPDATE_URL and SMS_SHIPMENT_UPDATE_TOKEN")
		return
	}
	var input struct {
		OrderIDs []string `json:"order_ids"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.OrderIDs) == 0 || len(input.OrderIDs) > 100 {
		writeError(w, http.StatusBadRequest, "provide between 1 and 100 order_ids")
		return
	}
	seen := make(map[string]bool, len(input.OrderIDs))
	for i := range input.OrderIDs {
		input.OrderIDs[i] = strings.TrimSpace(input.OrderIDs[i])
		if input.OrderIDs[i] == "" || seen[input.OrderIDs[i]] {
			writeError(w, http.StatusBadRequest, "order_ids must be non-empty and unique")
			return
		}
		seen[input.OrderIDs[i]] = true
	}
	records, err := h.store.GetShipmentRecords(r.Context(), input.OrderIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load saved EasyParcel shipments")
		return
	}
	eligible := make([]ShipmentRecord, 0, len(records))
	for _, record := range records {
		if record.ShipmentNumber != "" {
			eligible = append(eligible, record)
		}
	}
	if len(eligible) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "none of the selected orders has a successfully created EasyParcel shipment")
		return
	}

	// Shipment details includes AWB and tracking links. If EasyParcel has not
	// made the AWB available yet, retain the last saved values and still send
	// the current WMS shipment status to SMS.
	if awbs, refreshErr := h.parcel.AWBDetails(r.Context(), eligible); refreshErr == nil {
		byID := make(map[string]AWBResult, len(awbs))
		for _, awb := range awbs {
			byID[awb.OrderID] = awb
		}
		for i := range eligible {
			awb, ok := byID[eligible[i].OrderID]
			if !ok {
				continue
			}
			if awb.AWBURL != "" || awb.AWBNumber != "" || awb.TrackingURL != "" {
				if err := h.store.SaveAWBDetails(r.Context(), awb); err != nil {
					writeError(w, http.StatusInternalServerError, "could not save refreshed tracking and AWB details")
					return
				}
			}
			if awb.TrackingNumber != "" {
				eligible[i].TrackingNumber = awb.TrackingNumber
			}
			if awb.TrackingURL != "" {
				eligible[i].TrackingURL = awb.TrackingURL
			}
			if awb.AWBNumber != "" {
				eligible[i].AWBNumber = awb.AWBNumber
			}
			if awb.AWBURL != "" {
				eligible[i].AWBURL = awb.AWBURL
			}
		}
	}

	updates := make([]SMSShipmentUpdate, 0, len(eligible))
	for _, record := range eligible {
		awbStatus := "pending"
		if record.AWBURL != "" {
			awbStatus = "ready"
		}
		updates = append(updates, SMSShipmentUpdate{
			OrderID: record.OrderID, ServiceID: record.ServiceID, ShipmentStatus: record.Status,
			ProviderStatus: record.ProviderStatus, ProviderStatusCode: record.ProviderStatusCode,
			TrackingStatus: record.TrackingStatus, TrackingStatusCode: record.TrackingStatusCode,
			ProviderEventAt: record.ProviderEventAt,
			ShipmentNumber:  record.ShipmentNumber, TrackingNumber: record.TrackingNumber,
			TrackingURL: record.TrackingURL, AWBNumber: record.AWBNumber, AWBURL: record.AWBURL,
			AWBStatus: awbStatus,
		})
	}
	statusCode, err := h.smsNotifier.Send(r.Context(), updates)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"sent": len(updates), "sms_http_status": statusCode})
}

func (h *Handler) easyParcelConnect(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	if h.oauth == nil {
		writeError(w, http.StatusServiceUnavailable, ErrProviderNotConfigured.Error())
		return
	}
	h.oauth.Connect(w, r)
}
func (h *Handler) easyParcelCallback(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil {
		writeError(w, http.StatusServiceUnavailable, ErrProviderNotConfigured.Error())
		return
	}
	h.oauth.Callback(w, r)
}
func (h *Handler) easyParcelStatus(w http.ResponseWriter, r *http.Request) {
	environment := strings.TrimSpace(strings.ToLower(os.Getenv("EASYPARCEL_ENVIRONMENT")))
	if environment != "sandbox" && environment != "production" {
		environment = "unknown"
	}
	if h.oauth == nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "connected": false, "environment": environment, "webhook_configured": len(h.easyParcelWebhookToken) >= 32})
		return
	}
	tokens, err := h.oauth.store.Load(r.Context())
	connected := err == nil && (tokens.AccessToken != "" || tokens.RefreshToken != "")
	var expiresAt any
	if connected {
		expiresAt = tokens.ExpiresAt
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "connected": connected, "environment": environment, "expires_at": expiresAt, "webhook_configured": len(h.easyParcelWebhookToken) >= 32})
}
func (h *Handler) easyParcelDisconnect(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	if h.oauth == nil {
		writeError(w, http.StatusServiceUnavailable, ErrProviderNotConfigured.Error())
		return
	}
	h.oauth.Disconnect(w, r)
}

func queryDate(r *http.Request, key string) (time.Time, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02", v)
}
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func writeProviderError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, ErrProviderNotConfigured) || errors.Is(err, ErrNoEasyParcelConnection) {
		status = http.StatusServiceUnavailable
	}
	writeError(w, status, err.Error())
}
func writeOrderDataError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "order not found")
	} else {
		writeError(w, http.StatusPreconditionFailed, err.Error())
	}
}
func orderIDs(orders []PushOrder) []string {
	out := make([]string, 0, len(orders))
	for _, o := range orders {
		out = append(out, o.OrderID)
	}
	return out
}
func (h *Handler) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := map[string]bool{"http://localhost:5173": true, "http://127.0.0.1:5173": true, "http://localhost:4173": true, "http://127.0.0.1:4173": true}
		for _, v := range strings.Split(os.Getenv("WMS_ALLOWED_ORIGINS"), ",") {
			v = strings.TrimSpace(v)
			if v != "" {
				allowed[v] = true
			}
		}
		if allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, PATCH, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
