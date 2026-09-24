package feed

import (
	"fmt"
	"net"
	"strings"

	"openusenet/internal/nntp"
	"openusenet/internal/store"
)

// deliverArticle sends one article to a peer using STREAMING (CHECK/TAKETHIS)
// when the peer advertises it, otherwise IHAVE.
// deliverArticle returns accepted=false when the peer refuses the offer (435/438)
// without a transport error. Callers must not report that as a successful send.
func (f *Feeder) deliverArticle(p store.Peer, msgid string, wire []byte) (accepted bool, err error) {
	d := net.Dialer{Timeout: f.timeout}
	c, err := d.Dial("tcp", p.Addr())
	if err != nil {
		return false, err
	}
	nc := nntp.NewConn(c, f.timeout)
	defer func() { _ = nc.Close() }()

	code, line, err := nc.ReadReply()
	if err != nil {
		return false, err
	}
	if code != nntp.OKBannerPost && code != nntp.OKBannerNoPost {
		return false, fmt.Errorf("greeting %s", line)
	}
	if err := f.authPeer(nc, p); err != nil {
		return false, err
	}

	streaming, err := peerSupportsStreaming(nc)
	if err != nil {
		return false, err
	}
	if streaming {
		accepted, err = streamingTransfer(nc, msgid, wire)
	} else {
		accepted, err = ihaveTransfer(nc, msgid, wire)
	}
	if err != nil {
		return false, err
	}
	_ = nc.ReplyRaw("QUIT")
	_, _, _ = nc.ReadReply()
	return accepted, nil
}

func (f *Feeder) authPeer(nc *nntp.Conn, p store.Peer) error {
	pass := strings.TrimSpace(p.OutgoingPassword)
	if pass == "" {
		return nil
	}
	user := strings.TrimSpace(p.Name)
	if user == "" {
		user = f.cfg.Server.Pathhost
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
	if code != nntp.InfoCapabilities {
		// Old peer: assume IHAVE only.
		return false, nil
	}
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

func streamingTransfer(nc *nntp.Conn, msgid string, wire []byte) (bool, error) {
	if err := nc.ReplyRaw("CHECK " + msgid); err != nil {
		return false, err
	}
	code, line, err := nc.ReadReply()
	if err != nil {
		return false, err
	}
	switch code {
	case nntp.OKCheckWant:
		// proceed to TAKETHIS
	case nntp.FailCheckRefuse:
		return false, nil
	case nntp.FailCheckDefer:
		return false, fmt.Errorf("peer deferred CHECK: %s", line)
	case nntp.ErrCommand, nntp.ErrSyntax:
		return ihaveTransfer(nc, msgid, wire)
	default:
		return false, fmt.Errorf("CHECK %s", line)
	}

	if err := nc.ReplyRaw("TAKETHIS " + msgid); err != nil {
		return false, err
	}
	if err := nc.WriteDot(wire); err != nil {
		return false, err
	}
	code, line, err = nc.ReadReply()
	if err != nil {
		return false, err
	}
	switch code {
	case nntp.OKTakeThis:
		return true, nil
	case nntp.FailTakeThisReject:
		return false, nil
	default:
		return false, fmt.Errorf("TAKETHIS %s", line)
	}
}

func ihaveTransfer(nc *nntp.Conn, msgid string, wire []byte) (bool, error) {
	if err := nc.ReplyRaw("IHAVE " + msgid); err != nil {
		return false, err
	}
	code, line, err := nc.ReadReply()
	if err != nil {
		return false, err
	}
	switch code {
	case nntp.ContIHave:
		if err := nc.WriteDot(wire); err != nil {
			return false, err
		}
		code, line, err = nc.ReadReply()
		if err != nil {
			return false, err
		}
		switch code {
		case nntp.OKIHave:
			return true, nil
		case nntp.FailIHaveRefuse, nntp.FailIHaveReject:
			return false, nil
		case nntp.FailIHaveDefer:
			return false, fmt.Errorf("peer deferred: %s", line)
		default:
			return false, fmt.Errorf("after transfer %s", line)
		}
	case nntp.FailIHaveRefuse, nntp.FailIHaveReject:
		return false, nil
	case nntp.FailIHaveDefer:
		return false, fmt.Errorf("peer deferred: %s", line)
	default:
		return false, fmt.Errorf("IHAVE %s", line)
	}
}
