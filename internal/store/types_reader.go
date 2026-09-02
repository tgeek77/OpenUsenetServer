package store

import "time"

// Subscription is a user's subscribed newsgroup plus live group stats.
type Subscription struct {
	GroupName     string    `json:"group"`
	SubscribedAt  time.Time `json:"subscribed_at"`
	Description   string    `json:"description,omitempty"`
	Status        string    `json:"status,omitempty"`
	Low           int64     `json:"low"`
	High          int64     `json:"high"`
	Count         int64     `json:"count"`
	LastReadNum   int64     `json:"last_read_num"`
	Unread        int64     `json:"unread"`
}
