package feed

import (
	"testing"

	"openusenet/internal/config"
	"openusenet/internal/store"
)

func TestSkipPeer(t *testing.T) {
	cfg := config.Config{
		Server: config.Server{Hostname: "news-a", Pathhost: "news-a"},
	}
	path := "news-a!not-for-mail"
	if !skipPeer(cfg, store.Peer{Host: "news-a"}, path) {
		t.Fatal("should skip self")
	}
	if skipPeer(cfg, store.Peer{Host: "news-b"}, path) {
		t.Fatal("should offer to news-b")
	}
	if !skipPeer(cfg, store.Peer{Host: "news-b"}, "news-b!news-a!not-for-mail") {
		t.Fatal("should skip hop already in Path")
	}
	if skipPeer(cfg, store.Peer{Host: ""}, path) == false {
		t.Fatal("empty host should skip")
	}
}

func TestGroupsWanted(t *testing.T) {
	if !GroupsWanted("*,@*.bina*,!local.*", []string{"comp.lang.go"}) {
		t.Fatal("wanted")
	}
	if GroupsWanted("*,@*.bina*,!local.*", []string{"alt.binaries.foo"}) {
		t.Fatal("binary poisoned")
	}
	if GroupsWanted("*,!local.*", []string{"local.test"}) {
		t.Fatal("local excluded")
	}
	if !PeerWants(store.Peer{Patterns: ""}, []string{"any.group"}) {
		t.Fatal("empty patterns = *")
	}
}
