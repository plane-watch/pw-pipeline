package main

import (
	"fmt"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/nats-io/nats.go"
	"plane.watch/lib/export"
)

type (
	FeederApiHandler struct {
		ApiHandler
	}
)

func newFeederApi(idx int) *FeederApiHandler {
	api := FeederApiHandler{
		ApiHandler: ApiHandler{
			idx:     idx,
			name:    "feeder",
			subject: "v1.feeder.*",
		},
	}
	api.handler = api.feederHandler

	return &api
}

func (sa *FeederApiHandler) feederHandler(msg *nats.Msg) {
	// capture how long we spend searching
	tStart := time.Now()
	defer func() {
		d := time.Since(tStart)
		prometheusCounterFeederSummary.Observe(float64(d.Microseconds()))
	}()
	prometheusCounterFeeder.Inc()
	what := string(msg.Data)
	sa.log.Info().
		Str("subject", msg.Subject).
		Str("what", what).
		Msg("feeder request")

	var respondErr error
	var buf []byte
	json := jsoniter.ConfigFastest

	switch msg.Subject {
	case export.NatsApiFeederListV1:
		feeders := make(export.Feeders, 0)

		respondErr = db.Select(&feeders, `
SELECT
	f.id,
	concat_ws(' ', u.first_name, u.last_name) AS name,
	f.latitude,
	f.longitude,
	f.altitude,
	f.api_key,
	f.feed_direction,
	f.feed_protocol,
	f.label,
	f.mlat_enabled,
	f.feeder_code,
	concat('mux-#', LOWER(fm.name)) as container_name
FROM feeders f
    LEFT JOIN users u on f.user_id = u.id
    LEFT JOIN feeder_muxes fm on f.feeder_mux_id = fm.id`)

		buf, respondErr = json.Marshal(feeders)
		if nil == respondErr {
			respondErr = msg.Respond(buf)
		}

	case export.NatsApiFeederStatsUpdateV1:
		updates := make(export.FeederUpdates, 0)
		respondErr = json.Unmarshal(msg.Data, &updates)

		if nil == respondErr {
			for _, update := range updates {
				// db update last seen
				if _, err := db.Exec("UPDATE feeders SET last_seen=$1 WHERE api_key=$2", update.LastSeen, update.ApiKey); nil != err {
					sa.log.Error().
						Err(err).
						Time("last seen", update.LastSeen).
						Str("Api Key", update.ApiKey).
						Msg("Failed update last seen")
				}
			}
		}

		respondErr = msg.Respond(buf)

	default:
		respondErr = msg.Respond([]byte(fmt.Sprintf(ErrUnsupportedResponse, msg.Subject)))
	}

	if nil != respondErr {
		sa.log.Error().Err(respondErr).Msg("Failed sending reply")
		_ = msg.Respond([]byte(fmt.Sprintf(ErrRequestFailed, respondErr)))
	}
}
