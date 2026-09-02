package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/openusenet/openusenet/internal/inpaths"
	"github.com/openusenet/openusenet/internal/store"
)

func (p *Portal) inpathsStatus() map[string]any {
	st := map[string]any{
		"enabled":  p.cfg.InpathsEnabled(),
		"dir":      p.cfg.InpathsDir(),
		"schedule": p.cfg.Inpaths.Schedule,
		"mailto":   p.cfg.InpathsMailTo(),
		"smtp":     p.cfg.Inpaths.Report.SMTPHost != "",
	}
	if !p.cfg.InpathsEnabled() {
		return st
	}
	if p.inpaths != nil {
		st["pending"] = p.inpaths.PendingArticles()
	}
	dumps, _ := inpaths.ListDumps(p.cfg.InpathsDir())
	st["dumps"] = len(dumps)
	return st
}

func (p *Portal) inpathsAPI(w http.ResponseWriter, r *http.Request, _ store.User) {
	if !p.cfg.InpathsEnabled() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "inpaths not enabled — set inpaths.enabled in config.yml"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		report, _ := p.inpathsReportBody(false)
		writeJSON(w, http.StatusOK, map[string]any{
			"status": p.inpathsStatus(),
			"report": report,
		})
	case http.MethodPost:
		var in struct {
			Action string `json:"action"` // flush, report, send
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		switch in.Action {
		case "flush":
			if p.inpaths == nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path logger not running"})
				return
			}
			path, err := p.inpaths.Flush()
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": path, "status": p.inpathsStatus()})
		case "report":
			body, err := p.inpathsReportBody(true)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"report": body, "status": p.inpathsStatus()})
		case "send":
			body, err := p.inpathsReportBody(true)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if p.cfg.Inpaths.Report.SMTPHost == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error":  "smtp_host not configured — use openusenet inpaths report and mail manually, or set inpaths.report.smtp_host",
					"report": body,
				})
				return
			}
			if err := inpaths.SendReport(body, p.cfg.Server.Pathhost, p.cfg.InpathsMailTo(), p.cfg.Inpaths.Report.MailCC, inpaths.MailOpts{
				Host: p.cfg.Inpaths.Report.SMTPHost, Port: p.cfg.Inpaths.Report.SMTPPort,
				Username: p.cfg.Inpaths.Report.SMTPUser, Password: p.cfg.Inpaths.Report.SMTPPass,
				From:     p.cfg.Inpaths.Report.From,
			}); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": "sent", "status": p.inpathsStatus()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be flush, report, or send"})
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *Portal) inpathsReportBody(flushPending bool) (string, error) {
	if flushPending && p.inpaths != nil && p.inpaths.PendingArticles() > 0 {
		if _, err := p.inpaths.Flush(); err != nil {
			return "", err
		}
	}
	st, err := inpaths.LoadDumps(p.cfg.InpathsDir(), 32*24*time.Hour)
	if err != nil {
		return "", err
	}
	if p.inpaths != nil {
		st.Merge(p.inpaths.Snapshot())
	}
	return st.Report(p.cfg.Server.Pathhost)
}
