package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Shaped like the real response from the K-Link order API.
const sampleOrderResponse = `{
  "code": "OK", "success": true, "message": "change data successfully",
  "data": [
    {
      "orderId": "MYR-SMNIV-260928-B4LEK", "orderCode": "MYR-SMNIV-260928-CRHXF",
      "totalPrice": 2848, "totalProduct": 2, "claimAt": null, "status": "PAID", "currency": "",
      "createdBy": {"memberId": "KAKMUNGSHOP", "memberName": "NIK FATIHAH BINTI NIK OTHMAN"},
      "member": {"memberId": "KAKMUNGSHOP", "memberName": "NIK FATIHAH BINTI NIK OTHMAN"},
      "stockist": {"stockistCode": "WHMY"},
      "transaction": [{"transactionDetail": [
        {"quantity": 2, "productWeight": 0, "productCode": "NEXLIFE001", "codeProduct": "NEXLIFE001", "productName": "NEXLIFE"}
      ]}],
      "oderShipping": {"awbNumber": ""}
    },
    {
      "orderId": "MYR-SMNIV-260929-HE8NR", "totalPrice": 1424, "status": "PENDING",
      "member": {"memberName": ""}, "createdBy": {"memberName": ""},
      "stockist": {"stockistCode": "WHMY"},
      "transaction": [{"transactionDetail": [{"quantity": 1, "productCode": "NEXLIFE001", "productName": "NEXLIFE"}]}]
    },
    {
      "orderId": "MYR-SMNIV-260929-OTHER", "totalPrice": 10, "status": "PAID",
      "stockist": {"stockistCode": "ELSEWHERE"},
      "transaction": [{"transactionDetail": [{"quantity": 1, "productCode": "X", "productName": "X"}]}]
    }
  ]
}`

func TestOrderSyncTranslatesAPIOrders(t *testing.T) {
	var gotAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		w.Write([]byte(sampleOrderResponse))
	}))
	defer api.Close()

	store := NewMemoryStore()
	syncer, err := NewOrderSyncer(OrderSyncConfig{URL: api.URL, Authorization: "abc123", Stockist: "WHMY"}, store)
	if err != nil {
		t.Fatal(err)
	}
	// httptest serves plain http on 127.0.0.1, which the https rule allows for local hosts.
	result, err := syncer.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotAuth != "Basic abc123" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Basic abc123")
	}
	// PAID order created; PENDING order is not new-importable; other stockist ignored.
	if result.Received != 3 || result.Created != 1 || result.Skipped != 2 || result.Failed != 0 {
		t.Fatalf("result = %+v", result)
	}
	orders, _ := store.ListOrders(context.Background(), time.Time{}, time.Time{})
	if len(orders) != 1 {
		t.Fatalf("orders = %d, want 1", len(orders))
	}
	o := orders[0]
	if o.ID != "MYR-SMNIV-260928-B4LEK" || o.Customer != "NIK FATIHAH BINTI NIK OTHMAN" || o.Items != "NEXLIFE (NEXLIFE001) × 2" || o.Status != "PAID" {
		t.Errorf("order = %+v", o)
	}
	if got := o.Date.UTC().Format("2006-01-02"); got != "2026-09-28" {
		t.Errorf("date = %s, want 2026-09-28", got)
	}
	if !o.NeedsDetails {
		t.Error("a synced order should need delivery details")
	}

	// A second sync updates the same order instead of duplicating it.
	result, err = syncer.Run(context.Background())
	if err != nil || result.Created != 0 || result.Updated != 1 {
		t.Fatalf("second run = %+v, %v", result, err)
	}
}

func TestOrderSyncTranslateFallbacks(t *testing.T) {
	s, _ := NewOrderSyncer(OrderSyncConfig{URL: "https://example.com/sync", Authorization: "Basic x"}, NewMemoryStore())
	raw := json.RawMessage(`{"orderId":"NO-DATE-HERE","totalPrice":300,"status":"CANCELLED","claimAt":"2026-09-04T11:27:09.514162Z",
		"member":{"memberId":"M1"},"transaction":[{"transactionDetail":[
		{"quantity":1,"productCode":"A","productName":"Alpha"},{"quantity":2,"productCode":"B","productName":"Beta"}]}]}`)
	o, err := s.translate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if o.Customer != "M1" || o.Status != OrderStatusCancelled || o.AllowCreate {
		t.Errorf("order = %+v", o)
	}
	if got := o.Date.UTC().Format("2006-01-02"); got != "2026-09-04" {
		t.Errorf("date = %s, want claimAt date", got)
	}
	if o.Lines[0].Value != 100 || o.Lines[1].Value != 200 || o.Currency != "MYR" {
		t.Errorf("lines = %+v currency = %s", o.Lines, o.Currency)
	}
}

func TestOrderSyncRejectsPlainHTTP(t *testing.T) {
	if _, err := NewOrderSyncer(OrderSyncConfig{URL: "http://example.com/sync", Authorization: "Basic x"}, NewMemoryStore()); err == nil {
		t.Error("expected an error for a non-https URL")
	}
}
