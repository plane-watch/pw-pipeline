package main

import (
	"context"
	"math"

	jsoniter "github.com/json-iterator/go"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/export"
)

type (
	worker struct {
		router             *pwRouter
		destRoutingKeyLow  string
		destRoutingKeyHigh string
		spreadUpdates      bool

		ds *DataStream
	}
)

const SigHeadingChange = 1.0        // at least 1.0 degrees change.
const SigVerticalRateChange = 180.0 // at least 180 fpm change (3ft in 1min)
const SigAltitudeChange = 10.0      // at least 10 ft in altitude change.

func (w *worker) isSignificant(last, candidate export.PlaneLocation) bool {
	// check the candidate vs last, if any of the following have changed
	// - Heading, VerticalRate, Velocity, Altitude, FlightNumber, FlightStatus, OnGround, Special, Squawk

	sigLog := log.With().
		Str("aircraft", candidate.Icao).
		Dur("diff_time", candidate.LastMsg.Sub(last.LastMsg)).
		Logger()

	if candidate.HasOnGround && candidate.OnGround {
		if log.Debug().Enabled() {
			sigLog.Debug().
				Msg("Aircraft is on ground")
		}
		return true
	}

	// if any of these fields differ, indicate this update is significant
	if candidate.HasHeading && last.HasHeading && math.Abs(candidate.Heading-last.Heading) > SigHeadingChange {
		if candidate.Updates.Heading.After(last.Updates.Heading) {
			if log.Debug().Enabled() {
				sigLog.Debug().
					Float64("last", last.Heading).
					Float64("current", candidate.Heading).
					Float64("diff_value", last.Heading-candidate.Heading).
					Msg("Significant heading change.")
			}
			return true
		}
	}

	if candidate.HasVelocity && last.HasVelocity && candidate.Velocity != last.Velocity {
		if candidate.Updates.Velocity.After(last.Updates.Velocity) {
			if log.Debug().Enabled() {
				sigLog.Debug().
					Float64("last", last.Velocity).
					Float64("current", candidate.Velocity).
					Float64("diff_value", last.Velocity-candidate.Velocity).
					Msg("Significant velocity change.")
			}
			return true
		}
	}

	if candidate.HasVerticalRate && last.HasVerticalRate && math.Abs(float64(candidate.VerticalRate-last.VerticalRate)) > SigVerticalRateChange {
		if candidate.Updates.VerticalRate.After(last.Updates.VerticalRate) {
			if log.Debug().Enabled() {
				sigLog.Debug().
					Int("last", last.VerticalRate).
					Int("current", candidate.VerticalRate).
					Int("diff_value", last.VerticalRate-candidate.VerticalRate).
					Msg("Significant vertical rate change.")
			}
			return true
		}
	}

	if candidate.HasAltitude && last.HasAltitude && math.Abs(float64(candidate.Altitude-last.Altitude)) > SigAltitudeChange {
		if candidate.Updates.Altitude.After(last.Updates.Altitude) {
			if log.Debug().Enabled() {
				sigLog.Debug().
					Int("last", last.Altitude).
					Int("current", candidate.Altitude).
					Int("diff_value", last.Altitude-candidate.Altitude).
					Msg("Significant altitude change.")
			}
			return true
		}
	}

	if candidate.FlightStatus != last.FlightStatus {
		if candidate.Updates.FlightStatus.After(last.Updates.FlightStatus) {
			if log.Debug().Enabled() {
				sigLog.Debug().
					Str("last", last.FlightStatus).
					Str("current", candidate.FlightStatus).
					Msg("Significant FlightStatus change.")
			}
			return true
		}
	}

	if candidate.OnGround != last.OnGround {
		if candidate.Updates.OnGround.After(last.Updates.OnGround) {
			if log.Debug().Enabled() {
				sigLog.Debug().
					Bool("last", last.OnGround).
					Bool("current", candidate.OnGround).
					Msg("Significant OnGround change.")
			}
			return true
		}
	}

	if candidate.Special != last.Special {
		if candidate.Updates.Special.After(last.Updates.Special) {
			if log.Debug().Enabled() {
				sigLog.Debug().
					Str("last", last.Special).
					Str("current", candidate.Special).
					Msg("Significant Special change.")
			}
			return true
		}
	}

	if candidate.Squawk != last.Squawk {
		if candidate.Updates.Squawk.After(last.Updates.Squawk) {
			if log.Debug().Enabled() {
				sigLog.Debug().
					Str("last", last.Squawk).
					Str("current", candidate.Squawk).
					Msg("Significant Squawk change.")
			}
			return true
		}
	}

	if candidate.TileLocation != last.TileLocation {
		if candidate.Updates.Location.After(last.Updates.Location) {
			if log.Debug().Enabled() {
				sigLog.Debug().
					Str("last", last.TileLocation).
					Str("current", candidate.TileLocation).
					Msg("Significant TileLocation change.")
			}
			return true
		}
	}

	if log.Trace().Enabled() {
		sigLog.Trace().Msg("Ignoring insignificant event.")
	}

	return false
}

func (w *worker) run(ctx context.Context, ch <-chan []byte) {
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				log.Error().Msg("Worker ending due to error.")
				return
			}

			var gErr error
			if gErr = w.handleMsg(msg); nil != gErr {
				log.Error().Err(gErr).Send()
			}
		case <-ctx.Done():
			log.Debug().Msg("Ending Worker")
			return
		}
	}
}

