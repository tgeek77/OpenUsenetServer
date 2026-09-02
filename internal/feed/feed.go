package feed

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/openusenet/openusenet/internal/article"
	"github.com/openusenet/openusenet/internal/config"
	"github.com/openusenet/openusenet/internal/nntp"
)

// Feeder offers newly accepted articles to configured peers via IHAVE.
type Feeder struct {
	cfg     config.Config
	log     *log.Logger
	timeout time.Duration
}

func New(cfg config.Config, lg *log.Logger) *Feeder {
	if lg == nil {
		lg = log.Default()
	}
	return &Feeder{cfg: cfg, log: lg, timeout: 15 * time.Second}
}

// Offer sends the article to peers whose host is not already in Path.
// It returns immediately; transfers run in the background.
func (f *Feeder) Offer(msgid, path string, groups []string, wire []byte) {
	if f == nil || len(f.cfg.Peers) == 0 || msgid == "" || len(wire) == 0 {
		return
	}
	for _, p := range f.cfg.Peers {
		p := p
		if skipPeer(f.cfg, p, path) {
			continue
		}
		go func() {
			if err := f.ihave(p, msgid, wire); err != nil {
				f.log.Printf("feed %s: %v", p.Addr(), err)
			}
		}()
	}
	_ = groups
}

func skipPeer(cfg config.Config, p config.Peer, path string) bool {
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

func (f *Feeder) ihave(p config.Peer, msgid string, wire []byte) error {
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
