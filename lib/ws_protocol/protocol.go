package ws_protocol

import (
	"github.com/paulmach/orb"
	"plane.watch/lib/export"
)

const (
	WsProtocolPlanes = "planes"

	RequestTypeSubscribe          ProtocolRequest = "sub"
	RequestTypeSubscribeList      ProtocolRequest = "sub-list"
	RequestTypeSetSubscribedTiles ProtocolRequest = "set-sub-tile-list"
	RequestTypeSendAllSubscribed  ProtocolRequest = "send-all-subscribed-grid-planes"
	RequestTypeUnsubscribe        ProtocolRequest = "unsub"
	RequestTypeGridPlanes         ProtocolRequest = "grid-planes"            // returns the current plane locations in grid
	RequestTypePlaneLocHistory    ProtocolRequest = "plane-location-history" // returns the requested planes path
	RequestTypeTickAdjust         ProtocolRequest = "adjust-tick"            // adjusts how often we send updates
	RequestTypeSearch             ProtocolRequest = "search"                 // adjusts how often we send updates

	ResponseTypeError           ProtocolResponse = "error"
	ResponseTypeMsg             ProtocolResponse = "info"
	ResponseTypeAckSub          ProtocolResponse = "ack-sub"
	ResponseTypeAckUnsub        ProtocolResponse = "ack-unsub"
	ResponseTypeSubTiles        ProtocolResponse = "sub-list"
	ResponseTypePlaneLocation   ProtocolResponse = "plane-location"
	ResponseTypePlaneLocations  ProtocolResponse = "plane-location-list"
	ResponseTypePlaneLocHistory ProtocolResponse = "plane-location-history"
	ResponseTypeSearchResults   ProtocolResponse = "search-results"

	GridTileAllLow  = "all_low"
	GridTileAllHigh = "all_high"
)

type (
	ProtocolRequest  string
	ProtocolResponse string

	WsRequest struct {
		Type     ProtocolRequest `json:"type"`
		GridTile string          `json:"gridTile"`
		Icao     string          `json:"icao,omitempty"`
		CallSign string          `json:"callSign,omitempty"`
		Tick     int             `json:"tick,omitempty"`  // in Milliseconds
		Query    string          `json:"query,omitempty"` // in Milliseconds
	}
	LocationHistory struct {
		Lat, Lon          float64
		LatLon            orb.Point `json:"-"`
		Heading, Velocity float64
		Altitude          *int32
	}
	AircraftList []*export.PlaneLocation
	SearchResult struct {
		Query    string
		Aircraft AircraftList
		Airport  []AirportLocation
		Route    []string
	}
	AirportLocation struct {
		Name     string
		Icao     string
		Iata     string
		Lat, Lon float64
	}
	WsResponse struct {
		Type      ProtocolResponse        `json:"type"`
		Message   string                  `json:"message,omitempty"`
		Tiles     []string                `json:"tiles,omitempty"`
		Location  *export.PlaneLocation   `json:"location,omitempty"`
		Locations []*export.PlaneLocation `json:"locations,omitempty"`

		Icao     string            `json:"icao,omitempty"`
		CallSign string            `json:"callSign,omitempty"`
		History  []LocationHistory `json:"history,omitempty"`
		Results  *SearchResult     `json:"results,omitempty"`
	}
)

func (l AircraftList) Len() int {
	if nil == l {
		return 0
	}
	return len(l)
}
func unPtr[t any](what *t) t {
	var def t
	if nil == what {
		return def
	}
	return *what
}

func (l AircraftList) Less(i, j int) bool {
	if nil == l {
		return false
	}

	left := unPtr(l[i].CallSign) + ":" + unPtr(l[i].Registration) + ":" + l[i].Icao
	right := unPtr(l[j].CallSign) + ":" + unPtr(l[j].Registration) + ":" + l[j].Icao

	return left < right
}

func (l AircraftList) Swap(i, j int) {
	x := l[i]
	l[i] = l[j]
	l[j] = x
}

func (pr ProtocolRequest) String() string {
	return string(pr)
}

func (pr ProtocolResponse) String() string {
	return string(pr)
}
