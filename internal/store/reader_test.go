package store

import (
	"context"
	"testing"
)

func TestSubscriptionsAndReadState(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	if err := m.EnsureGroup(ctx, "local.test", "t", "y"); err != nil {
		t.Fatal(err)
	}
	u, err := m.CreateUser(ctx, User{Username: "bob", PasswordHash: "x", Role: RoleUser, CanPost: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Subscribe(ctx, u.ID, "local.test"); err != nil {
		t.Fatal(err)
	}
	if err := m.Subscribe(ctx, u.ID, "nope"); err == nil {
		t.Fatal("expected ErrNoGroup")
	}
	subs, err := m.ListSubscriptions(ctx, u.ID)
	if err != nil || len(subs) != 1 || subs[0].GroupName != "local.test" {
		t.Fatalf("%v %v", subs, err)
	}
	if err := m.SetReadState(ctx, u.ID, "local.test", 5); err != nil {
		t.Fatal(err)
	}
	n, err := m.GetReadState(ctx, u.ID, "local.test")
	if err != nil || n != 5 {
		t.Fatalf("%d %v", n, err)
	}
	if err := m.SetReadState(ctx, u.ID, "local.test", 3); err != nil {
		t.Fatal(err)
	}
	n, _ = m.GetReadState(ctx, u.ID, "local.test")
	if n != 5 {
		t.Fatalf("watermark should only increase, got %d", n)
	}
	if err := m.Unsubscribe(ctx, u.ID, "local.test"); err != nil {
		t.Fatal(err)
	}
	subs, _ = m.ListSubscriptions(ctx, u.ID)
	if len(subs) != 0 {
		t.Fatal(subs)
	}
}
