package feed

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"openusenet/internal/article"
	"openusenet/internal/config"
	"openusenet/internal/nntp"
	"openusenet/internal/store"
)

// PeerSource supplies outbound IHAVE destinations (usually the DB).
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
}

// Feeder offers newly accepted articles to configured peers via durable queue + IHAVE.
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

// Offer enqueues the article for peers whose Path/patterns allow it.
// Transfers run from RunWorker; wire is unused when the article is already stored.
func (f *Feeder) Offer(msgid, path string, groups []string, _ []byte) {
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
	for _, p := range list {
		if skipPeer(f.cfg, p, path) {
			continue
		}
		if !PeerWants(p, groups) {
			continue
		}
		if err := f.arts.EnqueueFeed(context.Background(), p.ID, msgid); err != nil {
			f.log.Printf("feed enqueue %s -> %d: %v", msgid, p.ID, err)
			continue
		}
		f.offered.Add(1)
	}
	// Kick an immediate drain so local tests and low-latency peers do not wait for the ticker.
	go f.drainOnce(context.Background())
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
		// Article cancelled/expired — drop quietly.
		return nil
	}
	wire := []byte(art.Headers + "\r\n\r\n" + art.Body)
	return f.ihave(*peer, it.MessageID, wire)
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
	if article.PathContains(path, host) || article.PathContains(path, token) {
		return true
	}
	if article.PathContains(path, cfg.Server.Pathhost) && strings.EqualFold(host, cfg.Server.Pathhost) {
		return true
	}
	return false
}

func (f *Feeder) ihave(p store.Peer, msgid string, wire []byte) error {
	d := net.Dialer{Timeout: f.timeout}
	c, err := d.Dial("tcp", p.Addr())
	if err != nil {
		return err
	}
	nc := nntp.NewConn(c, f.timeout)
	defer func() { _ = nc.Close() }()
	code, line, err := nc.ReadReply()
	if err != nil {
		return err
	}
	if code != nntp.OKBannerPost && code != nntp.OKBannerNoPost {
		return fmt.Errorf("greeting %s", line)
	}
	pass := strings.TrimSpace(p.OutgoingPassword)
	if pass != "" {
		user := strings.TrimSpace(p.Name)
		if user == "" {
			user = f.cfg.Server.Pathhost
		}
		if err := nc.ReplyRaw("AUTHINFO USER " + user); err != nil {
			return err
		}
		code, line, err = nc.ReadReply()
		if err != nil {
			return err
		}
		if code != nntp.ContAuthPass {
			return fmt.Errorf("AUTHINFO USER %s", line)
		}
		if err := nc.ReplyRaw("AUTHINFO PASS " + pass); err != nil {
			return err
		}
		code, line, err = nc.ReadReply()
		if err != nil {
			return err
		}
		if code != nntp.OKAuth {
			return fmt.Errorf("AUTHINFO PASS %s", line)
		}
	}
	if err := nc.ReplyRaw("IHAVE " + msgid); err != nil {
		return err
	}
	code, line, err = nc.ReadReply()
	if err != nil {
		return err
	}
	switch code {
	case nntp.ContIHave:
		if err := nc.WriteDot(wire); err != nil {
			return err
		}
		code, line, err = nc.ReadReply()
		if err != nil {
			return err
		}
		if code != nntp.OKIHave && code != nntp.FailIHaveRefuse && code != nntp.FailIHaveReject && code != nntp.FailIHaveDefer {
			return fmt.Errorf("after transfer %s", line)
		}
		if code == nntp.FailIHaveDefer {
			return fmt.Errorf("peer deferred: %s", line)
		}
	case nntp.FailIHaveRefuse, nntp.FailIHaveReject:
		// peer already has it or does not want it — success for our queue
	case nntp.FailIHaveDefer:
		return fmt.Errorf("peer deferred: %s", line)
	default:
		return fmt.Errorf("IHAVE %s", line)
	}
	_ = nc.ReplyRaw("QUIT")
	_, _, _ = nc.ReadReply()
	return nil
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
