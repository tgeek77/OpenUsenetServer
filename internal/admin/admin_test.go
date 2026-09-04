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
