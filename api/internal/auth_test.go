package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActorHeadersFromBrowserAreIgnored(t *testing.T) {
	handler := NewHandler(NewMemoryStore(), nil, nil, nil, "")
	req := httptest.NewRequest("GET", "/api/orders", nil)
	req.Header.Set("X-WMS-Actor-ID", "intruder")
	req.Header.Set("X-WMS-Actor-Name", "Intruder")
	req.Header.Set("X-WMS-Actor-Role", "warehouse_admin")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("faked actor headers must not sign in; got %d", res.Code)
	}
}

func TestLoginSessionAndLogout(t *testing.T) {
	store := NewMemoryStore()
	hash, err := HashPassword("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveStaffUser(context.Background(), StaffUser{Username: "Admin", DisplayName: "Admin", PasswordHash: hash, Role: "warehouse_admin"}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(store, nil, nil, nil, "")

	// Wrong password is refused.
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"nope"}`)))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: got %d", res.Code)
	}

	// Right password (username in any case) sets a session cookie.
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"correct-horse"}`)))
	if res.Code != http.StatusOK {
		t.Fatalf("login: got %d %s", res.Code, res.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range res.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly {
		t.Fatal("expected an HttpOnly session cookie")
	}

	// The cookie gives access, and the session reports who signed in.
	req := httptest.NewRequest("GET", "/api/session", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if !strings.Contains(res.Body.String(), `"authenticated":true`) || !strings.Contains(res.Body.String(), `"Admin"`) {
		t.Fatalf("session: %s", res.Body.String())
	}

	// After logout the same cookie no longer works.
	req = httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest("GET", "/api/orders", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: got %d", res.Code)
	}
}

func TestLoginLockoutAfterRepeatedFailures(t *testing.T) {
	handler := NewHandler(NewMemoryStore(), nil, nil, nil, "")
	var code int
	for i := 0; i < maxFailedLogins+1; i++ {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"x","password":"y"}`)))
		code = res.Code
	}
	if code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after %d failures, got %d", maxFailedLogins, code)
	}
}
