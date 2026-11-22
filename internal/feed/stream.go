package feed

import (
	"fmt"
	"net"
	"strings"
	"time"

	"openusenet/internal/nntp"
	"openusenet/internal/store"
)

const peerConnIdleTimeout = 60 * time.Second

type peerConn struct {
	nc        *nntp.Conn
	streaming bool
	addr      string
	user      string
	pass      string
	lastUsed  time.Time
}

func (pc *peerConn) closeGraceful() {
	if pc == nil || pc.nc == nil {
		return
	}
	_ = pc.nc.ReplyRaw("QUIT")
	_, _, _ = pc.nc.ReadReply()
	_ = pc.nc.Close()
	pc.nc = nil
}

func (pc *peerConn) closeNow() {
	if pc == nil || pc.nc == nil {
		return
	}
	_ = pc.nc.Close()
	pc.nc = nil
}

func (f *Feeder) peerAuthCreds(p store.Peer) (user, pass string) {
	pass = strings.TrimSpace(p.OutgoingPassword)
	if pass == "" {
		return "", ""
	}
	user = strings.TrimSpace(p.Name)
	if user == "" {
		user = f.cfg.Server.Pathhost
	}
	return user, pass
}

func (f *Feeder) dialPeer(p store.Peer) (*peerConn, error) {
	addr := p.Addr()
	d := net.Dialer{Timeout: f.timeout}
	c, err := d.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	nc := nntp.NewConn(c, f.timeout)

	code, line, err := nc.ReadReply()
	if err != nil {
		_ = nc.Close()
		return nil, err
	}
	if code != nntp.OKBannerPost && code != nntp.OKBannerNoPost {
		_ = nc.Close()
		return nil, fmt.Errorf("greeting %s", line)
	}
	if err := f.authPeer(nc, p); err != nil {
		_ = nc.Close()
		return nil, err
	}

	streaming, err := peerSupportsStreaming(nc)
	if err != nil {
		_ = nc.Close()
		return nil, err
	}
	user, pass := f.peerAuthCreds(p)
	return &peerConn{
		nc:        nc,
		streaming: streaming,
		addr:      addr,
		user:      user,
		pass:      pass,
		lastUsed:  time.Now(),
	}, nil
}

func (f *Feeder) acquirePeerConn(p store.Peer) (*peerConn, bool, error) {
	addr := p.Addr()
	user, pass := f.peerAuthCreds(p)

	f.connMu.Lock()
	pc := f.conns[p.ID]
	delete(f.conns, p.ID)
	f.connMu.Unlock()

	if pc != nil {
		if pc.nc != nil && pc.addr == addr && pc.user == user && pc.pass == pass && time.Since(pc.lastUsed) < peerConnIdleTimeout {
			return pc, true, nil
		}
		pc.closeGraceful()
	}

	pc, err := f.dialPeer(p)
	if err != nil {
		return nil, false, err
	}
	return pc, false, nil
}

func (f *Feeder) releasePeerConn(peerID int64, pc *peerConn) {
	if pc == nil || pc.nc == nil {
		return
	}
	pc.lastUsed = time.Now()
	f.connMu.Lock()
	if f.closed {
		f.connMu.Unlock()
		pc.closeGraceful()
		return
	}
	if old := f.conns[peerID]; old != nil {
		old.closeNow()
	}
	f.conns[peerID] = pc
	f.connMu.Unlock()
}

func (f *Feeder) pruneIdleConns() {
	now := time.Now()
	var stale []*peerConn
	f.connMu.Lock()
	for id, pc := range f.conns {
		if pc == nil || pc.nc == nil || now.Sub(pc.lastUsed) >= peerConnIdleTimeout {
			stale = append(stale, pc)
			delete(f.conns, id)
		}
	}
	f.connMu.Unlock()
	for _, pc := range stale {
		pc.closeGraceful()
	}
}

// Close closes all cached persistent peer connections.
func (f *Feeder) Close() {
	if f == nil {
		return
	}
	f.connMu.Lock()
	f.closed = true
	var all []*peerConn
	for id, pc := range f.conns {
		all = append(all, pc)
		delete(f.conns, id)
	}
	f.connMu.Unlock()
	for _, pc := range all {
		pc.closeGraceful()
	}
}

type preparedItem struct {
	item store.FeedQueueItem
	wire []byte
}

type itemOutcome struct {
	item     store.FeedQueueItem
	accepted bool
	done     bool
	err      error
}

