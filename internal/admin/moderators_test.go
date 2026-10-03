package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"openusenet/internal/auth"
	"openusenet/internal/config"
	"openusenet/internal/mail"
	"openusenet/internal/moderate"
	"openusenet/internal/store"
)

func TestModeratorRulesAndReaderMail(t *testing.T) {
	st := store.NewMemory()
	cfg := config.Defaults()
	cfg.Server.Hostname = "news.test"
	cfg.Server.Pathhost = "news.test"
	cfg.Limits.ArtCutoffDays = 0
	if err := st.EnsureGroup(context.Background(), "misc.test.moderated", "", "m"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveMailSettings(context.Background(), mail.Settings{
		Host: "127.0.0.1", Port: 25, From: "news@news.test", Security: mail.SecurityPlain,
	}); err != nil {
		t.Fatal(err)
	}
	h := New(cfg, st, nil, nil, nil, nil, nil).Handler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	adminCookie := rr.Result().Cookies()[0]

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)
	if !bytes.Contains(rr.Body.Bytes(), []byte("Moderated groups")) || !bytes.Contains(rr.Body.Bytes(), []byte("misc-test-moderated@moderators.isc.org")) {
		t.Fatal("admin page missing moderated submission help")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/moderators", nil)
	req.AddCookie(adminCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || !bytes.Contains(rr.Body.Bytes(), []byte(moderate.DefaultAddress)) || !bytes.Contains(rr.Body.Bytes(), []byte(`"rules":[]`)) {
		t.Fatalf("list %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/moderators", bytes.NewBufferString(`{"pattern":"local.*","address":"desk@example.org"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}

	hash, err := auth.HashPassword("user-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(context.Background(), store.User{
		Username: "reader", PasswordHash: hash, Role: store.RoleUser, CanPost: true,
	}); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(`{"username":"reader","password":"user-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	readerCookie := rr.Result().Cookies()[0]
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/moderators", nil)
	req.AddCookie(readerCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("reader rules %d %s", rr.Code, rr.Body.String())
	}

	var mailed string
	restore := moderate.SetDeliver(func(_ context.Context, _ mail.Settings, to []string, raw []byte) error {
		mailed = to[0] + "\n" + string(raw)
		return nil
	})
	t.Cleanup(restore)
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/reader/post", bytes.NewBufferString(`{"from":"Poster <poster@example.org>","newsgroups":"misc.test.moderated","subject":"hello mod","body":"please post this"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(readerCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || !bytes.Contains(rr.Body.Bytes(), []byte(`"moderated":true`)) || !bytes.Contains(rr.Body.Bytes(), []byte(`"address":"misc-test-moderated@moderators.isc.org"`)) {
		t.Fatalf("reader post %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(mailed, "misc-test-moderated@moderators.isc.org") || !strings.Contains(mailed, "From: Poster <poster@example.org>") || !strings.Contains(mailed, "Subject: hello mod") {
		t.Fatalf("mailed %q", mailed)
	}
	if n, _ := st.CountArticles(context.Background()); n != 0 {
		t.Fatalf("web post stored %d articles", n)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/moderators", bytes.NewBufferString(`{"pattern":"*","address":"%s@example.org"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	rules, err := st.ListModeratorRules(context.Background())
	if err != nil || len(rules) != 2 || rules[0].Pattern != "local.*" || rules[1].Pattern != "*" {
		t.Fatalf("order %+v %v", rules, err)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/moderators", bytes.NewBufferString(`{"pattern":"local.*","address":"not-an-email"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad address %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/moderators?pattern="+url.QueryEscape("local.*"), nil)
	req.AddCookie(adminCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	rules, err = st.ListModeratorRules(context.Background())
	if err != nil || len(rules) != 1 || rules[0].Pattern != "*" {
		t.Fatalf("after delete %+v %v", rules, err)
	}
}
