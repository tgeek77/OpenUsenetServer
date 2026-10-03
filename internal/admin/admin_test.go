package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"openusenet/internal/auth"
	"openusenet/internal/config"
	"openusenet/internal/store"
)

func TestSetupLoginAndAddGroup(t *testing.T) {
	st := store.NewMemory()
	if err := st.EnsureGroup(context.Background(), "local.test", "Local", "y"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Server.Hostname = "news-a"
	cfg.Peers = []config.Peer{{Host: "127.0.0.1", Port: 1}}
	h := New(cfg, st, nil, nil, nil, nil, nil).Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != 200 || !bytes.Contains(rr.Body.Bytes(), []byte("OpenUsenet")) {
		t.Fatalf("page %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	body := bytes.NewBufferString(`{"username":"admin","password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/setup", body)
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	cookie := rr.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("expected session cookie")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.AddCookie(cookie[0])
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var stj map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &stj); err != nil {
		t.Fatal(err)
	}
	if stj["hostname"] != "news-a" {
		t.Fatalf("%v", stj)
	}

	rr = httptest.NewRecorder()
	body = bytes.NewBufferString(`{"name":"alt.test.local","description":"x","status":"y"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/groups", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie[0])
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	g, err := st.GetGroup(context.Background(), "alt.test.local")
	if err != nil || g == nil {
		t.Fatalf("%v %v", g, err)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/groups?busy=0", nil)
	req.AddCookie(cookie[0])
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"name":"alt.test.local"`)) {
		t.Fatalf("want lowercase json keys, got %s", rr.Body.String())
	}

	hash, _ := auth.HashPassword("secret")
	if _, err := st.CreateUser(context.Background(), store.User{
		Username: "dup", PasswordHash: hash, Role: store.RoleUser, CanPost: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRememberMeAndPasswordChange(t *testing.T) {
	st := store.NewMemory()
	cfg := config.Defaults()
	h := New(cfg, st, nil, nil, nil, nil, nil).Handler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}

	hash, err := auth.HashPassword("temp-pass")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(context.Background(), store.User{
		Username: "newbie", PasswordHash: hash, Role: store.RoleUser, CanPost: true, MustChangePassword: true,
	}); err != nil {
		t.Fatal(err)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(`{"username":"newbie","password":"temp-pass","remember":true}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	cookies := rr.Result().Cookies()
	if len(cookies) == 0 || cookies[0].MaxAge < 29*24*3600 {
		t.Fatalf("remember cookie %#v", cookies)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.AddCookie(cookies[0])
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status before password change %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/me/password", bytes.NewBufferString(`{"current_password":"temp-pass","new_password":"chosen-pass"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookies[0])
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	next := rr.Result().Cookies()
	if len(next) == 0 {
		t.Fatal("expected refreshed cookie")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(next[0])
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || !bytes.Contains(rr.Body.Bytes(), []byte(`"must_change_password":false`)) {
		t.Fatalf("me %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/me", bytes.NewBufferString(`{"display_name":"New User","email":"new@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(next[0])
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || !bytes.Contains(rr.Body.Bytes(), []byte(`"email":"new@example.com"`)) {
		t.Fatalf("profile %d %s", rr.Code, rr.Body.String())
	}
}
