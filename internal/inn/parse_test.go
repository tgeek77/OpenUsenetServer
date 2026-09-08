package inn

import "testing"

func TestParseNewsfeedsAndIncoming(t *testing.T) {
	text := `
# newsfeeds
news-b:*,!control:Tp,G21:news-b.example
ME:*:Tc,NnN:news.localhost

peer news-c {
  hostname: news-c.example.com
  streaming: true
}

news-d:563
`
	d := Parse(text)
	if len(d) < 3 {
		t.Fatalf("got %#v", d)
	}
	found := map[string]bool{}
	for _, x := range d {
		found[x.Host] = true
	}
	if !found["news-b.example"] || !found["news-c.example.com"] {
		t.Fatalf("%v", found)
	}
}
