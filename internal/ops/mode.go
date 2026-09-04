// Package ops provides runtime server control similar to ctlinnd mode/pause/go
// and innwatch-style automatic throttling.
package ops

import (
	"sync"
	"time"
)

// Mode is the server acceptance mode.
type Mode string

const (
	ModeRunning   Mode = "running"
	ModePaused    Mode = "paused"
	ModeThrottled Mode = "throttled"
)

// Snapshot is a point-in-time view of runtime gates.
type Snapshot struct {
	Mode            Mode      `json:"mode"`
	Reason          string    `json:"reason"`
	ChangedAt       time.Time `json:"changed_at"`
	ChangedBy       string    `json:"changed_by"` // admin | watchdog
	ReadersEnabled  bool      `json:"readers_enabled"`
	ReadersMessage  string    `json:"readers_message"`
	PeersEnabled    bool      `json:"peers_enabled"`
	PeersMessage    string    `json:"peers_message"`
	WatchdogEnabled bool      `json:"watchdog_enabled"`
	LastWatch       time.Time `json:"last_watch,omitempty"`
	DiskUsedPct     float64   `json:"disk_used_pct,omitempty"`
	Load1           float64   `json:"load1,omitempty"`
	FeedQueueDepth  int       `json:"feed_queue_depth"`
	ExpireLast      time.Time `json:"expire_last,omitempty"`
	ExpireResult    any       `json:"expire_result,omitempty"`
}

// Controller holds live pause/throttle/reader/peer gates.
type Controller struct {
	mu sync.RWMutex

	mode           Mode
	reason         string
	changedAt      time.Time
	changedBy      string
	readersOn      bool
	readersMsg     string
	peersOn        bool
	peersMsg       string
	watchdogOn     bool
	lastWatch      time.Time
	diskUsedPct    float64
	load1          float64
	feedQueueDepth int
	expireLast     time.Time
	expireResult   any
}

func New() *Controller {
	return &Controller{
		mode:       ModeRunning,
		changedAt:  time.Now().UTC(),
		changedBy:  "startup",
		readersOn:  true,
		peersOn:    true,
		watchdogOn: true,
	}
}

func (c *Controller) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Snapshot{
		Mode:            c.mode,
		Reason:          c.reason,
		ChangedAt:       c.changedAt,
		ChangedBy:       c.changedBy,
		ReadersEnabled:  c.readersOn,
		ReadersMessage:  c.readersMsg,
		PeersEnabled:    c.peersOn,
		PeersMessage:    c.peersMsg,
		WatchdogEnabled: c.watchdogOn,
		LastWatch:       c.lastWatch,
		DiskUsedPct:     c.diskUsedPct,
		Load1:           c.load1,
		FeedQueueDepth:  c.feedQueueDepth,
		ExpireLast:      c.expireLast,
		ExpireResult:    c.expireResult,
	}
}

func (c *Controller) setMode(mode Mode, reason, by string) {
	c.mode = mode
	c.reason = reason
	c.changedAt = time.Now().UTC()
	c.changedBy = by
}

// Pause refuses new article accepts while keeping connections up.
func (c *Controller) Pause(reason, by string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if reason == "" {
		reason = "paused"
	}
	c.setMode(ModePaused, reason, by)
}

// Throttle refuses article accepts (and peer feeds when peers gate is on).
func (c *Controller) Throttle(reason, by string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if reason == "" {
		reason = "throttled"
	}
	c.setMode(ModeThrottled, reason, by)
}

// Go resumes normal operation.
func (c *Controller) Go(by string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setMode(ModeRunning, "", by)
}

func (c *Controller) SetReaders(enabled bool, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readersOn = enabled
	c.readersMsg = message
}

func (c *Controller) SetPeers(enabled bool, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.peersOn = enabled
	c.peersMsg = message
}

func (c *Controller) SetWatchdogEnabled(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.watchdogOn = on
}

func (c *Controller) WatchdogEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.watchdogOn
}

func (c *Controller) NoteWatch(load1, diskPct float64, queueDepth int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastWatch = time.Now().UTC()
	c.load1 = load1
	c.diskUsedPct = diskPct
	c.feedQueueDepth = queueDepth
}

func (c *Controller) NoteExpire(res any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLast = time.Now().UTC()
	c.expireResult = res
}

// AcceptArticles reports whether POST/IHAVE should accept new articles.
func (c *Controller) AcceptArticles() (ok bool, reason string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	switch c.mode {
	case ModePaused, ModeThrottled:
		return false, c.reason
	default:
		return true, ""
	}
}

// AcceptPeers reports whether inbound peer feeds (IHAVE) are allowed.
func (c *Controller) AcceptPeers() (ok bool, reason string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.peersOn {
		msg := c.peersMsg
		if msg == "" {
			msg = "peer feeds disabled"
		}
		return false, msg
	}
	if c.mode == ModeThrottled {
		return false, c.reason
	}
	return true, ""
}

// AcceptReaders reports whether reader sessions may proceed past greeting.
func (c *Controller) AcceptReaders() (ok bool, reason string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.readersOn {
		msg := c.readersMsg
		if msg == "" {
			msg = "readers disabled"
		}
		return false, msg
	}
	return true, ""
}
