package store

import (
	"context"
	"testing"
)

func TestRememberAndCancel(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.EnsureGroup(ctx, "local.test", "", "y")
	_, err := m.Post(ctx, "Subject: x\r\n", "body", "<a@b>", "x", "a@b", "Mon, 01 Jan 2024 00:00:00 +0000", "", "host", 10, 1, []string{"local.test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := m.HasMessageID(ctx, "<a@b>")
	if err != nil || !ok {
		t.Fatal("expected history")
	}
	existed, err := m.CancelMessageID(ctx, "<a@b>")
	if err != nil || !existed {
		t.Fatalf("cancel %v %v", existed, err)
	}
	art, err := m.GetByMsgID(ctx, "<a@b>")
	if err != nil || art != nil {
		t.Fatal("article should be gone")
	}
	ok, err = m.HasMessageID(ctx, "<a@b>")
	if err != nil || !ok {
		t.Fatal("history should remain")
	}
	if err := m.RememberMessageID(ctx, "<rej@b>"); err != nil {
		t.Fatal(err)
	}
	ok, _ = m.HasMessageID(ctx, "<rej@b>")
	if !ok {
		t.Fatal("remember failed")
	}
}

func TestFeedQueue(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	p, err := m.CreatePeer(ctx, Peer{Host: "peer.example", Port: 119, Enabled: true, Patterns: "*"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EnqueueFeed(ctx, p.ID, "<q@b>"); err != nil {
		t.Fatal(err)
	}
	items, err := m.ClaimFeedDue(ctx, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("claim %v %v", items, err)
	}
	_ = m.CompleteFeed(ctx, items[0].ID)
	st, _ := m.FeedQueueStats(ctx)
	if st.Depth != 0 {
		t.Fatalf("depth %d", st.Depth)
	}
}
