package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openusenet/openusenet/internal/config"
	"github.com/openusenet/openusenet/internal/store"
)

func TestStatusAndAddGroup(t *testing.T) {
	st := store.NewMemory()
	if err := st.EnsureGroup(context.Background(), "local.test", "Local", "y"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Server.Hostname = "news-a"
	cfg.Peers = []config.Peer{{Host: "127.0.0.1", Port: 1}}
	h := New(cfg, st, nil).Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != 200 || !bytes.Contains(rr.Body.Bytes(), []byte("OpenUsenet")) {
		t.Fatalf("page %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/status", nil))
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
	body := bytes.NewBufferString(`{"name":"alt.test.local","description":"x","status":"y"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/groups", body)
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	g, err := st.GetGroup(context.Background(), "alt.test.local")
	if err != nil || g == nil {
		t.Fatalf("%v %v", g, err)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/groups?busy=0", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"name":"alt.test.local"`)) {
		t.Fatalf("want lowercase json keys, got %s", rr.Body.String())
	}
}
