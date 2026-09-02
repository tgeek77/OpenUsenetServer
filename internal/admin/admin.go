package admin

import (
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openusenet/openusenet/internal/archive"
	"github.com/openusenet/openusenet/internal/article"
	"github.com/openusenet/openusenet/internal/auth"
	"github.com/openusenet/openusenet/internal/config"
	"github.com/openusenet/openusenet/internal/feed"
	"github.com/openusenet/openusenet/internal/inbound"
	"github.com/openusenet/openusenet/internal/inpaths"
	"github.com/openusenet/openusenet/internal/inn"
	"github.com/openusenet/openusenet/internal/isc"
	"github.com/openusenet/openusenet/internal/nntp"
	"github.com/openusenet/openusenet/internal/peerauth"
	"github.com/openusenet/openusenet/internal/store"
)

//go:embed index.html
var indexHTML []byte

type Portal struct {
	cfg     config.Config
	st      store.Store
	mbox    *archive.MBox
	feeder  *feed.Feeder
	inpaths *inpaths.Logger
	sess    *auth.Sessions
	started time.Time
	jobsMu  sync.Mutex
	jobs    map[string]*store.ArchiveJob
	log     *log.Logger
}

func New(cfg config.Config, st store.Store, mbox *archive.MBox, feeder *feed.Feeder, paths *inpaths.Logger, lg *log.Logger) *Portal {
	if lg == nil {
		lg = log.Default()
	}
	return &Portal{
		cfg: cfg, st: st, mbox: mbox, feeder: feeder, inpaths: paths, sess: auth.NewSessions(),
		started: time.Now().UTC(), jobs: map[string]*store.ArchiveJob{}, log: lg,
	}
}

func (p *Portal) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.page)
	mux.HandleFunc("/api/setup", p.setup)
	mux.HandleFunc("/api/login", p.login)
	mux.HandleFunc("/api/logout", p.logout)
	mux.HandleFunc("/api/me", p.me)
	mux.HandleFunc("/api/status", p.withAuth(p.status, false))
	mux.HandleFunc("/api/groups", p.withAuth(p.groups, true))
	mux.HandleFunc("/api/articles", p.withAuth(p.articles, false))
	mux.HandleFunc("/api/article", p.withAuth(p.article, false))
	mux.HandleFunc("/api/isc", p.withAuth(p.isc, true))
	mux.HandleFunc("/api/users", p.withAuth(p.users, true))
	mux.HandleFunc("/api/peers", p.withAuth(p.peers, true))
	mux.HandleFunc("/api/peers/import-inn", p.withAuth(p.peersImportINN, true))
	mux.HandleFunc("/api/peers/our-side", p.withAuth(p.peersOurSide, true))
	mux.HandleFunc("/api/inpaths", p.withAuth(p.inpathsAPI, true))
	mux.HandleFunc("/api/archive", p.withAuth(p.archiveAPI, true))
	mux.HandleFunc("/api/archive/jobs", p.withAuth(p.archiveJobs, true))
	mux.HandleFunc("/api/reader/", p.withAuth(p.reader, false))
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

func (p *Portal) needsSetup(ctx context.Context) (bool, error) {
	n, err := p.st.CountUsers(ctx)
	return n == 0, err
}

func (p *Portal) currentUser(r *http.Request) (store.User, bool) {
	c, err := r.Cookie(auth.SessionCookie)
	if err != nil {
		return store.User{}, false
	}
	return p.sess.Get(c.Value)
}

type handlerFunc func(http.ResponseWriter, *http.Request, store.User)

func (p *Portal) withAuth(next handlerFunc, adminOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		need, err := p.needsSetup(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		if need {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "setup required", "setup": "1"})
			return
		}
		u, ok := p.currentUser(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "login required"})
			return
		}
		if adminOnly && !u.IsAdmin() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin required"})
			return
		}
		next(w, r, u)
	}
}

