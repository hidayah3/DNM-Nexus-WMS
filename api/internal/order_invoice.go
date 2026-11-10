package internal

import (
	"net/http"
	"strings"
)

// GET /api/orders/{id}/commercial-invoice returns the saved sender, receiver and
// parcel/item details that the WMS page uses to print a commercial invoice.
func (h *Handler) commercialInvoiceData(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"order_id": id, "shipment": draft})
}