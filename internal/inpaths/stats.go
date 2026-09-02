package inpaths

import (
	"strings"
	"time"
	"unicode"
)

const Version = "3.1.1"

// Stats accumulates Path header statistics (ninpaths 3.1.x compatible).
type Stats struct {
	sites     []string
	siteIdx   map[string]int
	siteCount map[string]int
	rels      map[string]int // "a!b" site names, reverse consecutive hops
	articles  int
	start     time.Time
	end       time.Time
}

func NewStats() *Stats {
	now := time.Now()
	return &Stats{
		siteIdx:   map[string]int{},
		siteCount: map[string]int{},
		rels:      map[string]int{},
		start:     now,
		end:       now,
	}
}

// HopOK reports whether a Path hop should be counted (TOP1000 drops pure IPs).
func HopOK(hop string) bool {
	hop = strings.TrimSpace(hop)
	if hop == "" || strings.EqualFold(hop, "not-for-mail") {
		return false
	}
	for _, r := range hop {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// ParseHops splits a Path value into countable hops.
func ParseHops(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	var hops []string
	for _, hop := range strings.Split(path, "!") {
		if HopOK(hop) {
			hops = append(hops, hop)
		}
	}
	return hops
}

func (s *Stats) siteIndex(name string) int {
	if i, ok := s.siteIdx[name]; ok {
		return i
	}
	i := len(s.sites)
	s.sites = append(s.sites, name)
	s.siteIdx[name] = i
	return i
}

// AddPath records one Path header field.
func (s *Stats) AddPath(path string) {
	hops := ParseHops(path)
	if len(hops) == 0 {
		return
	}
	now := time.Now()
	if s.articles == 0 {
		s.start = now
	}
	s.end = now
	s.articles++
	for _, h := range hops {
		s.siteCount[h]++
		s.siteIndex(h)
	}
	for i := len(hops) - 1; i > 0; i-- {
		key := hops[i] + "!" + hops[i-1]
		s.rels[key]++
	}
}

// Merge adds other into s.
func (s *Stats) Merge(other *Stats) {
	if other == nil || other.articles == 0 {
		return
	}
	if s.articles == 0 {
		s.start = other.start
	} else if other.start.Before(s.start) {
		s.start = other.start
	}
	if other.end.After(s.end) {
		s.end = other.end
	}
	s.articles += other.articles
	for name, n := range other.siteCount {
		s.siteCount[name] += n
		s.siteIndex(name)
	}
	for key, n := range other.rels {
		s.rels[key] += n
	}
}

func (s *Stats) Articles() int { return s.articles }
