package retention

import (
	"time"

	"openusenet/internal/config"
	"openusenet/internal/store"
)

// FloodFromConfig builds store flood params from server config.
func FloodFromConfig(cfg config.Config) store.FloodParams {
	r := cfg.Retention
	return store.FloodParams{
		Window:      time.Duration(r.Flood.WindowHours) * time.Hour,
		MinBinary:   r.Flood.MinBinary,
		MinRatio:    r.Flood.MinBinaryRatio,
		FloodDays:   r.FloodLiveDays,
		SeedWildmat: r.SeedBinaryWildmat,
	}
}
