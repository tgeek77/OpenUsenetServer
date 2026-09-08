package feed

import (
	"testing"

	"openusenet/internal/store"
)

func TestParseFlagsSizeAndLimits(t *testing.T) {
	f := ParseFlags("Ap,Tm,<131072")
	if !f.PathOnlyExclude {
		t.Fatal("Ap expected")
	}
	if f.MaxBytes != 131072 {
		t.Fatalf("max bytes %d", f.MaxBytes)
	}
	f = ParseFlags("Af,Ap,C20,G12,U5,<32768,Tm")
	if !f.NoFiltered || !f.PathOnlyExclude {
		t.Fatalf("Af/Ap %#v", f)
	}
	if f.MaxCrossCost != 20 || f.MaxGroups != 12 || f.MaxFollowups != 5 || f.MaxBytes != 32768 {
		t.Fatalf("limits %#v", f)
	}
	f = ParseFlags("Ac,H3")
	if !f.ExcludeControl || f.MaxPathHops != 3 {
		t.Fatalf("%#v", f)
	}
}

func TestFlagsAllows(t *testing.T) {
	f := ParseFlags("<100,G2,U2,C10")
	v := ArticleView{Groups: []string{"a", "b"}, Bytes: 50, FollowupTo: "a"}
	if !f.Allows(v) {
		t.Fatal("should allow")
	}
	v.Bytes = 100
	if f.Allows(v) {
		t.Fatal("size limit")
	}
	v.Bytes = 50
	v.Groups = []string{"a", "b", "c"}
	if f.Allows(v) {
		t.Fatal("G2")
	}
	f = ParseFlags("Ac")
	if f.Allows(ArticleView{Control: "cancel <x>", Groups: []string{"a"}}) {
		t.Fatal("exclude control")
	}
	f = ParseFlags("AC")
	if f.Allows(ArticleView{Groups: []string{"a"}}) {
		t.Fatal("only control")
	}
	f = ParseFlags("Ae")
	v = ArticleView{Groups: []string{"local.test", "missing"}, GroupStatus: map[string]string{"local.test": "y"}}
	if f.Allows(v) {
		t.Fatal("Ae missing group")
	}
	v.GroupStatus["missing"] = "y"
	if !f.Allows(v) {
		t.Fatal("Ae all present")
	}
}

func TestDistributionWanted(t *testing.T) {
	if !DistributionWanted("", "world") {
		t.Fatal("empty distribs")
	}
	if !DistributionWanted("!local", "") {
		t.Fatal("no header")
	}
	if DistributionWanted("!local", "local") {
		t.Fatal("negated")
	}
	if !DistributionWanted("!local", "world") {
		t.Fatal("other with only negations")
	}
	if !DistributionWanted("world,usa", "usa") {
		t.Fatal("positive match")
	}
	if DistributionWanted("world,usa", "local") {
		t.Fatal("no positive match")
	}
}

func TestPeerWantsArticlePoisonCrosspost(t *testing.T) {
	p := storePeer(`*,@*.bina*`, "", "Ap,Tm")
	v := ArticleView{Groups: []string{"comp.lang.go", "alt.binaries.foo"}, Bytes: 10}
	if PeerWantsArticle(p, v) {
		t.Fatal("poison on any group rejects article")
	}
	v.Groups = []string{"comp.lang.go"}
	if !PeerWantsArticle(p, v) {
		t.Fatal("wanted")
	}
}

func storePeer(patterns, distribs, flags string) store.Peer {
	return store.Peer{Patterns: patterns, Distributions: distribs, Flags: flags, Host: "news-b"}
}
