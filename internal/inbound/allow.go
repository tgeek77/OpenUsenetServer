package inbound

import (
	"net"
	"strings"
	"sync"
	"time"

	"openusenet/internal/config"
)

type dnsEntry struct {
	ips []net.IP
	at  time.Time
}

var dnsCache sync.Map // hostname -> dnsEntry

const dnsTTL = 5 * time.Minute

// EffectiveRules returns the host/IP/CIDR rules used for IHAVE admission.
// Enabled peer incoming/outgoing hostnames are always included.
func EffectiveRules(cfg config.Config, peerHosts []string) []string {
	seen := make(map[string]struct{})
	var rules []string
	add := func(r string) {
		r = strings.TrimSpace(r)
		if r == "" {
			return
		}
		key := strings.ToLower(r)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		rules = append(rules, r)
	}
	for _, r := range cfg.Inbound.Allow {
		add(r)
	}
	for _, h := range peerHosts {
		add(h)
	}
	return rules
}

// Open reports whether IHAVE is accepted from any remote (no rules configured).
func Open(cfg config.Config, peerHosts []string) bool {
	if cfg.Inbound.Open != nil && !*cfg.Inbound.Open {
		return false
	}
	return len(cfg.Inbound.Allow) == 0 && len(peerHosts) == 0
}

// Allowed reports whether remoteAddr (host:port or IP) may IHAVE.
func Allowed(cfg config.Config, remoteAddr string, peerHosts []string) bool {
	if Open(cfg, peerHosts) {
		return true
	}
	rules := EffectiveRules(cfg, peerHosts)
	if len(rules) == 0 {
		return false
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	for _, rule := range rules {
		if matchRule(host, ip, rule) {
			return true
		}
	}
	return false
}

// MatchHost reports whether remoteAddr matches rule (hostname, IP, or CIDR).
func MatchHost(remoteAddr, rule string) bool {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	return matchRule(host, net.ParseIP(host), rule)
}

func matchRule(host string, ip net.IP, rule string) bool {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false
	}
	if strings.EqualFold(rule, host) {
		return true
	}
	if strings.Contains(rule, "/") {
		_, netw, err := net.ParseCIDR(rule)
		if err == nil && ip != nil && netw.Contains(ip) {
			return true
		}
		return false
	}
	if tip := net.ParseIP(rule); tip != nil {
		return ip != nil && tip.Equal(ip)
	}
	if ip == nil {
		return false
	}
	for _, resolved := range resolveHost(rule) {
		if resolved.Equal(ip) {
			return true
		}
	}
	return false
}

func resolveHost(host string) []net.IP {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}
	now := time.Now()
	if v, ok := dnsCache.Load(host); ok {
		e := v.(dnsEntry)
		if now.Sub(e.at) < dnsTTL {
			return e.ips
		}
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		dnsCache.Store(host, dnsEntry{at: now})
		return nil
	}
	dnsCache.Store(host, dnsEntry{ips: ips, at: now})
	return ips
}
