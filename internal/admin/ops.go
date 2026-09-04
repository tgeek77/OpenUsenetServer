package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"openusenet/internal/article"
	"openusenet/internal/nntp"
	"openusenet/internal/store"
)

func (p *Portal) opsAPI(w http.ResponseWriter, r *http.Request, _ store.User) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ops": p.ops.Snapshot()})
	case http.MethodPost:
		var in struct {
			Action  string `json:"action"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
			Enabled *bool  `json:"enabled"`
			PeerID  int64  `json:"peer_id"`
			MsgID   string `json:"message_id"`
			Group   string `json:"group"`
			Status  string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		ctx := r.Context()
		switch strings.ToLower(strings.TrimSpace(in.Action)) {
		case "pause":
			p.ops.Pause(in.Reason, "admin")
		case "throttle":
			p.ops.Throttle(in.Reason, "admin")
		case "go", "resume":
			p.ops.Go("admin")
		case "readers":
			en := true
			if in.Enabled != nil {
				en = *in.Enabled
			}
			p.ops.SetReaders(en, in.Message)
		case "peers":
			en := true
			if in.Enabled != nil {
				en = *in.Enabled
			}
			p.ops.SetPeers(en, in.Message)
		case "watchdog":
			en := true
			if in.Enabled != nil {
				en = *in.Enabled
			}
			p.ops.SetWatchdogEnabled(en)
		case "expire":
			res, err := p.st.Expire(ctx, p.cfg.Retention.HistoryDays)
			if err != nil {
				writeErr(w, err)
				return
			}
			p.ops.NoteExpire(res)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "expire": res, "ops": p.ops.Snapshot()})
			return
		case "cancel":
			id := strings.TrimSpace(in.MsgID)
			if !article.ValidMessageID(id) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid message_id required"})
				return
			}
			existed, err := p.st.CancelMessageID(ctx, id)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "existed": existed, "message_id": id})
			return
		case "remember":
			id := strings.TrimSpace(in.MsgID)
			if !article.ValidMessageID(id) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid message_id required"})
				return
			}
			if err := p.st.RememberMessageID(ctx, id); err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message_id": id})
			return
		case "flush_feed":
			n, err := p.st.FlushFeedQueue(ctx, in.PeerID)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "flushed": n})
			return
		case "rmgroup":
			name := strings.TrimSpace(in.Group)
			if !article.ValidGroupName(name) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid group"})
				return
			}
			if err := p.st.DeleteGroup(ctx, name); err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "group": name})
			return
		case "changegroup":
			name := strings.TrimSpace(in.Group)
			st := strings.TrimSpace(in.Status)
			if !article.ValidGroupName(name) || (st != "y" && st != "n" && st != "m") {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group and status y|n|m required"})
				return
			}
			if err := p.st.EnsureGroup(ctx, name, "", st); err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "group": name, "status": st})
			return
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ops": p.ops.Snapshot()})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *Portal) statsAPI(w http.ResponseWriter, r *http.Request, _ store.User) {
	ctx := r.Context()
	ng, _ := p.st.CountGroups(ctx)
	na, _ := p.st.CountArticles(ctx)
	nq, _ := p.st.FeedQueueStats(ctx)
	peers, _ := p.st.ListPeers(ctx)
	openAlerts, _ := p.st.ListGroupAlerts(ctx, store.AlertOpen)
	snap := p.ops.Snapshot()
	type peerRow struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Host     string `json:"host"`
		Enabled  bool   `json:"enabled"`
		Patterns string `json:"patterns"`
		Queue    int    `json:"queue"`
		Up       bool   `json:"up"`
		Error    string `json:"error,omitempty"`
	}
	rows := make([]peerRow, 0, len(peers))
	for _, peer := range peers {
		pr := peerRow{
			ID: peer.ID, Name: peer.Name, Host: peer.Host, Enabled: peer.Enabled,
			Patterns: peer.Patterns, Queue: nq.ByPeer[peer.ID],
		}
		if peer.Enabled {
			pr.Up, pr.Error = pingNNTP(peer.Addr(), 2*time.Second)
		}
		rows = append(rows, pr)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"software":     nntp.Software,
		"version":      nntp.Version,
		"hostname":     p.cfg.Server.Hostname,
		"uptime_sec":   int(time.Since(p.started).Seconds()),
		"started":      p.started.Format(time.RFC3339),
		"groups":       ng,
		"articles":     na,
		"open_alerts":  len(openAlerts),
		"feed":         p.feeder.Stats(),
		"feed_queue":   nq,
		"ops":          snap,
		"peers":        rows,
		"watchdog":     p.cfg.Watchdog,
		"art_cutoff":   p.cfg.Limits.ArtCutoffDays,
		"history_days": p.cfg.Retention.HistoryDays,
		"mode_label":   string(snap.Mode),
	})
}
