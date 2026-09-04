package store

import (
	"context"
	"testing"
)

func TestSearchArticles(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.EnsureGroup(ctx, "local.test", "", "y")
	_ = m.EnsureGroup(ctx, "misc.test", "", "y")
	_, err := m.Post(ctx, "Subject: Hello World\r\n", "unique pineapple body text",
		"<s1@t>", "Hello World", "alice@x", "Mon, 01 Jan 2024 00:00:00 +0000", "", "host", 40, 1, []string{"local.test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Post(ctx, "Subject: Other\r\n", "nothing special",
		"<s2@t>", "Other", "bob@x", "Mon, 01 Jan 2024 00:00:00 +0000", "", "host", 20, 1, []string{"misc.test"}, false)
	if err != nil {
		t.Fatal(err)
	}

	empty, err := m.SearchArticles(ctx, "", "", 10, 0)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty query: %v %v", empty, err)
	}

	hits, err := m.SearchArticles(ctx, "pineapple", "", 10, 0)
	if err != nil || len(hits) != 1 {
		t.Fatalf("body hit: %v %v", hits, err)
	}
	if hits[0].MessageID != "<s1@t>" || hits[0].Group != "local.test" {
		t.Fatalf("%+v", hits[0])
	}

	hits, err = m.SearchArticles(ctx, "Hello", "", 10, 0)
	if err != nil || len(hits) != 1 {
		t.Fatalf("subject hit: %v %v", hits, err)
	}

	hits, err = m.SearchArticles(ctx, "pineapple", "misc.test", 10, 0)
	if err != nil || len(hits) != 0 {
		t.Fatalf("group filter: %v %v", hits, err)
	}

	hits, err = m.SearchArticles(ctx, "pineapple -unique", "", 10, 0)
	if err != nil || len(hits) != 0 {
		t.Fatalf("exclusion: %v %v", hits, err)
	}
}
