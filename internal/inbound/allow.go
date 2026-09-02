package inbound

import (
	"net"
	"strings"

	"github.com/openusenet/openusenet/internal/config"
)

// Allowed reports whether remoteAddr (host:port or IP) may IHAVE.
// Empty allow list means everyone is allowed.
func Allowed(cfg config.Config, remoteAddr string) bool {
	if len(cfg.Inbound.Allow) == 0 {
		return true
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	for _, rule := range cfg.Inbound.Allow {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		if strings.EqualFold(rule, host) {
			return true
		}
		if strings.Contains(rule, "/") {
			_, netw, err := net.ParseCIDR(rule)
			if err == nil && ip != nil && netw.Contains(ip) {
				return true
			}
			continue
		}
		if tip := net.ParseIP(rule); tip != nil && ip != nil && tip.Equal(ip) {
			return true
		}
	}
	return false
}
