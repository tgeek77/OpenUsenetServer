package store

import (
	"context"
	"testing"
	"time"
)

func TestPathStatSites(t *testing.T) {
	got := PathStatSites("peerA!peerB!news-a!not-for-mail", "news-a", "News-A")
	if len(got) != 2 || got[0] != "peerA" || got[1] != "peerB" {
		t.Fatalf("%v", got)
	}
}

func TestContentStatsRollups(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.EnsureGroup(ctx, "local.test", "", "y")
	_ = m.EnsureGroup(ctx, "misc.test", "", "y")
	_ = m.EnsureGroup(ctx, "alt.binaries.foo", "", "y")

	day := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(m.RecordContentStats(ctx, ContentStatsEvent{
		Groups: []string{"local.test", "misc.test"}, From: "Alice <a@x>",
		Path: "peer1!news-a!not-for-mail", Binary: false, ExcludeSite: []string{"news-a"}, Day: day,
	}))
	must(m.RecordContentStats(ctx, ContentStatsEvent{
		Groups: []string{"local.test"}, From: "alice <a@x>",
		Path: "peer1!peer2!news-a", Binary: false, ExcludeSite: []string{"news-a"}, Day: day,
	}))
	must(m.RecordContentStats(ctx, ContentStatsEvent{
		Groups: []string{"alt.binaries.foo"}, From: "bin@x",
		Path: "peer3!news-a", Binary: true, ExcludeSite: []string{"news-a"}, Day: day,
	}))

	// Force "today" by using real now for a second event so ContentStats today window works.
	must(m.RecordContentStats(ctx, ContentStatsEvent{
		Groups: []string{"local.test"}, From: "Bob <b@x>",
		Path: "peer9!news-a", Binary: false, ExcludeSite: []string{"news-a"},
	}))

	st, err := m.ContentStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// populated: need count > 1; Memory EnsureGroup leaves count 0 until Post.
	// Seed two posts so local.test is populated.
	_, err = m.Post(ctx, "Subject: a\r\n", "body", "<p1@t>", "a", "a@x", "now", "", "host", 4, 1, []string{"local.test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Post(ctx, "Subject: b\r\n", "body", "<p2@t>", "b", "b@x", "now", "", "host", 4, 1, []string{"local.test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	st, err = m.ContentStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.PopulatedGroups < 1 {
		t.Fatalf("populated=%d", st.PopulatedGroups)
	}
	top := st.TopGroupsTotal["10"]
	if len(top) < 1 || top[0].Name != "local.test" {
		t.Fatalf("total groups: %+v", top)
	}
	// binary group should not appear in text rankings
	for _, g := range top {
		if g.Name == "alt.binaries.foo" {
			t.Fatal("binary group in text ranking")
		}
	}
	if len(st.TopPostersTotal) < 1 {
		t.Fatal("expected posters")
	}
	// alice normalized from two variants
	foundAlice := false
	for _, p := range st.TopPostersTotal {
		if p.From == "alice <a@x>" && p.Count >= 2 {
			foundAlice = true
		}
	}
	if !foundAlice {
		t.Fatalf("posters: %+v", st.TopPostersTotal)
	}
	if len(st.TopProvidersTotal) < 1 {
		t.Fatal("expected providers")
	}
}
