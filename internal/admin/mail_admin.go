package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"openusenet/internal/mail"
	"openusenet/internal/store"
)

func (p *Portal) mailAPI(w http.ResponseWriter, r *http.Request, _ store.User) {
	switch r.Method {
	case http.MethodGet:
		view, err := p.mailView(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPut:
		var in struct {
			Host          string `json:"host"`
			Port          int    `json:"port"`
			Username      string `json:"username"`
			Password      string `json:"password"`
			From          string `json:"from"`
			Security      string `json:"security"`
			ClearPassword bool   `json:"clear_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		cur, err := mail.Load(r.Context(), p.st, p.cfg)
		if err != nil {
			writeErr(w, err)
			return
		}
		next := mail.Settings{
			Host: in.Host, Port: in.Port, Username: in.Username,
			From: in.From, Security: in.Security,
		}
		switch {
		case in.Password != "":
			next.Password = in.Password
		case in.ClearPassword:
			next.Password = ""
		default:
			next.Password = cur.Password
		}
		next = next.Normalize()
		if next.Host != "" && strings.TrimSpace(next.From) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from address is required"})
			return
		}
		if err := next.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := p.st.SaveMailSettings(r.Context(), next); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		p.log.Printf("mail settings saved host=%s port=%d security=%s", next.Host, next.Port, next.Security)
		view, err := p.mailView(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *Portal) mailTest(w http.ResponseWriter, r *http.Request, _ store.User) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	to := strings.TrimSpace(in.To)
	if _, err := mail.Envelope(to); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "recipient must be an email address"})
		return
	}
	s, err := mail.Load(r.Context(), p.st, p.cfg)
	if err != nil {
		writeErr(w, err)
		return
	}
	if s.Host == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mail server is not configured"})
		return
	}
	host := p.cfg.Server.Hostname
	if err := mail.Send(r.Context(), s, mail.Message{
		To:      []string{to},
		Subject: "OpenUsenetServer test",
		Body:    "This is a test message from " + host + ".\r\n",
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": "sent"})
}

func (p *Portal) mailView(ctx context.Context) (map[string]any, error) {
	s, err := mail.Load(ctx, p.st, p.cfg)
	if err != nil {
		return nil, err
	}
	_, saved, err := p.st.GetMailSettings(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"host":         s.Host,
		"port":         s.Port,
		"username":     s.Username,
		"password_set": s.Password != "",
		"from":         s.From,
		"security":     s.Security,
		"saved":        saved,
	}, nil
}
