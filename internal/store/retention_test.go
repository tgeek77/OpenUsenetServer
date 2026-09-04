package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBinaryFloodTriggersRetentionAndAlert(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	if err := m.EnsureGroup(ctx, "alt.fan.test", "x", "y"); err != nil {
		t.Fatal(err)
	}
	flood := FloodParams{
		Window: time.Hour, MinBinary: 5, MinRatio: 0.5, FloodDays: 7,
	}
	for i := 0; i < 5; i++ {
		if _, err := m.NoteAccept(ctx, []string{"alt.fan.test"}, true, flood); err != nil {
			t.Fatal(err)
		}
	}
	g, err := m.GetGroup(ctx, "alt.fan.test")
	if err != nil || g == nil {
		t.Fatal(err)
	}
	if g.RetentionDays == nil || *g.RetentionDays != 7 {
		t.Fatalf("retention=%v", g.RetentionDays)
	}
	alerts, err := m.ListGroupAlerts(ctx, AlertOpen)
	if err != nil || len(alerts) != 1 {
		t.Fatalf("alerts=%v err=%v", alerts, err)
	}
	if err := m.ResolveGroupAlert(ctx, alerts[0].ID, "whitelist"); err != nil {
		t.Fatal(err)
	}
	g, _ = m.GetGroup(ctx, "alt.fan.test")
	if g.RetentionMode != RetentionModeWhitelist || g.RetentionDays != nil {
		t.Fatalf("whitelist failed: %+v", g)
	}
	// Further binaries should not re-apply quota.
	for i := 0; i < 10; i++ {
		_, _ = m.NoteAccept(ctx, []string{"alt.fan.test"}, true, flood)
	}
	g, _ = m.GetGroup(ctx, "alt.fan.test")
	if g.RetentionDays != nil {
		t.Fatal("whitelisted group got retention again")
	}
}

func TestUserBinaryQuota(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		used, err := m.ConsumeBinaryPostQuota(ctx, 1, 25)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if used != i+1 {
			t.Fatalf("used=%d", used)
		}
	}
	_, err := m.ConsumeBinaryPostQuota(ctx, 1, 25)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("want quota exceeded, got %v", err)
	}
	used, err := m.BinaryPostQuotaUsed(ctx, 1)
	if err != nil || used != 25 {
		t.Fatalf("used=%d err=%v", used, err)
	}
}

func TestExpireRemovesShortRetention(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.EnsureGroup(ctx, "alt.junk", "x", "y")
	days := 7
	_ = m.SetGroupRetention(ctx, "alt.junk", &days, RetentionModeAuto)
	_, err := m.Post(ctx, "Subject: x\r\n", "body", "<old@x>", "x", "a@b.c", "now", "", "news", 4, 1, []string{"alt.junk"})
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	for _, a := range m.arts {
		a.StoredAt = time.Now().UTC().Add(-8 * 24 * time.Hour)
	}
	m.mu.Unlock()
	res, err := m.Expire(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if res.ArticlesRemoved < 1 {
		t.Fatalf("expire result %+v", res)
	}
	n, _ := m.CountArticles(ctx)
	if n != 0 {
		t.Fatalf("articles left %d", n)
	}
}
