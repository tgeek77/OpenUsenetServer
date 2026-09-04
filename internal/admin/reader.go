package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"openusenet/internal/posting"
	"openusenet/internal/store"
)

func (p *Portal) reader(w http.ResponseWriter, r *http.Request, u store.User) {
	path := strings.TrimPrefix(r.URL.Path, "/api/reader")
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		path = "/"
	}
	switch {
	case path == "/subscriptions" && r.Method == http.MethodGet:
		p.readerListSubs(w, r, u)
	case path == "/subscriptions" && r.Method == http.MethodPost:
		p.readerSubscribe(w, r, u)
	case path == "/subscriptions" && r.Method == http.MethodDelete:
		p.readerUnsubscribe(w, r, u)
	case path == "/groups" && r.Method == http.MethodGet:
		p.readerSearchGroups(w, r, u)
	case path == "/search" && r.Method == http.MethodGet:
		p.readerSearchArticles(w, r, u)
	case path == "/post" && r.Method == http.MethodPost:
		p.readerPost(w, r, u)
	case strings.HasPrefix(path, "/groups/"):
		p.readerGroupPath(w, r, u, strings.TrimPrefix(path, "/groups/"))
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (p *Portal) readerListSubs(w http.ResponseWriter, r *http.Request, u store.User) {
	subs, err := p.st.ListSubscriptions(r.Context(), u.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if subs == nil {
		subs = []store.Subscription{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": subs})
}

func (p *Portal) readerSubscribe(w http.ResponseWriter, r *http.Request, u store.User) {
	var in struct {
		Group string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := p.st.Subscribe(r.Context(), u.ID, in.Group); err != nil {
		if errors.Is(err, store.ErrNoGroup) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "group not found"})
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": in.Group})
}

func (p *Portal) readerUnsubscribe(w http.ResponseWriter, r *http.Request, u store.User) {
	group := r.URL.Query().Get("group")
	if err := p.st.Unsubscribe(r.Context(), u.ID, group); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": group})
}

func (p *Portal) readerSearchGroups(w http.ResponseWriter, r *http.Request, _ store.User) {
	q := r.URL.Query().Get("q")
	busy := r.URL.Query().Get("busy") == "1"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	gs, err := p.st.SearchGroups(r.Context(), q, busy, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": gs})
}

func (p *Portal) readerSearchArticles(w http.ResponseWriter, r *http.Request, _ store.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"hits": []any{}, "q": q})
		return
	}
	group := strings.TrimSpace(r.URL.Query().Get("group"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	hits, err := p.st.SearchArticles(r.Context(), q, group, limit, offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	if hits == nil {
		hits = []store.ArticleSearchHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits, "q": q, "group": group})
}

func (p *Portal) readerGroupPath(w http.ResponseWriter, r *http.Request, u store.User, rest string) {
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	name := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p.readerGroupMeta(w, r, u, name)
		return
	}
	switch parts[1] {
	case "overview":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p.readerOverview(w, r, u, name)
	case "article":
		if r.Method != http.MethodGet || len(parts) < 3 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "article number required"})
			return
		}
		num, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad article number"})
			return
		}
		p.readerArticle(w, r, u, name, num)
	case "read":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p.readerMarkRead(w, r, u, name)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (p *Portal) readerGroupMeta(w http.ResponseWriter, r *http.Request, u store.User, name string) {
	g, err := p.st.GetGroup(r.Context(), name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if g == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "group not found"})
		return
	}
	last, err := p.st.GetReadState(r.Context(), u.ID, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	unread := int64(0)
	if g.High > last {
		unread = g.High - last
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group": g, "last_read_num": last, "unread": unread,
	})
}

func (p *Portal) readerOverview(w http.ResponseWriter, r *http.Request, u store.User, name string) {
	g, err := p.st.GetGroup(r.Context(), name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if g == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "group not found"})
		return
	}
	lo, _ := strconv.ParseInt(r.URL.Query().Get("lo"), 10, 64)
	hi, _ := strconv.ParseInt(r.URL.Query().Get("hi"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}
	if lo <= 0 && hi <= 0 {
		hi = g.High
		lo = g.High - int64(limit) + 1
		if lo < g.Low {
			lo = g.Low
		}
		if lo <= 0 {
			lo = 1
		}
	}
	rows, err := p.st.Overview(r.Context(), name, lo, hi)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	last, _ := p.st.GetReadState(r.Context(), u.ID, name)
	writeJSON(w, http.StatusOK, map[string]any{
		"group": name, "low": g.Low, "high": g.High, "last_read_num": last,
		"overview": rows,
	})
}

func (p *Portal) readerArticle(w http.ResponseWriter, r *http.Request, u store.User, name string, num int64) {
	a, err := p.st.GetByNumber(r.Context(), name, num)
	if err != nil {
		writeErr(w, err)
		return
	}
	if a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	next, _ := p.st.Next(r.Context(), name, num)
	prev, _ := p.st.Prev(r.Context(), name, num)
	var nextNum, prevNum int64
	if next != nil {
		nextNum = next.Num
	}
	if prev != nil {
		prevNum = prev.Num
	}
	_ = p.st.SetReadState(r.Context(), u.ID, name, num)
	writeJSON(w, http.StatusOK, map[string]any{
		"group":       name,
		"num":         a.Num,
		"message_id":  a.MessageID,
		"subject":     a.Subject,
		"from":        a.From,
		"date":        a.Date,
		"references":  a.Refs,
		"headers":     a.Headers,
		"body":        a.Body,
		"next":        nextNum,
		"prev":        prevNum,
	})
}

func (p *Portal) readerMarkRead(w http.ResponseWriter, r *http.Request, u store.User, name string) {
	var in struct {
		Through *int64 `json:"through"`
		All     bool   `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var through int64
	if in.All {
		g, err := p.st.GetGroup(r.Context(), name)
		if err != nil {
			writeErr(w, err)
			return
		}
		if g == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "group not found"})
			return
		}
		through = g.High
	} else if in.Through != nil {
		through = *in.Through
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "through or all required"})
		return
	}
	if err := p.st.SetReadState(r.Context(), u.ID, name, through); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "last_read_num": through})
}

func (p *Portal) readerPost(w http.ResponseWriter, r *http.Request, u store.User) {
	if !u.MayPost() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "posting not permitted"})
		return
	}
	if ok, msg := p.ops.AcceptArticles(); !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": msg})
		return
	}
	var in posting.Input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(in.From) == "" {
		in.From = u.Username + "@" + p.cfg.Server.Hostname
	}
	res, err := posting.Accept(r.Context(), p.cfg, p.st, p.mbox, p.feeder, p.log, in, u.ID)
	if err != nil {
		if errors.Is(err, store.ErrNoGroup) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "newsgroup does not exist"})
			return
		}
		if errors.Is(err, store.ErrDuplicate) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "duplicate Message-ID"})
			return
		}
		if errors.Is(err, store.ErrQuotaExceeded) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message_id": res.MessageID,
		"xref":       res.Xref,
		"numbers":    res.Numbers,
	})
}
