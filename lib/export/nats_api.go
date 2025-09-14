package export

import (
	"time"

	"github.com/google/uuid"
)

const (
	// NatsApiSearchAirportV1 is the Nats API for searching for an airport
	NatsApiSearchAirportV1 = "v1.search.airport"
	// NatsApiSearchRouteV1 is teh Nats API for searching for route info
	NatsApiSearchRouteV1 = "v1.search.route"

	// NatsApiEnrichAircraftV1 is the Nats API for requesting additional Enrichment data
	NatsApiEnrichAircraftV1 = "v1.enrich.aircraft"
	// NatsApiEnrichRouteV1 is the request name for enriching a  route
	NatsApiEnrichRouteV1 = "v1.enrich.routes"

	NatsApiFeederListV1        = "v1.feeder.list"
	NatsApiFeederStatsUpdateV1 = "v1.feeder.update-stats"
)

type (
	Airport struct {
		Id        int64     `db:"id"`
		Name      string    `db:"name"`
		City      string    `db:"city"`
		Country   string    `db:"country"`
		IataCode  string    `db:"iata_code"`
		IcaoCode  string    `db:"icao_code"`
		Latitude  float64   `db:"latitude"`
		Longitude float64   `db:"longitude"`
		Altitude  int       `db:"altitude"`
		Timezone  int       `db:"timezone"`
		DstType   string    `db:"dst_type"`
		CreatedAt time.Time `db:"created_at"`
		UpdatedAt time.Time `db:"updated_at"`
	}

	AircraftResponse struct {
		Aircraft `json:"Aircraft"`
	}
	Aircraft struct {
		// Enrichment Plane data
		Icao            string  `db:"icao_code"`
		Country         *string `db:"country"`
		Registration    *string `db:"registration"`
		TypeCode        *string `db:"type_code"`
		TypeCodeLong    *string `db:"type_code_long"`
		Serial          *string `db:"serial"`
		RegisteredOwner *string `db:"registered_owner"`
		COFAOwner       *string `db:"cofa_owner"`
		EngineType      *string `db:"engine_type"`
		FlagCode        *string `db:"flag_code"`
	}

	RouteResponse struct {
		Route Route
	}
	Route struct {
		CallSign  string
		Operator  *string
		RouteCode *string
		Segments  []Segment
	}

	Feeders []Feeder
	Feeder  struct { // part of schema for /api/v1/feeders.json atc endpoint
		Id            int       `db:"id"`
		User          string    `db:"name"`
		Latitude      float64   `db:"latitude" json:",string"`
		Longitude     float64   `db:"longitude" json:",string"`
		Altitude      float64   `db:"altitude" json:",string"`
		ApiKey        uuid.UUID `db:"api_key"`
		FeedDirection string    `db:"feed_direction"`
		FeedProtocol  string    `db:"feed_protocol"`
		Label         string    `db:"label"`
		MlatEnabled   bool      `db:"mlat_enabled"`
		Mux           string    `db:"container_name"`
		FeederCode    string    `db:"feeder_code"`
	}

	FeederUpdates []FeederUpdate
	FeederUpdate  struct {
		ApiKey   string
		LastSeen time.Time
	}
)
