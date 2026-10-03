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

func TestPortalStatsChrome(t *testing.T) {
	statsAt := bytes.Index(indexHTML, []byte(`<main id="stats"`))
	adminAt := bytes.Index(indexHTML, []byte(`<main id="admin"`))
	statsNav := bytes.Index(indexHTML, []byte(`data-mode="stats"`))
	adminGate := bytes.Index(indexHTML, []byte(`if (me.is_admin)`))
	if statsAt < 0 || adminAt < statsAt || statsNav < 0 || adminGate < statsNav {
		t.Fatal("stats page should be available to every signed-in user")
	}
	stats := indexHTML[statsAt:adminAt]
	for _, want := range []string{"Hall of shame", "Group search", "Recent articles", "Newsgroup", "Sender", `id="gsearch"`, `id="arts"`, `id="shame"`, "rows10"} {
		if !bytes.Contains(stats, []byte(want)) {
			t.Fatalf("stats missing %s", want)
		}
	}
	if bytes.Index(stats, []byte("Hall of shame")) < bytes.Index(stats, []byte("Recent articles")) {
		t.Fatal("hall of shame should be at the bottom of stats")
	}
	adminHTML := indexHTML[adminAt:]
	if bytes.Contains(adminHTML, []byte("Recent articles")) || bytes.Contains(adminHTML, []byte(`id="groups"`)) {
		t.Fatal("admin still has the group list or recent articles")
	}
	for _, theme := range []string{"Yaru", "Yaru Dark", "Adwaita", "Adwaita Dark", "Breeze", "Breeze Dark"} {
		if !bytes.Contains(indexHTML, []byte(theme)) {
			t.Fatalf("missing theme %s", theme)
		}
	}
	if !bytes.Contains(indexHTML, []byte("scrollbox")) {
		t.Fatal("missing scroll containers")
	}
	ruler := []byte("----+----1----+----2----+----3----+----4----+----5----+----6----+----7----+----8")
	if len(ruler) != 80 || !bytes.Contains(indexHTML, ruler) || !bytes.Contains(indexHTML, []byte(`wrap="off"`)) || !bytes.Contains(indexHTML, []byte("function wrapLongLines")) {
		t.Fatal("compose box should show an 80-column ruler and hard-wrap")
	}
}

func TestRecentArticlesIncludeGroupAndSender(t *testing.T) {
	if got := groupsFromXref("news.example local.test:3 alt.test:9"); got != "local.test, alt.test" {
		t.Fatalf("xref groups: %q", got)
	}
	if got := groupsFromXref("misc.test:1"); got != "misc.test" {
		t.Fatalf("bare xref: %q", got)
	}

	st := store.NewMemory()
	ctx := context.Background()
	if err := st.EnsureGroup(ctx, "local.test", "Local", "y"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Post(ctx, "From: Ada <ada@example.org>\r\n", "body\n", "<a@news>", "hello", "Ada <ada@example.org>", "Sat, 03 Oct 2026 12:00:00 +0000", "", "news.example", 10, 1, []string{"local.test"}, false); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	h := New(cfg, st, nil, nil, nil, nil, nil).Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	cookie := rr.Result().Cookies()[0]

	hash, err := auth.HashPassword("userpass")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, store.User{Username: "bob", PasswordHash: hash, Role: store.RoleUser, CanPost: true}); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(`{"username":"bob","password":"userpass"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	userCookie := rr.Result().Cookies()[0]

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/articles?limit=25", nil)
	req.AddCookie(userCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || !bytes.Contains(rr.Body.Bytes(), []byte(`"newsgroups":"local.test"`)) || !bytes.Contains(rr.Body.Bytes(), []byte(`"sender":"Ada `)) {
		t.Fatalf("articles %d %s", rr.Code, rr.Body.String())
	}
	_ = cookie
}
