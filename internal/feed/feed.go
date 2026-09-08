package feed

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"openusenet/internal/article"
	"openusenet/internal/config"
	"openusenet/internal/store"
)

// PeerSource supplies outbound feed destinations (usually the DB).
type PeerSource interface {
	ListEnabledPeers(ctx context.Context) ([]store.Peer, error)
}

// ArticleSource loads stored articles for outbound transfer.
type ArticleSource interface {
	GetByMsgID(ctx context.Context, msgid string) (*store.StoredArticle, error)
	EnqueueFeed(ctx context.Context, peerID int64, msgid string) error
	ClaimFeedDue(ctx context.Context, limit int) ([]store.FeedQueueItem, error)
	CompleteFeed(ctx context.Context, id int64) error
	FailFeed(ctx context.Context, id int64, errMsg string, retryAfter time.Duration) error
	GetPeer(ctx context.Context, id int64) (*store.Peer, error)
	FeedQueueStats(ctx context.Context) (store.FeedQueueStats, error)
	GetGroup(ctx context.Context, name string) (*store.Group, error)
}

// Feeder offers newly accepted articles to configured peers via durable queue
// and CHECK/TAKETHIS (streaming) or IHAVE.
type Feeder struct {
	cfg     config.Config
	peers   PeerSource
	arts    ArticleSource
	log     *log.Logger
	timeout time.Duration
	offered atomic.Int64
	ok      atomic.Int64
	fail    atomic.Int64
	last    atomic.Value // string
}

func New(cfg config.Config, peers PeerSource, arts ArticleSource, lg *log.Logger) *Feeder {
	if lg == nil {
		lg = log.Default()
	}
	return &Feeder{cfg: cfg, peers: peers, arts: arts, log: lg, timeout: 15 * time.Second}
}

// Offer enqueues the article for peers whose Path/patterns/flags allow it.
func (f *Feeder) Offer(msgid, path string, groups []string, wire []byte) {
	if f == nil || msgid == "" || f.arts == nil {
		return
	}
	var list []store.Peer
	if f.peers != nil {
		var err error
		list, err = f.peers.ListEnabledPeers(context.Background())
		if err != nil {
			f.log.Printf("feed list peers: %v", err)
			return
		}
	}
	view := ArticleView{Groups: groups, Path: path, Bytes: len(wire)}
	if art, err := article.Parse(wire); err == nil && art != nil {
		view = ViewFromArticle(art, len(wire))
		if path != "" {
			view.Path = path
		}
		if len(groups) > 0 {
			view.Groups = groups
		}
	}
	view.GroupStatus = f.groupStatus(context.Background(), view.Groups)

	for _, p := range list {
		if skipPeer(f.cfg, p, view.Path) {
			continue
		}
		if !PeerWantsArticle(p, view) {
			continue
		}
		if err := f.arts.EnqueueFeed(context.Background(), p.ID, msgid); err != nil {
			f.log.Printf("feed enqueue %s -> %d: %v", msgid, p.ID, err)
			continue
		}
		f.offered.Add(1)
	}
	go f.drainOnce(context.Background())
}

func (f *Feeder) groupStatus(ctx context.Context, groups []string) map[string]string {
	if f.arts == nil || len(groups) == 0 {
		return nil
	}
	out := make(map[string]string, len(groups))
	for _, g := range groups {
		gr, err := f.arts.GetGroup(ctx, g)
		if err != nil || gr == nil {
			continue
		}
		out[g] = gr.Status
	}
	return out
}

// RunWorker drains the durable feed queue until ctx is done.
func (f *Feeder) RunWorker(ctx context.Context) {
	if f == nil || f.arts == nil {
		return
	}
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		f.drainOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (f *Feeder) drainOnce(ctx context.Context) {
	items, err := f.arts.ClaimFeedDue(ctx, 32)
	if err != nil {
		f.log.Printf("feed claim: %v", err)
		return
	}
	for _, it := range items {
		if err := f.deliver(ctx, it); err != nil {
			f.fail.Add(1)
			backoff := retryBackoff(it.Attempts)
			_ = f.arts.FailFeed(ctx, it.ID, err.Error(), backoff)
			f.log.Printf("feed %s peer=%d: %v (retry in %s)", it.MessageID, it.PeerID, err, backoff)
			continue
		}
		_ = f.arts.CompleteFeed(ctx, it.ID)
		f.ok.Add(1)
		f.last.Store(fmt.Sprintf("%s -> peer %d", it.MessageID, it.PeerID))
	}
}

func retryBackoff(attempts int) time.Duration {
	d := time.Duration(attempts) * time.Minute
	if d < 30*time.Second {
		d = 30 * time.Second
	}
	if d > 30*time.Minute {
		d = 30 * time.Minute
	}
	return d
}

func (f *Feeder) deliver(ctx context.Context, it store.FeedQueueItem) error {
	peer, err := f.arts.GetPeer(ctx, it.PeerID)
	if err != nil {
		return err
	}
	if peer == nil || !peer.Enabled {
		_ = f.arts.CompleteFeed(ctx, it.ID)
		return nil
	}
	art, err := f.arts.GetByMsgID(ctx, it.MessageID)
	if err != nil {
		return err
	}
	if art == nil {
		return nil
	}
	wire := []byte(art.Headers + "\r\n\r\n" + art.Body)
	// Re-check flags at send time (size / active file may matter).
	parsed, _ := article.Parse(wire)
	view := ViewFromArticle(parsed, len(wire))
	if parsed == nil {
		view = ArticleView{Bytes: len(wire)}
	}
	view.GroupStatus = f.groupStatus(ctx, view.Groups)
	if !PeerWantsArticle(*peer, view) {
		return nil // drop quietly — peer no longer wants it
	}
	return f.deliverArticle(*peer, it.MessageID, wire)
}

func skipPeer(cfg config.Config, p store.Peer, path string) bool {
	host := strings.TrimSpace(p.Host)
	if host == "" {
		return true
	}
	token := strings.TrimSpace(p.PathToken)
	if token == "" {
		token = host
	}
	if strings.EqualFold(host, cfg.Server.Hostname) || strings.EqualFold(host, cfg.Server.Pathhost) {
		return true
	}
	flags := ParseFlags(p.Flags)
	// Always suppress if the peer's Path identity is already in Path.
	if article.PathContains(path, token) {
		return true
	}
	if !flags.PathOnlyExclude {
		// Without Ap, also treat sitename and host as Path exclusions.
		if article.PathContains(path, host) {
			return true
		}
		if name := strings.TrimSpace(p.Name); name != "" && article.PathContains(path, name) {
			return true
		}
	} else if article.PathContains(path, host) && strings.EqualFold(host, token) {
		// Host doubles as path identity.
		return true
	}
	if article.PathContains(path, cfg.Server.Pathhost) && strings.EqualFold(host, cfg.Server.Pathhost) {
		return true
	}
	return false
}

type Stats struct {
	Offered int64  `json:"offered"`
	OK      int64  `json:"ok"`
	Fail    int64  `json:"fail"`
	Last    string `json:"last"`
}

func (f *Feeder) Stats() Stats {
	if f == nil {
		return Stats{}
	}
	s := Stats{Offered: f.offered.Load(), OK: f.ok.Load(), Fail: f.fail.Load()}
	if v, ok := f.last.Load().(string); ok {
		s.Last = v
	}
	return s
}
