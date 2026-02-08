package main

import (
	"fmt"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/export"
)

var jsonFast = jsoniter.ConfigFastest

type (
	EnrichmentApiHandler struct {
		ApiHandler

		emptyAircraft []byte
		emptyRoutes   []byte
	}

	DbOperator struct {
		IcaoCode           *string `db:"icao_code"`
		IataCode           *string `db:"iata_code"`
		Name               *string `db:"name"`
		PositioningPattern *string `db:"positioning_pattern"`
		CharterPattern     *string `db:"charter_pattern"`
	}

	DbRoute struct {
		Id         int64  `db:"id"`
		CallSign   string `db:"callsign"`
		OperatorId int64  `db:"operator_id"`
	}

	DbRouteSegments struct {
		Name     string `db:"name"`
		IcaoCode string `db:"icao_code"`
	}
)

func newEnrichmentApi(idx int) *EnrichmentApiHandler {
	api := EnrichmentApiHandler{
		ApiHandler: ApiHandler{
			idx:     idx,
			name:    "enrichment",
			subject: "v1.enrich.*",
		},
	}
	api.handler = api.enrichHandler

	var err error
	if api.emptyAircraft, err = jsonFast.Marshal(export.AircraftResponse{}); err != nil {
		log.Fatal().Err(err).Msg("Failed to marshal empty aircraft response")
	}
	if api.emptyRoutes, err = jsonFast.Marshal(export.RouteResponse{}); err != nil {
		log.Fatal().Err(err).Msg("Failed to marshal empty route response")
	}

	return &api
}

func (sa *EnrichmentApiHandler) enrichHandler(msg *nats.Msg) {
	// capture how long we spend searching
	tStart := time.Now()
	defer func() {
		d := time.Since(tStart)
		prometheusCounterEnrichSummary.Observe(float64(d.Microseconds()))
	}()
	prometheusCounterEnrich.Inc()
	what := string(msg.Data)
	sa.log.Info().
		Str("subject", msg.Subject).
		Str("what", what).
		Msg("enrichment request")

	var respondErr error
	var buf []byte

	switch msg.Subject {
	case export.NatsApiEnrichAircraftV1:
		icao := strings.ToUpper(what)
		aircraft := export.AircraftResponse{}
		respondErr = db.Get(&aircraft.Aircraft, "SELECT icao_code,country,registration,type_code,type_code_long,serial,registered_owner,cofa_owner,engine_type,flag_code FROM aircraft WHERE icao_code = $1", icao)
		if nil == respondErr {
			buf, respondErr = jsonFast.Marshal(aircraft)
			if nil == respondErr {
				respondErr = msg.Respond(buf)
			} else {
				respondErr = msg.Respond(sa.emptyAircraft)
			}
		} else {
			sa.log.Error().Err(respondErr).Str("ICAO", icao).Msg("Failed to enrich aircraft")
			respondErr = msg.Respond(sa.emptyAircraft)
		}
	case export.NatsApiEnrichRouteV1:
		response := export.RouteResponse{}
		callSign := strings.ToUpper(what)
		response.Route.CallSign = callSign

		type routeRow struct {
			CallSign     string  `db:"callsign"`
			OperatorName *string `db:"operator_name"`
			AirportName  string  `db:"airport_name"`
			AirportICAO  string  `db:"airport_icao"`
		}

		var rows []routeRow
		respondErr = db.Select(&rows, `
			SELECT
    			r.callsign,
    			o.name AS operator_name,
    			COALESCE(a.name, '') AS airport_name,
    			COALESCE(a.icao_code, '') AS airport_icao
			FROM
			    routes r
			        LEFT JOIN operators o ON o.id = r.operator_id
			        LEFT JOIN route_segments rs ON rs.route_id = r.id
			        LEFT JOIN airports a ON a.id = rs.airport_id
			WHERE
			    r.callsign = $1
			ORDER BY rs."order"`,
			callSign)

		if nil == respondErr && len(rows) > 0 {
			response.Route.CallSign = rows[0].CallSign
			response.Route.Operator = rows[0].OperatorName

			icaoCodes := make([]string, 0, len(rows))
			response.Route.Segments = make([]export.Segment, 0, len(rows))
			for _, row := range rows {
				if row.AirportICAO == "" {
					continue
				}
				icaoCodes = append(icaoCodes, row.AirportICAO)
				response.Route.Segments = append(response.Route.Segments, export.Segment{
					Name:     row.AirportName,
					ICAOCode: row.AirportICAO,
				})
			}
			routeStr := strings.Join(icaoCodes, "-")
			response.Route.RouteCode = &routeStr
		} else if respondErr != nil {
			sa.log.Error().Err(respondErr).Str("call sign", callSign).Msg("Failed to enrich route")
		}

		if nil == respondErr {
			buf, respondErr = jsonFast.Marshal(response)
			if nil == respondErr {
				respondErr = msg.Respond(buf)
			} else {
				respondErr = msg.Respond(sa.emptyRoutes)
			}
		} else {
			respondErr = msg.Respond(sa.emptyRoutes)
		}
	default:
		respondErr = msg.Respond([]byte(fmt.Sprintf(ErrUnsupportedResponse, msg.Subject)))
	}

	if nil != respondErr {
		sa.log.Error().Err(respondErr).Msg("Failed sending reply")
	}
}
