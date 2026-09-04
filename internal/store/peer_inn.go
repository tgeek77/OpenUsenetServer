package store

import (
	"strings"

	"openusenet/internal/inn"
)

func (p Peer) Normalize() Peer {
	p.Host = strings.TrimSpace(p.Host)
	p.IncomingHost = strings.TrimSpace(p.IncomingHost)
	p.Name = strings.TrimSpace(p.Name)
	p.PathToken = strings.TrimSpace(p.PathToken)
	p.Patterns = strings.TrimSpace(p.Patterns)
	p.Distributions = strings.TrimSpace(p.Distributions)
	p.Flags = strings.TrimSpace(p.Flags)
	p.IncomingPassword = strings.TrimSpace(p.IncomingPassword)
	p.OutgoingPassword = strings.TrimSpace(p.OutgoingPassword)
	if p.IncomingHost == "" {
		p.IncomingHost = p.Host
	}
	if p.Host == "" {
		p.Host = p.IncomingHost
	}
	if p.Port <= 0 {
		p.Port = 119
	}
	if p.Patterns == "" {
		p.Patterns = "*"
	}
	if p.Flags == "" {
		p.Flags = "Tm"
	}
	if p.Name == "" && p.Host != "" {
		if i := strings.Index(p.Host, "."); i > 0 {
			p.Name = p.Host[:i]
		} else {
			p.Name = p.Host
		}
	}
	return p
}

func (p Peer) INNSpec() inn.Spec {
	p = p.Normalize()
	return inn.Spec{
		Name: p.Name, PathToken: p.PathToken, IncomingHost: p.IncomingHost,
		OutgoingHost: p.Host, Port: p.Port, Patterns: p.Patterns,
		Distributions: p.Distributions, Flags: p.Flags,
		Password: p.IncomingPassword,
	}
}

func PeerFromINNSpec(s inn.Spec) Peer {
	s.Defaults()
	return Peer{
		Name: s.Name, PathToken: s.PathToken, IncomingHost: s.IncomingHost,
		Host: s.OutgoingHost, Port: s.Port, Patterns: s.Patterns,
		Distributions: s.Distributions, Flags: s.Flags, Enabled: true,
		IncomingPassword: s.Password, OutgoingPassword: s.Password,
	}.Normalize()
}

// PeerIHAVEHosts returns deduplicated hostnames from enabled peers for inbound IHAVE checks.
func PeerIHAVEHosts(peers []Peer) []string {
	seen := make(map[string]struct{})
	var hosts []string
	add := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" {
			return
		}
		key := strings.ToLower(h)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		hosts = append(hosts, h)
	}
	for _, p := range peers {
		if !p.Enabled {
			continue
		}
		p = p.Normalize()
		add(p.IncomingHost)
		add(p.Host)
	}
	return hosts
}
