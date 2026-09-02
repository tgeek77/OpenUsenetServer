package store

import (
	"context"
	"time"
)

type Group struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"` // y, n, m
	Low         int64     `json:"low"`
	High        int64     `json:"high"`
	Count       int64     `json:"count"`
	CreatedAt   time.Time `json:"created_at"`
}

type OverviewRow struct {
	Num      int64
	Subject  string
	From     string
	Date     string
	MsgID    string
	Refs     string
	Bytes    int
	Lines    int
	Xref     string
	Header   string // for HDR: the requested header value
}

type StoredArticle struct {
	Num        int64 // 0 if retrieved by msgid without a selected group
	MessageID  string
	Headers    string
	Body       string
	Bytes      int
	Lines      int
	StoredAt   time.Time
	Xref       string
	Subject    string
	From       string
	Date       string
	Refs       string
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
	CountArticles(ctx context.Context) (int, error)
	RecentArticles(ctx context.Context, limit int) ([]StoredArticle, error)
	SearchGroups(ctx context.Context, query string, busyOnly bool, limit int) ([]Group, error)
	Post(ctx context.Context, headers, body, msgid, subject, from, date, refs, xref string, bytes, lines int, groups []string) (*PostResult, error)
	Next(ctx context.Context, group string, cur int64) (*StoredArticle, error)
	Prev(ctx context.Context, group string, cur int64) (*StoredArticle, error)
	Close() error
}
