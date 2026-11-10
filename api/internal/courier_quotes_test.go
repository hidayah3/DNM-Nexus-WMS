package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type courierQuoteTestStore struct {
	Store
	draft ShipmentDraft
	err   error
}

// Any session cookie signs in as this staff member.
func (s courierQuoteTestStore) SessionUser(context.Context, string) (StaffUser, error) {
	return StaffUser{Username: "staff-7", DisplayName: "Warehouse Admin", Role: "warehouse_admin", Active: true}, nil
}

func (s courierQuoteTestStore) GetShipmentDraft(context.Context, string) (ShipmentDraft, error) {
	return s.draft, s.err
}

type courierQuoteTestProvider struct {
	EasyParcel
	quotes []CourierQuote
	err    error
}

func (p courierQuoteTestProvider) Quotes(context.Context, ShipmentDraft) ([]CourierQuote, error) {
	return p.quotes, p.err
}

func TestCourierQuotesRequiresAuthenticatedWarehouseActor(t *testing.T) {
	handler := NewHandler(courierQuoteTestStore{}, courierQuoteTestProvider{}, nil, nil, "")
	req := httptest.NewRequest("POST", "/api/orders/SO-1/courier-quotes", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, req)

	if response.Code != 401 {
		t.Fatalf("expected 401 without an authenticated actor, got %d", response.Code)
	}
}

func TestCourierQuotesReturnsEasyParcelServicesForOrder(t *testing.T) {
	want := []CourierQuote{{
		ServiceID: "EP-SERVICE-1", ServiceName: "Courier Standard", CourierName: "Courier",
		Currency: "MYR", TotalAmount: 12.34, DeliveryDuration: "2 days",
	}}
	draft := ShipmentDraft{OrderID: "SO-1", Weight: 1, Receiver: Address{Postcode: "50000"}}
	handler := NewHandler(courierQuoteTestStore{draft: draft}, courierQuoteTestProvider{quotes: want}, nil, nil, "")
	req := httptest.NewRequest("POST", "/api/orders/SO-1/courier-quotes", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test-session"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, req)

	if response.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		OrderID string         `json:"order_id"`
		Quotes  []CourierQuote `json:"quotes"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.OrderID != "SO-1" || len(body.Quotes) != 1 || body.Quotes[0].ServiceID != want[0].ServiceID {
		t.Fatalf("unexpected quote response: %#v", body)
	}
}
