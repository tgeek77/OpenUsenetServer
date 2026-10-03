package store

import (
	"context"
	"testing"
)

func postTest(t *testing.T, st *Memory, groups []string, msgid, body string) {
	t.Helper()
	ctx := context.Background()
	for _, g := range groups {
		if err := st.EnsureGroup(ctx, g, "", "y"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.Post(ctx, "From: a@b\r\n", body, msgid, "sub", "a@b", "Sat, 03 Oct 2026 12:00:00 +0000", "", "news.test", len(body), 1, groups, true); err != nil {
		t.Fatal(err)
	}
}

func TestPurgeIgnoredGroupDeletesArticles(t *testing.T) {
	st := NewMemory()
	ctx := context.Background()
	postTest(t, st, []string{"alt.binaries.foo", "misc.test"}, "<cross@news>", "binary-body")
	postTest(t, st, []string{"misc.test"}, "<keep@news>", "text")
	if err := st.AddGroupBan(ctx, "alt.binaries.*"); err != nil {
		t.Fatal(err)
	}
	res, err := st.PurgeUnwantedArticles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Articles != 1 || len(res.MessageIDs) != 1 || res.MessageIDs[0] != "<cross@news>" {
		t.Fatalf("purge %+v", res)
	}
	if got, err := st.GetByMsgID(ctx, "<cross@news>"); err != nil || got != nil {
		t.Fatalf("crosspost still stored: %+v %v", got, err)
	}
	kept, err := st.GetByMsgID(ctx, "<keep@news>")
	if err != nil || kept == nil || kept.Body != "text" {
		t.Fatalf("kept article %+v %v", kept, err)
	}
	ok, err := st.HasMessageID(ctx, "<cross@news>")
	if err != nil || !ok {
		t.Fatalf("history should still remember the removed id: %v %v", ok, err)
	}
	unwanted, err := GroupsUnwanted(ctx, st, []string{"misc.test", "alt.binaries.bar"})
	if err != nil || !unwanted {
		t.Fatalf("crosspost into an ignored group should be unwanted: %v %v", unwanted, err)
	}
}

func TestPurgeBlockedGroupDeletesArticles(t *testing.T) {
	st := NewMemory()
	ctx := context.Background()
	postTest(t, st, []string{"alt.binaries.flood"}, "<flood@news>", "payload")
	if err := st.SetGroupRetention(ctx, "alt.binaries.flood", nil, RetentionModeBlocked); err != nil {
		t.Fatal(err)
	}
	res, err := st.PurgeUnwantedArticles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Articles != 1 {
		t.Fatalf("purge %+v", res)
	}
	if n, err := st.CountArticles(ctx); err != nil || n != 0 {
		t.Fatalf("articles left %d %v", n, err)
	}
	unwanted, err := GroupsUnwanted(ctx, st, []string{"alt.binaries.flood", "misc.test"})
	if err != nil || !unwanted {
		t.Fatalf("blocked group should make the article unwanted: %v %v", unwanted, err)
	}
}
