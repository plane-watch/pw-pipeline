package timing

import (
	"time"

	"github.com/rs/zerolog"
)

// PerformOnTicker runs function f ever t duration
func PerformOnTicker(logger zerolog.Logger, t time.Duration, f func() error) func() {
	ticker := time.NewTicker(t)
	tickerFinished := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := f(); err != nil {
					logger.Error().Err(err).Msg("Failed to perform on ticker")
				}
			case <-tickerFinished:
				return
			}
		}
	}()
	return func() {
		ticker.Stop()
		tickerFinished <- struct{}{}
	}
}
