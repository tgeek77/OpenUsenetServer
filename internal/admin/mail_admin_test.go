package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"openusenet/internal/auth"
	"openusenet/internal/config"
	"openusenet/internal/store"
)

func TestMailSettingsRoundTrip(t *testing.T) {
	st := store.NewMemory()
	cfg := config.Defaults()
	cfg.Mail.Host = "smtp.example"
	cfg.Mail.From = "news@example.org"
	cfg.Mail.Password = "from-config"
	cfg.Mail.Security = "plain"
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
	if !bytes.Contains(rr.Body.Bytes(), []byte("Mail server")) {
		t.Fatal("admin page missing mail panel")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/mail", nil)
	req.AddCookie(adminCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || bytes.Contains(rr.Body.Bytes(), []byte("from-config")) {
		t.Fatalf("get %d %s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"host":"smtp.example"`)) || !bytes.Contains(rr.Body.Bytes(), []byte(`"saved":false`)) || !bytes.Contains(rr.Body.Bytes(), []byte(`"password_set":true`)) {
		t.Fatalf("config fallback %s", rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/mail", bytes.NewBufferString(`{"host":"127.0.0.1","port":25,"security":"plain","from":"News <news@example.org>","username":"news","password":"s3cret-mail"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || bytes.Contains(rr.Body.Bytes(), []byte("s3cret-mail")) {
		t.Fatalf("put %d %s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"saved":true`)) || !bytes.Contains(rr.Body.Bytes(), []byte(`"password_set":true`)) {
		t.Fatalf("saved view %s", rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/mail", bytes.NewBufferString(`{"host":"127.0.0.1","port":25,"security":"plain","from":"news@example.org","username":"news","password":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	saved, ok, err := st.GetMailSettings(context.Background())
	if err != nil || !ok || saved.Password != "s3cret-mail" {
		t.Fatalf("password not kept: ok=%v %+v %v", ok, saved, err)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/mail", bytes.NewBufferString(`{"host":"127.0.0.1","port":25,"security":"nope","from":"news@example.org"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad security %d %s", rr.Code, rr.Body.String())
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
	if rr.Code != 200 || len(rr.Result().Cookies()) == 0 {
		t.Fatal(rr.Body.String())
	}
	readerCookie := rr.Result().Cookies()[0]
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/mail", nil)
	req.AddCookie(readerCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "admin required") {
		t.Fatalf("reader %d %s", rr.Code, rr.Body.String())
	}
}
