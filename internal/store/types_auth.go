package store

import (
	"errors"
	"net"
	"strconv"
	"time"
)

var (
	ErrUserExists   = errors.New("user already exists")
	ErrUserNotFound = errors.New("user not found")
	ErrPeerNotFound = errors.New("peer not found")
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CanPost      bool      `json:"can_post"`
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"created_at"`
}

func (u User) IsAdmin() bool { return u.Role == RoleAdmin }
func (u User) MayPost() bool { return !u.Disabled && (u.CanPost || u.IsAdmin()) }

type Peer struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	PathToken      string    `json:"path_token"`
	IncomingHost   string    `json:"incoming_host"`
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	Patterns       string    `json:"patterns"`
	Distributions  string    `json:"distributions"`
	Flags          string    `json:"flags"`
	Enabled          bool      `json:"enabled"`
	IncomingPassword string    `json:"incoming_password,omitempty"`
	OutgoingPassword string    `json:"outgoing_password,omitempty"`
	Notes            string    `json:"notes"`
	Created          time.Time `json:"created_at"`
}

func (p Peer) Addr() string {
	port := p.Port
	if port <= 0 {
		port = 119
	}
	return net.JoinHostPort(p.Host, strconv.Itoa(port))
}

type ArchiveJob struct {
	ID       string    `json:"id"`
	Status   string    `json:"status"` // pending, running, done, error
	Selector string    `json:"selector"`
	Dir      string    `json:"dir"`
	Files    []string  `json:"files,omitempty"`
	Error    string    `json:"error,omitempty"`
	Started  time.Time `json:"started_at"`
	Finished time.Time `json:"finished_at,omitempty"`
	Groups   int       `json:"groups"`
	Articles int       `json:"articles"`
}
