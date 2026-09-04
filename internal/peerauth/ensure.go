package peerauth

import (
	"context"

	"openusenet/internal/store"
)

// EnsurePasswords is a no-op retained for call-site compatibility.
// Peer AUTHINFO passwords are optional and must be set explicitly when needed.
func EnsurePasswords(context.Context, store.Store, string) error {
	return nil
}

// PreparePeer normalizes peer fields before create/update.
// Passwords are left alone — set them explicitly when AUTHINFO is needed.
func PreparePeer(_ string, p *store.Peer) {
	if p == nil {
		return
	}
	*p = p.Normalize()
}