func (p *Portal) setup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		need, err := p.needsSetup(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"needs_setup": need})
	case http.MethodPost:
		need, err := p.needsSetup(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		if !need {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "already set up"})
			return
		}
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		hash, err := auth.HashPassword(in.Password)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		u, err := p.st.CreateUser(r.Context(), store.User{
			Username: in.Username, PasswordHash: hash, Role: store.RoleAdmin, CanPost: true,
		})
		if err != nil {
			writeErr(w, err)
			return
		}
		id, err := p.sess.Create(*u)
		if err != nil {
			writeErr(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": publicUser(*u)})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *Portal) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	u, err := p.st.GetUser(r.Context(), in.Username)
	if err != nil {
		writeErr(w, err)
		return
	}
	if u == nil || u.Disabled || !auth.CheckPassword(u.PasswordHash, in.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	id, err := p.sess.Create(*u)
	if err != nil {
		writeErr(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": publicUser(*u)})
}

func (p *Portal) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		p.sess.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (p *Portal) me(w http.ResponseWriter, r *http.Request) {
	need, err := p.needsSetup(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	u, ok := p.currentUser(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"needs_setup": need,
		"user":        publicUser(u),
		"logged_in":   ok,
		"hostname":    p.cfg.Server.Hostname,
		"can_post":    ok && u.MayPost(),
		"is_admin":    ok && u.IsAdmin(),
	})
}

func publicUser(u store.User) map[string]any {
	if u.Username == "" {
		return nil
	}
	return map[string]any{
		"id": u.ID, "username": u.Username, "role": u.Role, "can_post": u.CanPost, "disabled": u.Disabled,
	}
}

func (p *Portal) status(w http.ResponseWriter, r *http.Request, _ store.User) {
	ctx := r.Context()
	ng, _ := p.st.CountGroups(ctx)
	na, _ := p.st.CountArticles(ctx)
	peers, _ := p.st.ListPeers(ctx)
	type peerStat struct {
		ID               int64  `json:"id"`
		Name             string `json:"name"`
		Host             string `json:"host"`
		IncomingHost     string `json:"incoming_host"`
		Port             int    `json:"port"`
		Up               bool   `json:"up"`
		Error            string `json:"error,omitempty"`
		Notes            string `json:"notes"`
		Enabled          bool   `json:"enabled"`
		HasIncomingPass  bool   `json:"has_incoming_password"`
	}
	ps := make([]peerStat, 0, len(peers))
	for _, peer := range peers {
		st := peerStat{
			ID: peer.ID, Name: peer.Name, Host: peer.Host, IncomingHost: peer.IncomingHost,
			Port: peer.Port, Notes: peer.Notes, Enabled: peer.Enabled,
			HasIncomingPass: strings.TrimSpace(peer.IncomingPassword) != "",
		}
		if peer.Enabled {
			st.Up, st.Error = pingNNTP(peer.Addr(), 2*time.Second)
		}
		ps = append(ps, st)
	}
	peerHosts := store.PeerIHAVEHosts(peers)
	writeJSON(w, http.StatusOK, map[string]any{
		"software":     nntp.Software,
		"version":      nntp.Version,
		"hostname":     p.cfg.Server.Hostname,
		"pathhost":     p.cfg.Server.Pathhost,
		"organization": p.cfg.Server.Organization,
		"listen_nntp":  p.cfg.Listen.NNTP,
		"listen_http":  p.cfg.Listen.HTTP,
		"listen_nntp_tls": p.cfg.Listen.NNTPTLS,
		"listen_http_tls": p.cfg.Listen.HTTPTLS,
		"started":      p.started.Format(time.RFC3339),
		"groups":       ng,
		"articles":     na,
		"peers":        peers,
		"peer_status":  ps,
		"feed":         p.feeder.Stats(),
		"postgres":     redactURL(p.cfg.Storage.Postgres),
		"mbox_dir":     p.cfg.Storage.MBoxDir,
		"export_dir":   p.cfg.Archive.ExportDir,
		"inbound": map[string]any{
			"open":               inbound.Open(p.cfg, peerHosts),
			"allow":              p.cfg.Inbound.Allow,
			"effective_allow":    inbound.EffectiveRules(p.cfg, peerHosts),
			"peer_hosts":         peerHosts,
			"require_peer_auth":  p.cfg.Inbound.PeerAuthRequired(),
		},
		"cleanfeed": map[string]any{
			"enabled": p.cfg.Cleanfeed.Enabled,
			"mode":    p.cfg.Cleanfeed.Mode,
		},
		"archive_schedule": p.cfg.Archive.Schedule,
		"inpaths":          p.inpathsStatus(),
		"our_side":         p.ourSideSnippets(inn.ExportOpts{}),
	})
}

func (p *Portal) groups(w http.ResponseWriter, r *http.Request, _ store.User) {
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

func (p *Portal) articles(w http.ResponseWriter, r *http.Request, _ store.User) {
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

func (p *Portal) article(w http.ResponseWriter, r *http.Request, _ store.User) {
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

func (p *Portal) isc(w http.ResponseWriter, r *http.Request, _ store.User) {
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

func (p *Portal) users(w http.ResponseWriter, r *http.Request, me store.User) {
	switch r.Method {
	case http.MethodGet:
		us, err := p.st.ListUsers(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		out := make([]map[string]any, 0, len(us))
		for _, u := range us {
			out = append(out, publicUser(u))
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": out})
	case http.MethodPost:
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
			CanPost  *bool  `json:"can_post"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		hash, err := auth.HashPassword(in.Password)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		canPost := true
		if in.CanPost != nil {
			canPost = *in.CanPost
		}
		role := in.Role
		if role == "" {
			role = store.RoleUser
		}
		u, err := p.st.CreateUser(r.Context(), store.User{
			Username: in.Username, PasswordHash: hash, Role: role, CanPost: canPost,
		})
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"user": publicUser(*u)})
	case http.MethodPatch:
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
			CanPost  *bool  `json:"can_post"`
			Disabled *bool  `json:"disabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		hash := ""
		if in.Password != "" {
			var err error
			hash, err = auth.HashPassword(in.Password)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		if err := p.st.UpdateUser(r.Context(), in.Username, in.Role, in.CanPost, in.Disabled, hash); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": in.Username})
	case http.MethodDelete:
		user := r.URL.Query().Get("username")
		if user == me.Username {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete yourself"})
			return
		}
		if err := p.st.DeleteUser(r.Context(), user); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": user})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *Portal) peers(w http.ResponseWriter, r *http.Request, _ store.User) {
	switch r.Method {
	case http.MethodGet:
		if idStr := strings.TrimSpace(r.URL.Query().Get("id")); idStr != "" {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad id"})
				return
			}
			peer, err := p.st.GetPeer(r.Context(), id)
			if err != nil {
				writeErr(w, err)
				return
			}
			if peer == nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
				return
			}
			writeJSON(w, http.StatusOK, p.peerDetail(*peer))
			return
		}
		ps, err := p.st.ListPeers(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"peers": ps})
	case http.MethodPost:
		var in store.Peer
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		in.ID = 0
		peerauth.PreparePeer(p.cfg.Server.Pathhost, &in)
		created, err := p.st.CreatePeer(r.Context(), in)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p.peerDetail(*created))
	case http.MethodPatch:
		var in store.Peer
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		peerauth.PreparePeer(p.cfg.Server.Pathhost, &in)
		peer, err := p.st.UpdatePeer(r.Context(), in)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p.peerDetail(*peer))
	case http.MethodDelete:
		id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if err := p.st.DeletePeer(r.Context(), id); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *Portal) peerDetail(peer store.Peer) map[string]any {
	spec := peer.INNSpec()
	remote := strings.TrimSpace(peer.PathToken)
	if remote == "" {
		remote = strings.TrimSpace(peer.IncomingHost)
	}
	if remote == "" {
		remote = peer.Host
	}
	opts := inn.ExportOpts{
		RemotePathhost: remote,
		Password:       peerauth.PairPassword(p.cfg.Server.Pathhost, remote),
	}
	return map[string]any{
		"peer":     peer,
		"snippets": inn.Snippets(spec),
		"our_side": p.ourSideSnippets(opts),
	}
}

func (p *Portal) ourSideSnippets(opts inn.ExportOpts) map[string]string {
	return inn.OurSide(p.cfg.Server.Hostname, p.cfg.Server.Pathhost, p.nntpPort(), opts)
}

func (p *Portal) peersOurSide(w http.ResponseWriter, r *http.Request, _ store.User) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	opts := inn.ExportOpts{}
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	} else {
		q := r.URL.Query()
		opts.Patterns = q.Get("patterns")
		opts.Distributions = q.Get("distributions")
		opts.Flags = q.Get("flags")
		if portStr := q.Get("port"); portStr != "" {
			opts.Port, _ = strconv.Atoi(portStr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hostname": p.cfg.Server.Hostname,
		"pathhost": p.cfg.Server.Pathhost,
		"port":     p.nntpPort(),
		"opts":     opts,
		"our_side": p.ourSideSnippets(opts),
	})
}

func (p *Portal) nntpPort() int {
	addr := strings.TrimSpace(p.cfg.Listen.NNTP)
	if addr == "" {
		return 119
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			portStr = strings.TrimPrefix(addr, ":")
		} else {
			return 119
		}
	}
	port, _ := strconv.Atoi(portStr)
	if port <= 0 {
		return 119
	}
	return port
}

func (p *Portal) peersImportINN(w http.ResponseWriter, r *http.Request, _ store.User) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Text     string `json:"text"`
		FileType string `json:"file_type"`
		PeerID   int64  `json:"peer_id"`
		Apply    bool   `json:"apply"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	fileType := strings.TrimSpace(in.FileType)
	if fileType == "" {
		fileType = "auto"
	}
	spec, warns := inn.ParseFile(in.Text, fileType)
	if spec == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "parse failed", "warnings": warns})
		return
	}
	preview := store.PeerFromINNSpec(*spec)
	preview.Notes = strings.Join(warns, "; ")
	peerauth.PreparePeer(p.cfg.Server.Pathhost, &preview)

	if !in.Apply {
		writeJSON(w, http.StatusOK, map[string]any{
			"preview":  preview,
			"warnings": warns,
			"snippets": inn.Snippets(*spec),
		})
		return
	}

	var saved *store.Peer
	if in.PeerID > 0 {
		cur, err := p.st.GetPeer(r.Context(), in.PeerID)
		if err != nil {
			writeErr(w, err)
			return
		}
		if cur == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "peer not found"})
			return
		}
		merged := cur.INNSpec()
		inn.MergeSpec(&merged, *spec)
		up := store.PeerFromINNSpec(merged)
		up.ID = cur.ID
		up.Enabled = cur.Enabled
		if strings.TrimSpace(up.IncomingPassword) == "" {
			up.IncomingPassword = cur.IncomingPassword
		}
		if strings.TrimSpace(up.OutgoingPassword) == "" {
			up.OutgoingPassword = cur.OutgoingPassword
		}
		peerauth.PreparePeer(p.cfg.Server.Pathhost, &up)
		if cur.Notes != "" && preview.Notes == "" {
			up.Notes = cur.Notes
		} else if preview.Notes != "" {
			up.Notes = strings.TrimSpace(cur.Notes + "; " + preview.Notes)
		}
		saved, err = p.st.UpdatePeer(r.Context(), up)
		if err != nil {
			writeErr(w, err)
			return
		}
	} else {
		created, err := p.st.CreatePeer(r.Context(), preview)
		if err != nil {
			writeErr(w, err)
			return
		}
		saved = created
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"peer":     saved,
		"warnings": warns,
		"snippets": inn.Snippets(saved.INNSpec()),
	})
}

func (p *Portal) archiveAPI(w http.ResponseWriter, r *http.Request, _ store.User) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Selector string `json:"selector"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.Selector == "" {
		in.Selector = p.cfg.Archive.Groups
	}
	job := p.startArchiveJob(in.Selector)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (p *Portal) archiveJobs(w http.ResponseWriter, r *http.Request, _ store.User) {
	p.jobsMu.Lock()
	defer p.jobsMu.Unlock()
	out := make([]*store.ArchiveJob, 0, len(p.jobs))
	for _, j := range p.jobs {
		cp := *j
		out = append(out, &cp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (p *Portal) startArchiveJob(selector string) *store.ArchiveJob {
	id := strconv.FormatInt(time.Now().UnixNano(), 36)
	job := &store.ArchiveJob{
		ID: id, Status: "running", Selector: selector,
		Dir: p.cfg.Archive.ExportDir, Started: time.Now().UTC(),
	}
	p.jobsMu.Lock()
	p.jobs[id] = job
	p.jobsMu.Unlock()
	go func() {
		res, err := archive.Export(context.Background(), p.st, p.cfg.Archive.ExportDir, selector)
		p.jobsMu.Lock()
		defer p.jobsMu.Unlock()
		job.Finished = time.Now().UTC()
		if err != nil {
			job.Status = "error"
			job.Error = err.Error()
			return
		}
		job.Status = "done"
		job.Dir = res.Dir
		job.Files = res.Files
		job.Groups = res.Groups
		job.Articles = res.Articles
		_ = archive.PruneOldExports(p.cfg.Archive.ExportDir, p.cfg.Archive.RetainGens)
	}()
	return job
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
