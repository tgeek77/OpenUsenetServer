package store

import (
	"context"
	"strings"
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

func TestTopGroupsFollowCurrentCounts(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	if err := m.EnsureGroup(ctx, "free.usenet", "", "y"); err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureGroup(ctx, "misc.test", "", "y"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Post(ctx, "Subject: kept\r\n", "body", "<kept@t>", "kept", "a@x", "now", "", "host", 4, 1, []string{"misc.test"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Post(ctx, "Subject: drop\r\n", "body", "<drop@t>", "drop", "a@x", "now", "", "host", 4, 1, []string{"free.usenet"}, false); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordContentStats(ctx, ContentStatsEvent{
		Groups: []string{"free.usenet"}, From: "spam <s@x>", Path: "peer!news", Day: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.AddGroupBan(ctx, "free.usenet"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PurgeUnwantedArticles(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.RefreshContentStats(ctx, nil); err != nil {
		t.Fatal(err)
	}
	st, err := m.ContentStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"10", "25", "100"} {
		for _, row := range append(st.TopGroupsToday[label], st.TopGroupsTotal[label]...) {
			if row.Name == "free.usenet" {
				t.Fatalf("ignored empty group still in %s: %+v", label, row)
			}
		}
	}
	top := st.TopGroupsTotal["10"]
	if len(top) != 1 || top[0].Name != "misc.test" || top[0].Count != 1 {
		t.Fatalf("total %+v", top)
	}
	for _, p := range st.TopPostersTotal {
		if strings.Contains(p.From, "j. smith") {
			t.Fatalf("poster of an ignored group still counted: %+v", st.TopPostersTotal)
		}
	}
}

func TestIgnoredPosterDropsOnRecount(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	if err := m.EnsureGroup(ctx, "free.usenet", "", "y"); err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureGroup(ctx, "misc.test", "", "y"); err != nil {
		t.Fatal(err)
	}
	ignored := "Path: peer!news!not-for-mail\r\n"
	if _, err := m.Post(ctx, ignored, "body", "<spam@t>", "spam", "J. Smith <j@x>", "now", "", "host", 4, 1, []string{"free.usenet"}, false); err != nil {
		t.Fatal(err)
	}
	kept := "Path: peer!news!not-for-mail\r\n"
	if _, err := m.Post(ctx, kept, "body", "<kept@t>", "kept", "Ada <a@x>", "now", "", "host", 4, 1, []string{"misc.test"}, false); err != nil {
		t.Fatal(err)
	}
	if err := m.RefreshContentStats(ctx, []string{"news"}); err != nil {
		t.Fatal(err)
	}
	st, err := m.ContentStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.TopPostersTotal) != 2 || st.TopProvidersTotal[0].Site != "peer" {
		t.Fatalf("before ignore posters=%+v providers=%+v", st.TopPostersTotal, st.TopProvidersTotal)
	}
	if err := m.AddGroupBan(ctx, "free.usenet"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PurgeUnwantedArticles(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.RefreshContentStats(ctx, []string{"news"}); err != nil {
		t.Fatal(err)
	}
	st, err = m.ContentStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.TopPostersTotal) != 1 || st.TopPostersTotal[0].From != "ada <a@x>" || st.TopPostersTotal[0].Count != 1 {
		t.Fatalf("after recount %+v", st.TopPostersTotal)
	}
	if len(st.TopProvidersTotal) != 1 || st.TopProvidersTotal[0].Count != 1 {
		t.Fatalf("providers %+v", st.TopProvidersTotal)
	}
}
