package main

import (
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"github.com/nats-io/nats.go"
	"plane.watch/lib/export"
	"strings"
	"time"
)

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
	json := jsoniter.ConfigFastest

	api.emptyAircraft, _ = json.Marshal(export.AircraftResponse{})
	api.emptyRoutes, _ = json.Marshal(export.RouteResponse{})

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
			json := jsoniter.ConfigFastest
			buf, respondErr = json.Marshal(aircraft)
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
		route := DbRoute{}
		callSign := strings.ToUpper(what)
		respondErr = db.Get(&route, "SELECT id,operator_id,callsign from routes WHERE callsign = $1 LIMIT 1", callSign)
		if nil == respondErr {
			// we have a route!
			response.Route.CallSign = route.CallSign

			operator := DbOperator{}
			if err := db.Get(&operator, "SELECT name FROM operators WHERE id = $1", route.OperatorId); nil != err {
				sa.log.Error().Err(err).Send()
			}
			response.Route.Operator = operator.Name

			var segments []DbRouteSegments
			var routeStr string
			err := db.Select(&segments, `SELECT a.name,a.icao_code FROM route_segments rs left join airports a on a.id = rs.airport_id  WHERE route_id=$1 order by rs."order"`, route.Id)
			if err == nil {
				for _, segment := range segments {
					routeStr += segment.IcaoCode + "-"
					response.Route.Segments = append(response.Route.Segments, export.Segment{
						Name:     segment.Name,
						ICAOCode: segment.IcaoCode,
					})
				}
			} else {
				sa.log.Error().Err(err).Send()
			}
			routeStr = strings.Trim(routeStr, "-")
			response.Route.RouteCode = &routeStr
		} else {
			sa.log.Error().Err(respondErr).Str("call sign", callSign).Msg("Failed to enrich route")
		}
		if nil == respondErr {
			json := jsoniter.ConfigFastest
			buf, respondErr = json.Marshal(response)
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
		_ = msg.Respond([]byte(fmt.Sprintf(ErrRequestFailed, respondErr)))
	}
}