// deliverBatch delivers a batch of prepared items to one peer over a persistent
// connection, pipelining CHECK/TAKETHIS when streaming is supported.
func (f *Feeder) deliverBatch(p store.Peer, batch []preparedItem) []itemOutcome {
	if len(batch) == 0 {
		return nil
	}
	pc, reused, err := f.acquirePeerConn(p)
	if err != nil {
		out := make([]itemOutcome, len(batch))
		for i, pi := range batch {
			out[i] = itemOutcome{item: pi.item, err: err}
		}
		return out
	}

	out, connBroken, progressed := f.runBatchOnConn(pc, batch)
	if connBroken {
		pc.closeNow()
		if reused && !progressed {
			// Stale pooled connection died before making progress; retry once fresh.
			pc2, err := f.dialPeer(p)
			if err != nil {
				for i := range out {
					out[i] = itemOutcome{item: batch[i].item, err: err}
				}
				return out
			}
			out, connBroken, _ = f.runBatchOnConn(pc2, batch)
			if connBroken {
				pc2.closeNow()
			} else {
				f.releasePeerConn(p.ID, pc2)
			}
		}
		return out
	}
	f.releasePeerConn(p.ID, pc)
	return out
}

func (f *Feeder) runBatchOnConn(pc *peerConn, batch []preparedItem) (outcomes []itemOutcome, connBroken bool, progressed bool) {
	if pc.streaming {
		return runStreamingBatch(pc, batch)
	}
	return runIHaveBatch(pc, batch)
}

func runStreamingBatch(pc *peerConn, batch []preparedItem) ([]itemOutcome, bool, bool) {
	outcomes := make([]itemOutcome, len(batch))
	for i, pi := range batch {
		outcomes[i].item = pi.item
	}

	// Pipeline all CHECK commands for the batch.
	for i, pi := range batch {
		if err := pc.nc.ReplyRaw("CHECK " + pi.item.MessageID); err != nil {
			for j := i; j < len(batch); j++ {
				outcomes[j].err = err
			}
			// Mark earlier ones as failed too since we haven't read their replies.
			for j := 0; j < i; j++ {
				outcomes[j].err = err
			}
			return outcomes, true, false
		}
	}

	type wantedItem struct {
		idx int
		pi  preparedItem
	}
	var want []wantedItem
	progressed := false
	fallbackFrom := -1

	for i, pi := range batch {
		code, line, err := pc.nc.ReadReply()
		if err != nil {
			for j := i; j < len(batch); j++ {
				outcomes[j].err = err
			}
			return outcomes, true, progressed
		}
		progressed = true
		switch code {
		case nntp.OKCheckWant:
			want = append(want, wantedItem{idx: i, pi: pi})
		case nntp.FailCheckRefuse:
			outcomes[i].done = true
			outcomes[i].accepted = false
		case nntp.FailCheckDefer:
			outcomes[i].err = fmt.Errorf("peer deferred CHECK: %s", line)
		case nntp.ErrCommand, nntp.ErrSyntax:
			// Peer does not actually support CHECK; drain remaining CHECK replies
			// and fall back to IHAVE for this and remaining items.
			pc.streaming = false
			if fallbackFrom < 0 {
				fallbackFrom = i
			}
		default:
			outcomes[i].err = fmt.Errorf("CHECK %s", line)
		}
	}

	if fallbackFrom >= 0 {
		sub, broken, subProg := runIHaveBatch(pc, batch[fallbackFrom:])
		for i, o := range sub {
			outcomes[fallbackFrom+i] = o
		}
		return outcomes, broken, progressed || subProg
	}

	if len(want) == 0 {
		return outcomes, false, progressed
	}

	// Pipeline all TAKETHIS commands + article bodies for wanted items.
	for wi, w := range want {
		if err := pc.nc.ReplyRaw("TAKETHIS " + w.pi.item.MessageID); err != nil {
			for j := wi; j < len(want); j++ {
				outcomes[want[j].idx].err = err
			}
			for j := 0; j < wi; j++ {
				outcomes[want[j].idx].err = err
			}
			return outcomes, true, progressed
		}
		if err := pc.nc.WriteDot(w.pi.wire); err != nil {
			for j := 0; j < len(want); j++ {
				outcomes[want[j].idx].err = err
			}
			return outcomes, true, progressed
		}
	}

	// Read all TAKETHIS replies in order.
	for wi, w := range want {
		code, line, err := pc.nc.ReadReply()
		if err != nil {
			for j := wi; j < len(want); j++ {
				outcomes[want[j].idx].err = err
			}
			return outcomes, true, progressed
		}
		switch code {
		case nntp.OKTakeThis:
			outcomes[w.idx].done = true
			outcomes[w.idx].accepted = true
		case nntp.FailTakeThisReject:
			outcomes[w.idx].done = true
			outcomes[w.idx].accepted = false
		default:
			outcomes[w.idx].err = fmt.Errorf("TAKETHIS %s", line)
		}
	}

	return outcomes, false, true
}

