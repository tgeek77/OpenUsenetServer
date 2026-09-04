package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"openusenet/internal/auth"
	"openusenet/internal/config"
	"openusenet/internal/store"
)

func TestArchiveImportAPI(t *testing.T) {
	st := store.NewMemory()
	cfg := config.Defaults()
	cfg.Server.Hostname = "news-a"
	p := New(cfg, st, nil, nil, nil, nil, nil)
	h := p.Handler()

	rr := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"username":"admin","password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/setup", body)
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	cookie := rr.Result().Cookies()[0]

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("files", "local.test.mbox")
	if err != nil {
		t.Fatal(err)
	}
	mbox := `From a@b Mon Jan 1 00:00:00 2020
From: a@b
Newsgroups: local.test
Subject: import ui
Message-ID: <admin-import@test>
Date: Mon, 1 Jan 2020 00:00:00 +0000

hello from admin import
`
	if _, err := io.WriteString(fw, mbox); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/archive/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var out struct {
		Job store.ArchiveJob `json:"job"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Job.Kind != "import" || out.Job.ID == "" {
		t.Fatalf("%+v", out.Job)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		p.jobsMu.Lock()
		j := p.jobs[out.Job.ID]
		status := j.Status
		imported := j.Imported
		errMsg := j.Error
		p.jobsMu.Unlock()
		if status == "done" {
			if imported != 1 {
				t.Fatalf("imported=%d", imported)
			}
			break
		}
		if status == "error" {
			t.Fatal(errMsg)
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for import job")
		}
		time.Sleep(20 * time.Millisecond)
	}

	art, err := st.GetByMsgID(context.Background(), "<admin-import@test>")
	if err != nil || art == nil {
		t.Fatalf("get: %v %+v", err, art)
	}
	if !strings.Contains(art.Body, "hello from admin import") {
		t.Fatalf("body=%q", art.Body)
	}

	// Non-admin cannot import.
	hash, _ := auth.HashPassword("pw")
	if _, err := st.CreateUser(context.Background(), store.User{
		Username: "reader", PasswordHash: hash, Role: store.RoleUser, CanPost: true,
	}); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	login := bytes.NewBufferString(`{"username":"reader","password":"pw"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/login", login)
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	userCookie := rr.Result().Cookies()[0]

	var buf2 bytes.Buffer
	mw2 := multipart.NewWriter(&buf2)
	fw2, _ := mw2.CreateFormFile("files", "x.mbox")
	_, _ = io.WriteString(fw2, mbox)
	_ = mw2.Close()
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/archive/import", &buf2)
	req.Header.Set("Content-Type", mw2.FormDataContentType())
	req.AddCookie(userCookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d %s", rr.Code, rr.Body.String())
	}
}
