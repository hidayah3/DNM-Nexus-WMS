package internal

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDevOriginsOnlyWhenEnabled(t *testing.T) {
	handler := NewHandler(NewMemoryStore(), nil, nil, nil, "")
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Origin", "http://localhost:5173")

	t.Setenv("WMS_ALLOW_DEV_ORIGINS", "")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("dev origin must not be trusted by default, got %q", got)
	}

	t.Setenv("WMS_ALLOW_DEV_ORIGINS", "true")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("dev origin should be trusted when enabled, got %q", got)
	}
}

func TestSafeErrorHidesInternalDetails(t *testing.T) {
	res := httptest.NewRecorder()
	writeSafeError(res, errors.New(`ERROR: relation "wms_inventory_lot" does not exist (SQLSTATE 42P01)`), "could not record the stock receipt", "test")
	if res.Code != http.StatusInternalServerError || strings.Contains(res.Body.String(), "wms_inventory_lot") {
		t.Fatalf("internal error leaked: %d %s", res.Code, res.Body.String())
	}
	res = httptest.NewRecorder()
	writeSafeError(res, UserError("reduction exceeds the selected batch quantity"), "generic", "test")
	if res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), "reduction exceeds") {
		t.Fatalf("business message should be shown: %d %s", res.Code, res.Body.String())
	}
}

func TestStatusEndpointsNeedSignIn(t *testing.T) {
	handler := NewHandler(NewMemoryStore(), nil, nil, nil, "")
	for _, path := range []string{"/api/easyparcel/status", "/api/inventory/sync-status"} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest("GET", path, nil))
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s without sign-in: got %d", path, res.Code)
		}
	}
}
