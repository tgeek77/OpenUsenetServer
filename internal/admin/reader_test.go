package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openusenet/openusenet/internal/auth"
	"github.com/openusenet/openusenet/internal/config"
	"github.com/openusenet/openusenet/internal/store"
)

func TestReaderSubscribeOverviewPost(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	if err := st.EnsureGroup(ctx, "local.test", "Local", "y"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Server.Hostname = "news-a"
	h := New(cfg, st, nil, nil, nil, nil).Handler()

	rr := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"username":"alice","password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/setup", body)
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	cookie := rr.Result().Cookies()[0]

	rr = httptest.NewRecorder()
	body = bytes.NewBufferString(`{"group":"local.test"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/reader/subscriptions", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}

	rr = httptest.NewRecorder()
	body = bytes.NewBufferString(`{"newsgroups":"local.test","subject":"hello","from":"alice@news-a","body":"hi there\n"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/reader/post", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var posted map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &posted); err != nil {
		t.Fatal(err)
	}
	if posted["message_id"] == nil {
		t.Fatalf("%v", posted)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/reader/groups/local.test/overview", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"subject":"hello"`)) {
		t.Fatalf("%s", rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/reader/groups/local.test/article/1", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("hi there")) {
		t.Fatalf("%s", rr.Body.String())
	}

	_ = auth.SessionCookie
}
