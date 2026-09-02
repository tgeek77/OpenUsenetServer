package admin

import (
	"context"
	_ "embed"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/openusenet/openusenet/internal/article"
	"github.com/openusenet/openusenet/internal/config"
	"github.com/openusenet/openusenet/internal/feed"
	"github.com/openusenet/openusenet/internal/isc"
	"github.com/openusenet/openusenet/internal/nntp"
	"github.com/openusenet/openusenet/internal/store"
)

//go:embed index.html
var indexHTML []byte

type Portal struct {
	cfg     config.Config
	st      store.Store
	feeder  *feed.Feeder
	started time.Time
}

func New(cfg config.Config, st store.Store, feeder *feed.Feeder) *Portal {
	return &Portal{cfg: cfg, st: st, feeder: feeder, started: time.Now().UTC()}
}

func (p *Portal) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.page)
	mux.HandleFunc("/api/status", p.status)
	mux.HandleFunc("/api/groups", p.groups)
	mux.HandleFunc("/api/articles", p.articles)
	mux.HandleFunc("/api/article", p.article)
	mux.HandleFunc("/api/isc", p.isc)
	return mux
}

func (p *Portal) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func (p *Portal) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ng, _ := p.st.CountGroups(ctx)
	na, _ := p.st.CountArticles(ctx)
	peers := p.cfg.Peers
	type peerStat struct {
		Host  string `json:"host"`
		Port  int    `json:"port"`
		Up    bool   `json:"up"`
		Error string `json:"error,omitempty"`
	}
	ps := make([]peerStat, 0, len(peers))
	for _, peer := range peers {
		st := peerStat{Host: peer.Host, Port: peer.Port}
		st.Up, st.Error = pingNNTP(peer.Addr(), 2*time.Second)
		ps = append(ps, st)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"software":    nntp.Software,
		"version":     nntp.Version,
		"hostname":    p.cfg.Server.Hostname,
		"pathhost":    p.cfg.Server.Pathhost,
		"organization": p.cfg.Server.Organization,
		"listen_nntp": p.cfg.Listen.NNTP,
		"listen_http": p.cfg.Listen.HTTP,
		"started":     p.started.Format(time.RFC3339),
		"groups":      ng,
		"articles":    na,
		"peers":       peers,
		"peer_status": ps,
		"feed":        p.feeder.Stats(),
		"postgres":    redactURL(p.cfg.Storage.Postgres),
		"mbox_dir":    p.cfg.Storage.MBoxDir,
	})
}

func (p *Portal) groups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query().Get("q")
		busy := r.URL.Query().Get("busy") != "0"
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		gs, err := p.st.SearchGroups(r.Context(), q, busy, limit)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"groups": gs})
	case http.MethodPost:
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Status      string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if !article.ValidGroupName(in.Name) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid group name"})
			return
		}
		if in.Status == "" {
			in.Status = "y"
		}
		if err := p.st.EnsureGroup(r.Context(), in.Name, in.Description, in.Status); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": in.Name})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *Portal) articles(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	arts, err := p.st.RecentArticles(r.Context(), limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	type row struct {
		MessageID string `json:"message_id"`
		Subject   string `json:"subject"`
		From      string `json:"from"`
		Date      string `json:"date"`
		StoredAt  string `json:"stored_at"`
		Xref      string `json:"xref"`
		Bytes     int    `json:"bytes"`
	}
	out := make([]row, 0, len(arts))
	for _, a := range arts {
		out = append(out, row{
			MessageID: a.MessageID, Subject: a.Subject, From: a.From, Date: a.Date,
			StoredAt: a.StoredAt.UTC().Format(time.RFC3339), Xref: a.Xref, Bytes: a.Bytes,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"articles": out})
}

func (p *Portal) article(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	a, err := p.st.GetByMsgID(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message_id": a.MessageID,
		"headers":    a.Headers,
		"body":       a.Body,
		"subject":    a.Subject,
		"from":       a.From,
	})
}

func (p *Portal) isc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	groups, err := isc.Fetch(ctx, p.cfg.GroupsSource.ISCURL)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := p.st.EnsureGroups(ctx, groups); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"loaded": len(groups)})
}

func pingNNTP(addr string, timeout time.Duration) (bool, string) {
	d := net.Dialer{Timeout: timeout}
	c, err := d.Dial("tcp", addr)
	if err != nil {
		return false, err.Error()
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	buf := make([]byte, 256)
	n, err := c.Read(buf)
	if err != nil {
		return false, err.Error()
	}
	line := string(buf[:n])
	if len(line) < 3 || (line[:3] != "200" && line[:3] != "201") {
		return false, strings.TrimSpace(line)
	}
	_, _ = c.Write([]byte("QUIT\r\n"))
	return true, ""
}

func redactURL(s string) string {
	// postgres://user:pass@host → postgres://user:@host
	scheme, rest, ok := strings.Cut(s, "://")
	if !ok {
		return s
	}
	userinfo, host, ok := strings.Cut(rest, "@")
	if !ok {
		return s
	}
	user, _, hasPass := strings.Cut(userinfo, ":")
	if hasPass {
		userinfo = user + ":"
	}
	return scheme + "://" + userinfo + "@" + host
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
