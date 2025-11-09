package main

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	jsoniter "github.com/json-iterator/go"
	"github.com/nats-io/nats.go"
	"plane.watch/lib/export"
)

type (
	FeederApiHandler struct {
		ApiHandler
	}

	feederQueryRow struct { //
		Id            int64     `db:"id"`
		User          string    `db:"name"`
		Latitude      *float64  `db:"latitude"`
		Longitude     *float64  `db:"longitude"`
		Altitude      *float64  `db:"altitude"`
		ApiKey        uuid.UUID `db:"api_key"`
		FeedDirection *int      `db:"feed_direction"`
		FeedProtocol  *int      `db:"feed_protocol"`
		Label         *string   `db:"label"`
		MlatEnabled   *bool     `db:"mlat_enabled"`
		FeederCode    *string   `db:"feeder_code"`
		Mux           *string   `db:"container_name"`
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
		queryRows := make([]feederQueryRow, 0)

		respondErr = db.Select(&queryRows, `
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

		if respondErr != nil {
			sa.log.Error().Err(respondErr).Msg("failed to fetch feeders")
		} else {
			sa.log.Info().Int("count", len(queryRows)).Msg("fetched feeders via db query")

			feeders := make(export.Feeders, 0, len(queryRows))

			for _, row := range queryRows {
				feeders = append(feeders, export.Feeder{
					MlatEnabled:   unPtr(row.MlatEnabled),
					Id:            row.Id,
					Latitude:      row.Latitude,
					Longitude:     row.Longitude,
					Altitude:      row.Altitude,
					User:          row.User,
					FeedDirection: unPtr(row.FeedDirection),
					FeedProtocol:  unPtr(row.FeedProtocol),
					Label:         unPtr(row.Label),
					Mux:           unPtr(row.Mux),
					FeederCode:    unPtr(row.FeederCode),
					ApiKey:        row.ApiKey,
				})
			}

			buf, respondErr = json.Marshal(feeders)
			if respondErr == nil {
				respondErr = msg.Respond(buf)
			}
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

	if respondErr != nil {
		sa.log.Error().Err(respondErr).Msg("Failed sending reply")
		_ = msg.Respond([]byte(fmt.Sprintf(ErrRequestFailed, respondErr)))
	}
}

func unPtr[T comparable](in *T) T {
	var defaultVal T
	if nil == in {
		return defaultVal
	}
	return *in
}
