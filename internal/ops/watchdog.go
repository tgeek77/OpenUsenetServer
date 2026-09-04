package ops

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"openusenet/internal/config"
	"openusenet/internal/store"
)

// Watchdog evaluates load/disk/queue like innwatch and adjusts mode.
type Watchdog struct {
	cfg  config.Watchdog
	ctl  *Controller
	st   store.Store
	path string // filesystem path to measure (mbox dir)
	log  *log.Logger
}

func NewWatchdog(cfg config.Watchdog, ctl *Controller, st store.Store, path string, lg *log.Logger) *Watchdog {
	if lg == nil {
		lg = log.Default()
	}
	return &Watchdog{cfg: cfg, ctl: ctl, st: st, path: path, log: lg}
}

func (w *Watchdog) Run(ctx context.Context) {
	if w == nil || w.ctl == nil {
		return
	}
	every := time.Duration(w.cfg.IntervalSeconds) * time.Second
	if every <= 0 {
		every = 60 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	w.tick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick()
		}
	}
}

func (w *Watchdog) tick() {
	if !w.ctl.WatchdogEnabled() || !w.cfg.IsEnabled() {
		return
	}
	load1 := readLoad1()
	diskPct := diskUsedPercent(w.path)
	qDepth := 0
	if w.st != nil {
		if st, err := w.st.FeedQueueStats(context.Background()); err == nil {
			qDepth = st.Depth
		}
	}
	w.ctl.NoteWatch(load1, diskPct, qDepth)

	snap := w.ctl.Snapshot()
	// Only auto-change when last change was by watchdog or startup, or when escalating.
	needThrottle := false
	needPause := false
	reason := ""

	if w.cfg.DiskThrottlePct > 0 && diskPct >= float64(w.cfg.DiskThrottlePct) {
		needThrottle = true
		reason = "disk space low"
	}
	if w.cfg.QueueThrottle > 0 && qDepth >= w.cfg.QueueThrottle {
		needThrottle = true
		reason = "feed queue depth"
	}
	if w.cfg.LoadThrottle > 0 && load1 >= w.cfg.LoadThrottle {
		needThrottle = true
		reason = "load average high"
	} else if w.cfg.LoadPause > 0 && load1 >= w.cfg.LoadPause {
		needPause = true
		reason = "load average elevated"
	}

	switch {
	case needThrottle:
		if snap.Mode != ModeThrottled || snap.ChangedBy == "admin" && snap.Mode == ModeThrottled {
			// Don't override an admin throttle with a different reason unless escalating from running/paused.
		}
		if snap.ChangedBy == "admin" && (snap.Mode == ModePaused || snap.Mode == ModeThrottled) {
			return // honor manual control
		}
		w.ctl.Throttle(reason, "watchdog")
		w.log.Printf("watchdog throttle: %s (load=%.2f disk=%.1f%% queue=%d)", reason, load1, diskPct, qDepth)
	case needPause:
		if snap.ChangedBy == "admin" && (snap.Mode == ModePaused || snap.Mode == ModeThrottled) {
			return
		}
		if snap.Mode == ModeThrottled && snap.ChangedBy == "watchdog" {
			// Stay throttled until load drops to go threshold.
			if w.cfg.LoadGo > 0 && load1 <= w.cfg.LoadGo &&
				(w.cfg.DiskThrottlePct == 0 || diskPct < float64(w.cfg.DiskThrottlePct)-2) &&
				(w.cfg.QueueThrottle == 0 || qDepth < w.cfg.QueueThrottle/2) {
				w.ctl.Go("watchdog")
				w.log.Printf("watchdog go (from throttle)")
			}
			return
		}
		w.ctl.Pause(reason, "watchdog")
		w.log.Printf("watchdog pause: %s (load=%.2f)", reason, load1)
	default:
		if snap.ChangedBy != "watchdog" {
			return
		}
		if snap.Mode == ModeRunning {
			return
		}
		loadOK := w.cfg.LoadGo <= 0 || load1 <= w.cfg.LoadGo
		diskOK := w.cfg.DiskThrottlePct <= 0 || diskPct < float64(w.cfg.DiskThrottlePct)-2
		queueOK := w.cfg.QueueThrottle <= 0 || qDepth < w.cfg.QueueThrottle/2
		if loadOK && diskOK && queueOK {
			w.ctl.Go("watchdog")
			w.log.Printf("watchdog go (load=%.2f disk=%.1f%% queue=%d)", load1, diskPct, qDepth)
		}
	}
}

func readLoad1() float64 {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 1 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func diskUsedPercent(path string) float64 {
	if path == "" {
		path = "."
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	total := float64(st.Blocks) * float64(st.Bsize)
	if total <= 0 {
		return 0
	}
	free := float64(st.Bavail) * float64(st.Bsize)
	return 100 * (1 - free/total)
}
