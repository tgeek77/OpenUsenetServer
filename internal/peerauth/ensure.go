package peerauth

import (
	"context"

	"github.com/openusenet/openusenet/internal/store"
)

// EnsurePasswords fills empty peer passwords and persists updates.
func EnsurePasswords(ctx context.Context, st store.Store, localPathhost string) error {
	peers, err := st.ListPeers(ctx)
	if err != nil {
		return err
	}
	for _, p := range peers {
		cp := p
		FillPasswords(localPathhost, &cp)
		if cp.IncomingPassword == p.IncomingPassword && cp.OutgoingPassword == p.OutgoingPassword {
			continue
		}
		if _, err := st.UpdatePeer(ctx, cp); err != nil {
			return err
		}
	}
	return nil
}

// PreparePeer fills passwords on a peer before create/update.
func PreparePeer(localPathhost string, p *store.Peer) {
	if p == nil {
		return
	}
	FillPasswords(localPathhost, p)
}