func runIHaveBatch(pc *peerConn, batch []preparedItem) ([]itemOutcome, bool, bool) {
	outcomes := make([]itemOutcome, len(batch))
	progressed := false
	for i, pi := range batch {
		outcomes[i].item = pi.item
		accepted, broken, err := ihaveTransfer(pc.nc, pi.item.MessageID, pi.wire)
		if err != nil {
			outcomes[i].err = err
			if broken {
				for j := i + 1; j < len(batch); j++ {
					outcomes[j] = itemOutcome{item: batch[j].item, err: err}
				}
				return outcomes, true, progressed
			}
			progressed = true
			continue
		}
		progressed = true
		outcomes[i].done = true
		outcomes[i].accepted = accepted
	}
	return outcomes, false, progressed
}

// deliverArticle sends one article to a peer using a fresh connection (kept for
// compatibility with callers/tests that send a single article directly).
func (f *Feeder) deliverArticle(p store.Peer, msgid string, wire []byte) (accepted bool, err error) {
	pc, err := f.dialPeer(p)
	if err != nil {
		return false, err
	}
	defer pc.closeGraceful()

	if pc.streaming {
		out, _, _ := runStreamingBatch(pc, []preparedItem{{item: store.FeedQueueItem{MessageID: msgid}, wire: wire}})
		if out[0].err != nil {
			return false, out[0].err
		}
		return out[0].accepted, nil
	}
	accepted, _, err = ihaveTransfer(pc.nc, msgid, wire)
	return accepted, err
}

func (f *Feeder) authPeer(nc *nntp.Conn, p store.Peer) error {
	user, pass := f.peerAuthCreds(p)
	if pass == "" {
		return nil
	}
	if err := nc.ReplyRaw("AUTHINFO USER " + user); err != nil {
		return err
	}
	code, line, err := nc.ReadReply()
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
	return nil
}

func peerSupportsStreaming(nc *nntp.Conn) (bool, error) {
	if err := nc.ReplyRaw("CAPABILITIES"); err != nil {
		return false, err
	}
	code, _, err := nc.ReadReply()
	if err != nil {
		return false, err
	}
	if code == nntp.InfoCapabilities {
		streaming := false
		for {
			line, err := nc.ReadLine()
			if err != nil {
				return false, err
			}
			if line == "." {
				break
			}
			line = strings.TrimPrefix(line, ".")
			fields := strings.Fields(line)
			if len(fields) > 0 && strings.EqualFold(fields[0], "STREAMING") {
				streaming = true
			}
		}
		return streaming, nil
	}
	// Peer does not implement CAPABILITIES (e.g. Diablo): try MODE STREAM (RFC 4644).
	if err := nc.ReplyRaw("MODE STREAM"); err != nil {
		return false, err
	}
	code, _, err = nc.ReadReply()
	if err != nil {
		return false, err
	}
	return code == nntp.OKStreamMode, nil
}

func ihaveTransfer(nc *nntp.Conn, msgid string, wire []byte) (accepted bool, broken bool, err error) {
	if err := nc.ReplyRaw("IHAVE " + msgid); err != nil {
		return false, true, err
	}
	code, line, err := nc.ReadReply()
	if err != nil {
		return false, true, err
	}
	switch code {
	case nntp.ContIHave:
		if err := nc.WriteDot(wire); err != nil {
			return false, true, err
		}
		code, line, err = nc.ReadReply()
		if err != nil {
			return false, true, err
		}
		switch code {
		case nntp.OKIHave:
			return true, false, nil
		case nntp.FailIHaveRefuse, nntp.FailIHaveReject:
			return false, false, nil
		case nntp.FailIHaveDefer:
			return false, false, fmt.Errorf("peer deferred: %s", line)
		default:
			return false, false, fmt.Errorf("after transfer %s", line)
		}
	case nntp.FailIHaveRefuse, nntp.FailIHaveReject:
		return false, false, nil
	case nntp.FailIHaveDefer:
		return false, false, fmt.Errorf("peer deferred: %s", line)
	default:
		return false, false, fmt.Errorf("IHAVE %s", line)
	}
}
