package peerauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"

	"github.com/openusenet/openusenet/internal/inbound"
	"github.com/openusenet/openusenet/internal/store"
)

// PairPassword returns a deterministic shared secret for two pathhosts (mesh/testing).
func PairPassword(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if strings.EqualFold(a, b) {
		return ""
	}
	if strings.ToLower(a) > strings.ToLower(b) {
		a, b = b, a
	}
	sum := sha256.Sum256([]byte(a + "\x00" + b + "\x00openusenet-peer"))
	return hex.EncodeToString(sum[:8])
}

// FillPasswords sets incoming/outgoing passwords when empty.
func FillPasswords(localPathhost string, p *store.Peer) {
	if p == nil {
		return
	}
	remote := strings.TrimSpace(p.PathToken)
	if remote == "" {
		remote = strings.TrimSpace(p.IncomingHost)
	}
	if remote == "" {
		remote = strings.TrimSpace(p.Host)
	}
	if remote == "" || net.ParseIP(remote) != nil {
		return
	}
	secret := PairPassword(localPathhost, remote)
	if secret == "" {
		return
	}
	if strings.TrimSpace(p.IncomingPassword) == "" {
		p.IncomingPassword = secret
	}
	if strings.TrimSpace(p.OutgoingPassword) == "" {
		p.OutgoingPassword = secret
	}
}

// MatchPeer finds the enabled peer matching remoteAddr.
func MatchPeer(peers []store.Peer, remoteAddr string) *store.Peer {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	for i := range peers {
		p := peers[i]
		if !p.Enabled {
			continue
		}
		p = p.Normalize()
		for _, rule := range []string{p.IncomingHost, p.Host} {
			if rule == "" {
				continue
			}
			if inbound.MatchHost(remoteAddr, rule) || strings.EqualFold(rule, host) {
				return &peers[i]
			}
		}
	}
	return nil
}

// VerifyFeedAuth checks INN-style feeder password for the matched peer.
func VerifyFeedAuth(peer *store.Peer, password string, require bool) bool {
	if peer == nil {
		return false
	}
	want := strings.TrimSpace(peer.IncomingPassword)
	if want == "" {
		return !require
	}
	return password == want
}

// ListEnabled loads enabled peers - convenience for session.
func ListEnabled(ctx context.Context, st store.Store) ([]store.Peer, error) {
	return st.ListEnabledPeers(ctx)
}
