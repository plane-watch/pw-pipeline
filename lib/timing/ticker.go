package timing

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// RunOnTicker runs function f ever t duration, until the returned function is called.
func RunOnTicker(logger zerolog.Logger, t time.Duration, f func() error) context.CancelFunc {
	logger = logger.With().
		Str("component", "timing.RunOnTicker").
		Str("interval", t.String()).
		Logger()
	ctx, cancel := context.WithCancel(context.Background())
	ticker := time.NewTicker(t)
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := f(); err != nil {
					logger.Error().Err(err).Msg("function returned error")
				}
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
	return cancel
}
