package feed

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/openusenet/openusenet/internal/article"
	"github.com/openusenet/openusenet/internal/config"
	"github.com/openusenet/openusenet/internal/nntp"
	"github.com/openusenet/openusenet/internal/store"
)

// PeerSource supplies outbound IHAVE destinations (usually the DB).
type PeerSource interface {
	ListEnabledPeers(ctx context.Context) ([]store.Peer, error)
}

// Feeder offers newly accepted articles to configured peers via IHAVE.
type Feeder struct {
	cfg     config.Config
	peers   PeerSource
	log     *log.Logger
	timeout time.Duration
	offered atomic.Int64
	ok      atomic.Int64
	fail    atomic.Int64
	last    atomic.Value // string
}

func New(cfg config.Config, peers PeerSource, lg *log.Logger) *Feeder {
	if lg == nil {
		lg = log.Default()
	}
	return &Feeder{cfg: cfg, peers: peers, log: lg, timeout: 15 * time.Second}
}

// Offer sends the article to peers whose host is not already in Path.
// It returns immediately; transfers run in the background.
func (f *Feeder) Offer(msgid, path string, groups []string, wire []byte) {
	if f == nil || msgid == "" || len(wire) == 0 {
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
	if len(list) == 0 {
		return
	}
	for _, p := range list {
		p := p
		if skipPeer(f.cfg, p, path) {
			continue
		}
		go func() {
			f.offered.Add(1)
			if err := f.ihave(p, msgid, wire); err != nil {
				f.fail.Add(1)
				f.log.Printf("feed %s: %v", p.Addr(), err)
				return
			}
			f.ok.Add(1)
			f.last.Store(msgid + " -> " + p.Addr())
		}()
	}
	_ = groups
}

func skipPeer(cfg config.Config, p store.Peer, path string) bool {
	host := strings.TrimSpace(p.Host)
	if host == "" {
		return true
	}
	if strings.EqualFold(host, cfg.Server.Hostname) || strings.EqualFold(host, cfg.Server.Pathhost) {
		return true
	}
	if article.PathContains(path, host) {
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
	defer nc.Close()
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
	case nntp.FailIHaveRefuse, nntp.FailIHaveReject, nntp.FailIHaveDefer:
		// peer already has it or does not want it
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
