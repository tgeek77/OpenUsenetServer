package store

import (
	"context"
	"errors"
	"time"
)

type Group struct {
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Status        string    `json:"status"` // y, n, m
	Low           int64     `json:"low"`
	High          int64     `json:"high"`
	Count         int64     `json:"count"`
	CreatedAt     time.Time `json:"created_at"`
	RetentionDays *int      `json:"retention_days,omitempty"` // nil = inherit forever default
	RetentionMode string    `json:"retention_mode,omitempty"` // auto | whitelist
}

const (
	RetentionModeAuto      = "auto"
	RetentionModeWhitelist = "whitelist"
)

// GroupAlert is an admin review item (e.g. binary flood).
type GroupAlert struct {
	ID        int64     `json:"id"`
	GroupName string    `json:"group_name"`
	Kind      string    `json:"kind"`
	Detail    string    `json:"detail"`
	Status    string    `json:"status"` // open | blocked | whitelisted | dismissed
	CreatedAt time.Time `json:"created_at"`
}

const (
	AlertKindBinaryFlood = "binary_flood"
	AlertOpen            = "open"
	AlertBlocked         = "blocked"
	AlertWhitelisted     = "whitelisted"
	AlertDismissed       = "dismissed"
)

// FloodParams controls auto short-retention when a group is binary-flooded.
type FloodParams struct {
	Window      time.Duration
	MinBinary   int
	MinRatio    float64
	FloodDays   int
	SeedWildmat string
}

// ExpireResult summarizes a retention pass.
type ExpireResult struct {
	OverviewRemoved int `json:"overview_removed"`
	ArticlesRemoved int `json:"articles_removed"`
	HistoryRemoved  int `json:"history_removed"`
}

// FeedQueueItem is a pending outbound IHAVE offer.
type FeedQueueItem struct {
	ID          int64     `json:"id"`
	PeerID      int64     `json:"peer_id"`
	MessageID   string    `json:"message_id"`
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt"`
	LastError   string    `json:"last_error"`
	CreatedAt   time.Time `json:"created_at"`
}

// FeedQueueStats summarizes outbound backlog.
type FeedQueueStats struct {
	Depth     int            `json:"depth"`
	ByPeer    map[int64]int  `json:"by_peer,omitempty"`
	OldestAge time.Duration  `json:"oldest_age_ns"`
}

var (
	ErrQuotaExceeded = errors.New("binary post quota exceeded")
)

type OverviewRow struct {
	Num     int64  `json:"num"`
	Subject string `json:"subject"`
	From    string `json:"from"`
	Date    string `json:"date"`
	MsgID   string `json:"message_id"`
	Refs    string `json:"references"`
	Bytes   int    `json:"bytes"`
	Lines   int    `json:"lines"`
	Xref    string `json:"xref"`
	Header  string `json:"header,omitempty"` // for HDR: the requested header value
}

type StoredArticle struct {
	Num       int64 // 0 if retrieved by msgid without a selected group
	MessageID string
	Headers   string
	Body      string
	Bytes     int
	Lines     int
	StoredAt  time.Time
	Xref      string
	Subject   string
	From      string
	Date      string
	Refs      string
}

type PostResult struct {
	MessageID string
	Xref      string
	Numbers   map[string]int64 // group -> article number
}

type Store interface {
	EnsureGroup(ctx context.Context, name, desc, status string) error
	EnsureGroups(ctx context.Context, groups []Group) error
	CountGroups(ctx context.Context) (int, error)
	ListGroups(ctx context.Context, wildmat string) ([]Group, error)
	GetGroup(ctx context.Context, name string) (*Group, error)
	ArticleNumbers(ctx context.Context, group string, lo, hi int64) ([]int64, error)
	GetByNumber(ctx context.Context, group string, num int64) (*StoredArticle, error)
	GetByMsgID(ctx context.Context, msgid string) (*StoredArticle, error)
	Overview(ctx context.Context, group string, lo, hi int64) ([]OverviewRow, error)
	Header(ctx context.Context, group string, lo, hi int64, header string) ([]OverviewRow, error)
	NewNews(ctx context.Context, wildmat string, since time.Time) ([]string, error)
	NewGroups(ctx context.Context, since time.Time) ([]Group, error)
	HasMessageID(ctx context.Context, msgid string) (bool, error)
	RememberMessageID(ctx context.Context, msgid string) error
	CancelMessageID(ctx context.Context, msgid string) (bool, error)
	DeleteGroup(ctx context.Context, name string) error
	CountArticles(ctx context.Context) (int, error)
	RecentArticles(ctx context.Context, limit int) ([]StoredArticle, error)
	SearchGroups(ctx context.Context, query string, busyOnly bool, limit int) ([]Group, error)
	Post(ctx context.Context, headers, body, msgid, subject, from, date, refs, xref string, bytes, lines int, groups []string) (*PostResult, error)
	Next(ctx context.Context, group string, cur int64) (*StoredArticle, error)
	Prev(ctx context.Context, group string, cur int64) (*StoredArticle, error)

	CountUsers(ctx context.Context) (int, error)
	ListUsers(ctx context.Context) ([]User, error)
	GetUser(ctx context.Context, username string) (*User, error)
	CreateUser(ctx context.Context, u User) (*User, error)
	UpdateUser(ctx context.Context, username string, role string, canPost, disabled *bool, passwordHash string) error
	DeleteUser(ctx context.Context, username string) error

	ListPeers(ctx context.Context) ([]Peer, error)
	ListEnabledPeers(ctx context.Context) ([]Peer, error)
	GetPeer(ctx context.Context, id int64) (*Peer, error)
	CreatePeer(ctx context.Context, p Peer) (*Peer, error)
	UpdatePeer(ctx context.Context, peer Peer) (*Peer, error)
	DeletePeer(ctx context.Context, id int64) error
	CountPeers(ctx context.Context) (int, error)

	EnqueueFeed(ctx context.Context, peerID int64, msgid string) error
	ClaimFeedDue(ctx context.Context, limit int) ([]FeedQueueItem, error)
	CompleteFeed(ctx context.Context, id int64) error
	FailFeed(ctx context.Context, id int64, errMsg string, retryAfter time.Duration) error
	FlushFeedQueue(ctx context.Context, peerID int64) (int, error)
	FeedQueueStats(ctx context.Context) (FeedQueueStats, error)

	ArticlesForGroup(ctx context.Context, group string) ([]StoredArticle, error)

	ListSubscriptions(ctx context.Context, userID int64) ([]Subscription, error)
	Subscribe(ctx context.Context, userID int64, group string) error
	Unsubscribe(ctx context.Context, userID int64, group string) error
	GetReadState(ctx context.Context, userID int64, group string) (int64, error)
	SetReadState(ctx context.Context, userID int64, group string, lastReadNum int64) error

	// Retention / binary flood / user quota
	NoteAccept(ctx context.Context, groups []string, binary bool, flood FloodParams) ([]string, error)
	ConsumeBinaryPostQuota(ctx context.Context, userID int64, limit int) (used int, err error)
	BinaryPostQuotaUsed(ctx context.Context, userID int64) (int, error)
	ListGroupAlerts(ctx context.Context, status string) ([]GroupAlert, error)
	ResolveGroupAlert(ctx context.Context, id int64, action string) error
	SetGroupRetention(ctx context.Context, name string, days *int, mode string) error
	Expire(ctx context.Context, historyDays int) (ExpireResult, error)

	Close() error
}
