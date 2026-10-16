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

func TestReaderSubscribeOverviewPost(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	if err := st.EnsureGroup(ctx, "local.test", "Local", "y"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Server.Hostname = "news-a"
	h := New(cfg, st, nil, nil, nil, nil, nil).Handler()

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

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/reader/search?q=hi+there", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"subject":"hello"`)) {
		t.Fatalf("search: %s", rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/reader/stats", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"populated_groups"`)) {
		t.Fatalf("stats: %s", rr.Body.String())
	}

	_ = auth.SessionCookie
}

func TestReaderDecodesEncodedSubjectAndQuotedPrintableBody(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	if err := st.EnsureGroup(ctx, "cn.comp", "CN", "y"); err != nil {
		t.Fatal(err)
	}
	subj := "=?utf-8?q?=5BS=5D_=E5=AD=98=E5=9C=A8=E6=AF=94=E5=9C=B0=E7=90=83?= =?utf-8?q?=E6=9B=B4=E5=AE=9C=E5=B1=85=E7=9A=84=E6=98=9F=E7=90=83?="
	headers := "Subject: " + subj + "\r\nFrom: Mobot <mobot@fakemail.com>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n"
	body := "[S] =E5=AD=98=E5=9C=A8=E6=AF=94=E5=9C=B0=E7=90=83\r\n"
	if _, err := st.Post(ctx, headers, body, "<qp@test>", subj, "Mobot <mobot@fakemail.com>",
		"Thu, 15 Oct 2026 23:40:25 -0000", "", "news.test", len(body), 1, []string{"cn.comp"}, false); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Server.Hostname = "news-a"
	h := New(cfg, st, nil, nil, nil, nil, nil).Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"username":"alice","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	cookie := rr.Result().Cookies()[0]

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/reader/groups/cn.comp/overview", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`[S] 存在比地球更宜居的星球`)) {
		t.Fatalf("overview subject: %s", rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/reader/groups/cn.comp/article/1", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`[S] 存在比地球`)) {
		t.Fatalf("article body: %s", rr.Body.String())
	}
	if bytes.Contains(rr.Body.Bytes(), []byte(`=E5=AD=98`)) {
		t.Fatalf("body still quoted-printable: %s", rr.Body.String())
	}
}

func TestReaderStatsNonAdmin(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	cfg := config.Defaults()
	cfg.Server.Hostname = "news-a"
	h := New(cfg, st, nil, nil, nil, nil, nil).Handler()

	rr := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"username":"admin","password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/setup", body)
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}

	hash, err := auth.HashPassword("userpass")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, store.User{
		Username: "bob", PasswordHash: hash, Role: store.RoleUser, CanPost: true,
	}); err != nil {
		t.Fatal(err)
	}

	rr = httptest.NewRecorder()
	body = bytes.NewBufferString(`{"username":"bob","password":"userpass"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/login", body)
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	cookie := rr.Result().Cookies()[0]

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/reader/stats", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("ops stats should be admin-only, got %d %s", rr.Code, rr.Body.String())
	}
}