func (w *worker) handleMsg(msg []byte) error {
	var err error

	var json = jsoniter.ConfigFastest
	// unmarshal the JSON and ensure it's valid.
	// report the error if not and skip this message.
	update := export.PlaneLocation{}
	if err = json.Unmarshal(msg, &update); nil != err {
		log.Error().Err(err).Msg("Unable to unmarshal JSON")
		updatesError.Inc()
		return err
	}

	if update.Icao == "" {
		log.Debug().Str("payload", string(msg)).Msg("empty ICAO")
		updatesError.Inc()
		return nil
	}

	updatesProcessed.Inc()

	// lookup what we know about this plane.
	item, ok := w.router.syncSamples.Load(update.Icao)

	// if this Icao is not in the cache, it's new.
	if !ok {
		update.InitSourceTags()
		update.IncSourceTag(update.SourceTag)
		w.router.syncSamples.Store(update.Icao, update)

		w.handleNewUpdate(update, msg)
		return nil // finish here, no significance check as we have nothing to compare.
	}

	// upstream signals that this plane has been removed / lost.
	if update.Removed {
		// TODO: we need to do our own reaping, since we can have multiple upstreams and one upstream losing track of
		// TODO: a plane does not mean it should be lost entirely
		//w.handleRemovedUpdate(update, msg)
		return nil // don't need to do anything else with this.
	}

	// is this update significant versus the previous one
	lastRecord := item.(export.PlaneLocation)
	merged, err := export.MergePlaneLocations(lastRecord, update)
	if nil != err {
		return nil
	}
	w.router.syncSamples.Store(merged.Icao, merged)

	mergedMsg, err := merged.ToJSONBytes()
	if nil != err {
		return err
	}

	if w.isSignificant(lastRecord, update) {
		w.handleSignificantUpdate(merged, mergedMsg)
	} else {
		w.handleInsignificantUpdate(merged, mergedMsg)
	}

	return nil
}

func (w *worker) handleRemovedUpdate(update export.PlaneLocation, msg []byte) {
	// check if this is a removed record and purge it from the cache and emit an event
	// this ensures downstream pipeline components always know about a removed record.
	// we get the removed flag from pw_ingest - this shortcuts our cache expiry for efficiency.
	w.router.syncSamples.Delete(update.Icao)
	cacheEntries.Dec()
	cacheEvictions.Inc()

	// emit the event to both queues
	w.publishLocationUpdate(w.destRoutingKeyLow, msg)  // to the reduced feed queue
	w.publishLocationUpdate(w.destRoutingKeyHigh, msg) // to the full-feed queue

	if w.spreadUpdates {
		w.publishLocationUpdate(update.TileLocation+qSuffixLow, msg)  // to the low-speed tile-queue.
		w.publishLocationUpdate(update.TileLocation+qSuffixHigh, msg) // to the high-speed tile-queue.
	}
}

func (w *worker) handleSignificantUpdate(update export.PlaneLocation, msg []byte) {
	// store the new update in-place of the old one
	// w.router.syncSamples.Store(update.Icao, update)
	updatesSignificant.Inc()

	// emit the new lastSignificant
	w.publishLocationUpdate(w.destRoutingKeyLow, msg)  // all low speed messages
	w.publishLocationUpdate(w.destRoutingKeyHigh, msg) // all high speed messages
	if w.spreadUpdates {
		w.publishLocationUpdate(update.TileLocation+qSuffixLow, msg)
		w.publishLocationUpdate(update.TileLocation+qSuffixHigh, msg)
	}
	if nil != w.ds {
		w.ds.AddLow(&update)
	}
}

func (w *worker) handleNewUpdate(update export.PlaneLocation, msg []byte) {
	// store the new update
	cacheEntries.Inc()

	log.Debug().
		Str("aircraft", update.Icao).
		Msg("First time seeing aircraft.")

	// new messages go to both queues
	w.publishLocationUpdate(w.destRoutingKeyLow, msg)  // all low speed messages
	w.publishLocationUpdate(w.destRoutingKeyHigh, msg) // all high speed messages

	// if spreading updates is enabled, output to spread queues
	if w.spreadUpdates {
		w.publishLocationUpdate(update.TileLocation+qSuffixLow, msg)
		w.publishLocationUpdate(update.TileLocation+qSuffixHigh, msg)
	}
}

func (w *worker) handleInsignificantUpdate(update export.PlaneLocation, msg []byte) {
	updatesInsignificant.Inc()

	w.publishLocationUpdate(w.destRoutingKeyHigh, msg) // all high speed messages

	if w.spreadUpdates {
		// always publish updates to the high queue.
		w.publishLocationUpdate(update.TileLocation+qSuffixHigh, msg)
	}

	if nil != w.ds {
		w.ds.AddHigh(&update)
	}
}

func (w *worker) publishLocationUpdate(routingKey string, msg []byte) {
	if log.Trace().Enabled() {
		log.Trace().Str("routing-key", routingKey).Bytes("Location", msg).Msg("Publish")
	}

	if err := w.router.nats.publish(routingKey, msg); nil != err {
		log.Warn().Err(err).Msg("Failed to send update")
		return
	}

	if log.Trace().Enabled() {
		log.Trace().Str("routingKey", routingKey).Msg("Sent msg")
	}
	updatesPublished.Inc()
}
