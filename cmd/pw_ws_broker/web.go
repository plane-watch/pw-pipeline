package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	jsoniter "github.com/json-iterator/go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/exp/maps"

	"plane.watch/lib/dedupe/forgetfulmap"
	"plane.watch/lib/export"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/tile_grid"
	"plane.watch/lib/ws_protocol"
)

const MaxTickDuration = time.Second * 10

//go:embed test-web
var testWebDir embed.FS

var (
	gridJsonPayload     []byte
	gridJsonPayloadGzip []byte
	gridJsonPayloadETag string
)

func init() {
	var err error
	grid := tile_grid.GetGrid()
	json := jsoniter.ConfigCompatibleWithStandardLibrary
	gridJsonPayload, err = json.MarshalIndent(grid, "", "  ")
	if nil != err {
		log.Fatal().Err(err).Msg("Failed to json encode the tile grid")
	}
	gridJsonPayloadETag = fmt.Sprintf(`"%X"`, md5.Sum(gridJsonPayload))
	gridJsonPayloadGzip = mustGzipBytes(gridJsonPayload)
}

type (
	PwWsBrokerWeb struct {
		natsRpc   *nats_io.Server
		Addr      string
		ServeTest bool

		serveMux      http.ServeMux
		httpServer    http.Server
		cert, certKey string

		domainsToServe []string

		clients   *ClientList
		listening bool

		sendTickDuration time.Duration
	}

	loadedResponse struct {
		out ws_protocol.WsResponse

		highLow, tile string
	}

	WsClient struct {
		conn    *websocket.Conn
		outChan chan loadedResponse
		cmdChan chan WsCmd

		parent     *ClientList
		identifier string
		log        zerolog.Logger

		sendTickDuration time.Duration
	}
	WsCmd struct {
		action     ws_protocol.ProtocolRequest
		what       string
		extra      string
		tick       time.Duration
		locHistory []ws_protocol.LocationHistory
		results    ws_protocol.SearchResult
		list       []string
	}
	ClientList struct {
		//clients     map[*WsClient]chan ws_protocol.WsResponse
		clients sync.Map

		// naive approach
		//globalList sync.Map
		globalList *forgetfulmap.ForgetfulSyncMap

		broker *PwWsBrokerWeb
	}
)

// configureWeb Sets up our serve mux to handle our web endpoints
func (bw *PwWsBrokerWeb) configureWeb() error {
	bw.clients = newClientList(bw)

	bw.serveMux.HandleFunc("/", bw.indexPage)
	bw.serveMux.HandleFunc("/grid", bw.jsonGrid)
	bw.serveMux.HandleFunc("/planes", bw.servePlanes)

	if bw.ServeTest {
		bw.serveMux.Handle(
			"/test-web/",
			bw.logRequest(
				http.FileServer(http.FS(testWebDir)),
			),
		)
	}

	if "" != bw.certKey {
		log.Info().Str("cert", bw.cert).Msg("Using Certificate")
		tlsCert, err := tls.LoadX509KeyPair(bw.cert, bw.certKey)
		if nil != err {
			return err
		}
		x509Cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
		if nil != err {
			return err
		}
		for _, d := range x509Cert.DNSNames {
			bw.domainsToServe = append(bw.domainsToServe, d)
			bw.domainsToServe = append(bw.domainsToServe, d+":*")
		}
		if "" != x509Cert.Subject.CommonName {
			bw.domainsToServe = append(bw.domainsToServe, x509Cert.Subject.CommonName)
		}
	} else {
		bw.domainsToServe = []string{
			"localhost",
			"localhost:3000",
			"localhost:30001",
			"*plane.watch",
			"*plane.watch:3000",
			"*plane.watch:3001",
		}
	}
	log.Info().
		Int("# Domains", len(bw.domainsToServe)).
		Str("On Address", bw.Addr).
		Msg("Serving Domain Count")
	for _, d := range bw.domainsToServe {
		log.Info().Str("domain", d).Msg("Serving For Domain")
	}

	bw.httpServer = http.Server{
		Addr:         bw.Addr,
		Handler:      &bw.serveMux,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	return nil
}

// listenAndServe runs the top level serving and channel control
func (bw *PwWsBrokerWeb) listenAndServe(exitChan chan bool) {
	log.Info().Str("HttpAddr", bw.Addr).Msg("HTTP Listening on")
	bw.listening = true
	var err error
	isTls := false
	if "" != bw.cert {
		isTls = true
		err = bw.httpServer.ListenAndServeTLS(bw.cert, bw.certKey)
	} else {
		err = bw.httpServer.ListenAndServe()
	}
	if nil != err {
		bw.listening = false
		if err != http.ErrServerClosed {
			log.Error().
				Err(err).
				Bool("tls", isTls).
				Str("cert", bw.cert).
				Str("cert-key", bw.certKey).
				Msg("web server error")
		}
	}
	exitChan <- true
}

// logRequest logs all the web requests we get
func (bw *PwWsBrokerWeb) logRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug().Str("Remote", r.RemoteAddr).Str("Request", r.RequestURI).Msg("Web RQ")
		handler.ServeHTTP(w, r)
	})
}

// ServeHTTP Asks our internal serveMux to Serve HTTP web requests
func (bw *PwWsBrokerWeb) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bw.serveMux.ServeHTTP(w, r)
}

// indexPage gives people something to look at if they ask us for the index
func (bw *PwWsBrokerWeb) indexPage(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	_, _ = w.Write([]byte("Plane.Watch Websocket Broker"))
}

// jsonGrid Serves the grid that we base all our planes on
func (bw *PwWsBrokerWeb) jsonGrid(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("If-None-Match") == gridJsonPayloadETag {
		w.WriteHeader(304)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("ETag", gridJsonPayloadETag)
	w.Header().Set("Cache-Control", "public, max-age=86400")

	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.Itoa(len(gridJsonPayloadGzip)))
		_, _ = w.Write(gridJsonPayloadGzip)
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(gridJsonPayload)))
		_, _ = w.Write(gridJsonPayload)
	}
}

// search handles
func (cl *ClientList) performSearch(query string) ws_protocol.SearchResult {

	results := ws_protocol.SearchResult{
		Query:    query,
		Aircraft: []*export.PlaneLocation{},
		Airport:  []ws_protocol.AirportLocation{},
		Route:    []string{},
	}

	if len(query) >= 2 {
		// find any aircraft
		cl.globalList.Range(func(key, value interface{}) bool {
			loc := value.(*export.PlaneLocation)

			if strings.Contains(strings.ToLower(loc.Icao), query) ||
				(nil != loc.CallSign && strings.Contains(strings.ToLower(*loc.CallSign), query)) ||
				(nil != loc.RegisteredOwner && strings.Contains(strings.ToLower(*loc.RegisteredOwner), query)) ||
				(nil != loc.Registration && strings.Contains(strings.ToLower(*loc.Registration), query)) {
				results.Aircraft = append(results.Aircraft, loc)
			}

			return len(results.Aircraft) < 10
		})

		sort.Sort(results.Aircraft)
	}

	// Airport Lookup, if we have a nats connection
	if nil != cl.broker.natsRpc {
		headers := map[string]string{}
		resp, err := cl.broker.natsRpc.Request(export.NatsApiSearchAirportV1, []byte(query), headers, time.Second)
		if nil != err {
			log.Error().Err(err).Msg("Failed to search for airport")
		} else {
			json := jsoniter.ConfigFastest
			var airports []export.Airport
			err = json.Unmarshal(resp, &airports)
			if nil != err {
				log.Error().Err(err).Msg("Failed to unmarshal airport search results")
			} else {
				for _, airport := range airports {
					results.Airport = append(results.Airport, ws_protocol.AirportLocation{
						Name: airport.Name,
						Icao: airport.IcaoCode,
						Iata: airport.IataCode,
						Lat:  airport.Latitude,
						Lon:  airport.Longitude,
					})
				}
			}
		}
	}

	return results
}

// servePlanes Serves our Websocket Endpoint
func (bw *PwWsBrokerWeb) servePlanes(w http.ResponseWriter, r *http.Request) {
	log.Debug().Str("New Connection", r.RemoteAddr).Msg("New /planes WS")

	compress := r.URL.Query().Get("compress")
	wsCompression := websocket.CompressionContextTakeover

	if "false" == compress || "False" == compress {
		wsCompression = websocket.CompressionDisabled
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:       []string{ws_protocol.WsProtocolPlanes},
		InsecureSkipVerify: false,
		OriginPatterns:     bw.domainsToServe,
		CompressionMode:    wsCompression,
	})
	if nil != err {
		log.Error().Err(err).Msg("Failed to setup websocket connection")
		w.WriteHeader(500)
		_, _ = w.Write([]byte("Failed to setup websocket connection"))
		return
	}

	log.Debug().Str("protocol", conn.Subprotocol()).Msg("Speaking...")
	switch conn.Subprotocol() {
	case ws_protocol.WsProtocolPlanes:
		client := NewWsClient(conn, r.RemoteAddr, bw.sendTickDuration)
		bw.clients.addClient(client)
		client.Handle(r.Context())
		bw.clients.removeClient(client)
	default:
		_ = conn.Close(websocket.StatusPolicyViolation, "Unknown Sub Protocol")
		log.Debug().Str("proto", conn.Subprotocol()).Msg("Bad connection, could not speak protocol")
		return
	}
}

// HealthCheck allows us to check the health of this component
func (bw *PwWsBrokerWeb) HealthCheck() bool {
	log.Info().Bool("Web Listening", bw.listening).Msg("Health check")
	return bw.listening
}

// HealthCheckName Gives the name of this health check
func (bw *PwWsBrokerWeb) HealthCheckName() string {
	return "WS Broker Web"
}

// NewWsClient creates a new Websocket Client. This represents an individual connection and its handling
func NewWsClient(conn *websocket.Conn, identifier string, defaultSendTick time.Duration) *WsClient {
	client := WsClient{
		conn:             conn,
		cmdChan:          make(chan WsCmd),
		outChan:          make(chan loadedResponse, 500),
		identifier:       identifier,
		log:              log.With().Str("client", identifier).Logger(),
		sendTickDuration: defaultSendTick,
	}
	return &client
}

// Handle is a top level method that is called to Handle a websocket client connection
func (c *WsClient) Handle(ctx context.Context) {
	err := c.planeProtocolHandler(ctx, c.conn)
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure || websocket.CloseStatus(err) == websocket.StatusGoingAway {
		return
	}
	if nil != err {
		if -1 == websocket.CloseStatus(err) {
			c.log.Error().Err(err).Msg("Failure in protocol handler")
		}
		return
	}
}

// AddSub adds a "Please Subscribe this client to this tile" command to the clients command queue
func (c *WsClient) AddSub(tileName string) {
	c.log.Debug().Msg("Add Sub")
	c.cmdChan <- WsCmd{
		action: ws_protocol.RequestTypeSubscribe,
		what:   tileName,
	}
	c.cmdChan <- WsCmd{
		action: ws_protocol.RequestTypeGridPlanes,
		what:   tileName,
	}
	c.log.Debug().Msg("Add Sub Done")
}

// SetSubscribedTiles Sets the list of tiles that should subscribed right now
func (c *WsClient) SetSubscribedTiles(tileList []string) {
	c.log.Debug().Msg("Setting Tile List")
	c.cmdChan <- WsCmd{
		action: ws_protocol.RequestTypeSetSubscribedTiles,
		list:   tileList,
	}

	c.cmdChan <- WsCmd{
		action: ws_protocol.RequestTypeSendAllSubscribed,
	}

	c.log.Debug().Msg("Setting Tile List Done")
}

// UnSub adds a "Please remove this tile from the clients list" command to the clients command queue
func (c *WsClient) UnSub(tileName string) {
	c.log.Debug().Msg("Unsub")
	c.cmdChan <- WsCmd{
		action: ws_protocol.RequestTypeUnsubscribe,
		what:   tileName,
	}
	c.log.Debug().Msg("Unsub done")
}

// SendSubscribedTiles adds a "Please send the list of tiles that we are currently subscribed to" command to the queue
func (c *WsClient) SendSubscribedTiles() {
	c.log.Debug().Msg("Unsub")
	c.cmdChan <- WsCmd{
		action: ws_protocol.RequestTypeSubscribeList,
		what:   "",
	}
	c.log.Debug().Msg("Unsub done")
}

// SendTilePlanes adds a "send to the client the list of planes on the requested tile" command to the queue
func (c *WsClient) SendTilePlanes(tileName string) {
	c.log.Debug().Str("tile", tileName).Msg("Planes on Tile")
	c.cmdChan <- WsCmd{
		action: ws_protocol.RequestTypeGridPlanes,
		what:   tileName,
	}
}

// SendPlaneLocationHistory adds a "send the location history (from clickhouse) of the requested flight" command to the queue
func (c *WsClient) SendPlaneLocationHistory(icao, callSign string) {
	c.log.Debug().Str("icao", icao).Str("callSign", callSign).Msg("Request Flight Path")
	go func() {
		c.cmdChan <- WsCmd{
			action:     ws_protocol.RequestTypePlaneLocHistory,
			what:       icao,
			extra:      callSign,
			locHistory: GlobalClickHouseData.PlaneLocationHistory(icao, callSign),
		}
	}()
}

func (c *WsClient) AdjustSendTick(tick int) {
	if tick > 0 {
		tickDuration := time.Duration(tick) * time.Millisecond
		if tickDuration < MaxTickDuration {
			c.log.Debug().Dur("Requested Tick (ms)", tickDuration).Msg("Client Adjust Tick Timing")
			go func() {
				c.cmdChan <- WsCmd{
					action: ws_protocol.RequestTypeTickAdjust,
					tick:   tickDuration,
				}
			}()
		}
	}
}
func (c *WsClient) SendSearchResults(query string) {
	go func() {
		c.log.Debug().Str("Searching for", query).Msg("Performing Search")
		c.cmdChan <- WsCmd{
			action:  ws_protocol.RequestTypeSearch,
			what:    query,
			results: c.parent.performSearch(strings.ToLower(query)),
		}
	}()
}

// planeProtocolHandler handles our websocket protocol
// it runs the command queue that sends information to the client
// it runs (in a go routine) the reading of requests from the client.
// the read requests become commands in the command queue
func (c *WsClient) planeProtocolHandler(ctx context.Context, conn *websocket.Conn) error {
	// read from the connection for commands to perform
	// these get added to the queue to process
	json := jsoniter.ConfigFastest
	go func() {
		for {
			mt, frame, err := conn.Read(ctx)
			if nil != err {
				if !(errors.Is(err, io.EOF) || websocket.CloseStatus(err) >= 0) {
					log.Debug().Err(err).Int("Close Status", int(websocket.CloseStatus(err))).Msg("Error from reading")
				}
				c.cmdChan <- WsCmd{action: "exit"}
				return
			}
			switch mt {
			case websocket.MessageText:
				log.Debug().Bytes("Client Msg", frame).Msg("From Client")
				rq := ws_protocol.WsRequest{}
				if err = json.Unmarshal(frame, &rq); nil != err {
					log.Warn().Err(err).Msg("Failed to understand message from client")
				}
				switch rq.Type {
				case ws_protocol.RequestTypeSubscribe:
					c.AddSub(rq.GridTile)
				case ws_protocol.RequestTypeSubscribeList:
					c.SendSubscribedTiles()
				case ws_protocol.RequestTypeSetSubscribedTiles:
					c.SetSubscribedTiles(strings.Split(rq.GridTile, ","))
				case ws_protocol.RequestTypeUnsubscribe:
					c.UnSub(rq.GridTile)
				case ws_protocol.RequestTypeGridPlanes:
					c.SendTilePlanes(rq.GridTile)
				case ws_protocol.RequestTypePlaneLocHistory:
					c.SendPlaneLocationHistory(rq.Icao, rq.CallSign)
				case ws_protocol.RequestTypeTickAdjust:
					c.AdjustSendTick(rq.Tick)
				case ws_protocol.RequestTypeSearch:
					c.SendSearchResults(rq.Query)
				default:
					_ = c.sendError(ctx, "Unknown request type")
				}

			case websocket.MessageBinary:
				_ = c.sendError(ctx, "Please speak text")
			}
		}
	}()

	// write a stream of location information
	subs := make(map[string]struct{})

	grid := make(map[string]bool)
	gridNames := make(map[string]bool)
	gridNames[""] = true
	grid["all_low"] = true
	grid["all_high"] = true
	for k := range tile_grid.GetGrid() {
		gridNames[k] = true
		grid[k+"_low"] = true
		grid[k+"_high"] = true
	}

	locationMessages := make([]*export.PlaneLocation, 0, 1000)
	icaoIdLookup := make(map[string]int, 1000)

	currentSendTickDuration := c.sendTickDuration
	if 0 == currentSendTickDuration {
		// it still runs, but we do not send anything
		currentSendTickDuration = 10 * time.Second // something long enough that it is not much of an overhead
	}
	sendTick := time.NewTicker(currentSendTickDuration)
	defer sendTick.Stop()

	// this is the command processing main loop
	var err error
	for {
		select {
		case cmdMsg := <-c.cmdChan:
			switch cmdMsg.action {
			case "exit":
				return nil
			case ws_protocol.RequestTypeSubscribe:
				if _, ok := grid[cmdMsg.what]; ok {
					subs[cmdMsg.what] = struct{}{}
					err = c.sendAck(ctx, ws_protocol.ResponseTypeAckSub, cmdMsg.what)
					prometheusSubscriptions.WithLabelValues(cmdMsg.what).Inc()
				} else {
					err = c.sendError(ctx, "Unknown Tile: "+cmdMsg.what)
				}
			case ws_protocol.RequestTypeUnsubscribe:
				if _, ok := subs[cmdMsg.what]; ok {
					prometheusSubscriptions.WithLabelValues(cmdMsg.what).Dec()
					err = c.sendAck(ctx, ws_protocol.ResponseTypeAckUnsub, cmdMsg.what)
				} else {
					err = c.sendError(ctx, "Not Subbed to: "+cmdMsg.what)
				}
				delete(subs, cmdMsg.what)
			case ws_protocol.RequestTypeSetSubscribedTiles:
				clear(subs)
				for _, tile := range cmdMsg.list {
					if _, ok := grid[tile]; ok {
						subs[tile] = struct{}{}
					}
				}
				err = c.sendAck(ctx, ws_protocol.ResponseTypeAckUnsub, cmdMsg.what)
				prometheusSubscriptions.WithLabelValues(cmdMsg.what).Set(float64(len(subs)))
			case ws_protocol.RequestTypeSubscribeList:
				tiles := maps.Keys(subs)
				err = c.sendPlaneMessage(ctx, &ws_protocol.WsResponse{
					Type:  ws_protocol.ResponseTypeSubTiles,
					Tiles: tiles,
				})

			case ws_protocol.RequestTypeSendAllSubscribed:
				matching := 0
				// find all things currently in requested grid
				c.parent.globalList.Range(func(key, value interface{}) bool {
					loc := value.(*export.PlaneLocation)
					if _, subbed := subs[loc.TileLocation]; subbed {
						if id, ok := icaoIdLookup[loc.Icao]; ok {
							locationMessages[id] = loc
						} else {
							locationMessages = append(locationMessages, loc)
							icaoIdLookup[loc.Icao] = len(locationMessages) - 1
						}
						matching++
					}
					return true
				})

			case ws_protocol.RequestTypeGridPlanes:
				if _, gridOk := gridNames[cmdMsg.what]; gridOk {
					matching := 0
					// find all things currently in requested grid
					c.parent.globalList.Range(func(key, value interface{}) bool {
						loc := value.(*export.PlaneLocation)
						if cmdMsg.what == loc.TileLocation {
							if id, ok := icaoIdLookup[loc.Icao]; ok {
								locationMessages[id] = loc
							} else {
								locationMessages = append(locationMessages, loc)
								icaoIdLookup[loc.Icao] = len(locationMessages) - 1
							}
							matching++
						}
						return true
					})
				} else {
					err = c.sendError(ctx, "Unknown Tile: "+cmdMsg.what)
				}
			case ws_protocol.RequestTypePlaneLocHistory:
				err = c.sendPlaneMessage(ctx, &ws_protocol.WsResponse{
					Type:     ws_protocol.ResponseTypePlaneLocHistory,
					History:  cmdMsg.locHistory,
					Icao:     cmdMsg.what,
					CallSign: cmdMsg.extra,
				})
			case ws_protocol.RequestTypeTickAdjust:
				// this is already less than 10 seconds (MaxTickDuration)
				if cmdMsg.tick > c.sendTickDuration {
					currentSendTickDuration = cmdMsg.tick
				} else {
					// set the smallest allowed
					currentSendTickDuration = c.sendTickDuration
				}
				c.log.Info().Dur("Tick", currentSendTickDuration).Msg("Changing Tick Rate")
				sendTick.Reset(currentSendTickDuration)

				err = c.sendPlaneMessage(ctx, &ws_protocol.WsResponse{
					Type:    ws_protocol.ResponseTypeMsg,
					Message: fmt.Sprintf("Set Tick Rate To %s", currentSendTickDuration),
				})
			case ws_protocol.RequestTypeSearch:
				err = c.sendPlaneMessage(ctx, &ws_protocol.WsResponse{
					Type:    ws_protocol.ResponseTypeSearchResults,
					Results: &cmdMsg.results,
				})
			default:
				err = c.sendError(ctx, "Unknown Command")
			}
		case planeMsg := <-c.outChan:
			// if we have a subscription to this planes tile or all tiles
			// log.Debug().Str("tile", planeMsg.tile).Str("highlow", planeMsg.highLow).Msg("info")
			_, tileOk := subs[planeMsg.tile]
			_, allOk := subs["all"+planeMsg.highLow]
			if tileOk || allOk {
				if c.sendTickDuration > 0 {
					// limit our updates to only 1 per icao, sent periodically
					if id, ok := icaoIdLookup[planeMsg.out.Location.Icao]; ok {
						locationMessages[id] = planeMsg.out.Location
					} else {
						locationMessages = append(locationMessages, planeMsg.out.Location)
						icaoIdLookup[planeMsg.out.Location.Icao] = len(locationMessages) - 1
					}
				} else {
					err = c.sendPlaneMessage(ctx, &ws_protocol.WsResponse{
						Type:     ws_protocol.ResponseTypePlaneLocation,
						Location: planeMsg.out.Location,
					})
				}
			}
		case <-sendTick.C:
			if len(locationMessages) > 0 {
				err = c.sendPlaneMessage(ctx, &ws_protocol.WsResponse{
					Type:      ws_protocol.ResponseTypePlaneLocations,
					Locations: locationMessages,
				})
				// reset the slice and lookup map
				locationMessages = make([]*export.PlaneLocation, 0, 1000)
				clear(locationMessages)
				clear(icaoIdLookup)
			}
		}

		if nil != err {
			break
		}
	}

	// tell prometheus we are no longer caring about the tiles
	for k := range subs {
		prometheusSubscriptions.WithLabelValues(k).Dec()
	}
	return err
}

// sendAck sends an acknowledgement message to the client
func (c *WsClient) sendAck(ctx context.Context, ackType ws_protocol.ProtocolResponse, tile string) error {
	rs := ws_protocol.WsResponse{
		Type:  ackType,
		Tiles: []string{tile},
	}
	return c.sendPlaneMessage(ctx, &rs)
}

// sendError sends an error message to the client
func (c *WsClient) sendError(ctx context.Context, msg string) error {
	c.log.Error().Str("protocol", "error").Msg(msg)
	rs := ws_protocol.WsResponse{
		Type:    ws_protocol.ResponseTypeError,
		Message: msg,
	}
	return c.sendPlaneMessage(ctx, &rs)
}

// sendPlaneMessage sends a message to the client, with timeout
func (c *WsClient) sendPlaneMessage(ctx context.Context, planeMsg *ws_protocol.WsResponse) error {
	json := jsoniter.ConfigFastest
	buf, err := json.Marshal(planeMsg)
	if nil != err {
		c.log.Debug().Err(err).Str("type", planeMsg.Type.String()).Msg("Failed to marshal plane msg to send to client")
		return err
	}
	go func() {
		if err = c.writeTimeout(ctx, 3*time.Second, buf); nil != err {
			c.log.Debug().
				Err(err).
				Str("type", planeMsg.Type.String()).
				Msgf("Failed to send message to client. %+v", err)
		}
	}()
	return nil
}

// writeTimeout handles the writing of a message to the actual websocket connection
func (c *WsClient) writeTimeout(ctx context.Context, timeout time.Duration, msg []byte) error {
	ctxW, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	prometheusMessagesSent.Inc()
	prometheusMessagesSize.Add(float64(len(msg)))
	return c.conn.Write(ctxW, websocket.MessageText, msg)
}

// newClientList represents a list of websocket clients that we are currently servicing
func newClientList(bw *PwWsBrokerWeb) *ClientList {
	cl := ClientList{
		broker: bw,
	}
	cl.globalList = forgetfulmap.NewForgetfulSyncMap(
		forgetfulmap.WithPrometheusCounters(prometheusKnownPlanes),
		forgetfulmap.WithPreEvictionAction(func(key, value any) {
			log.Debug().Str("ICAO", key.(string)).Msg("Removing Aircraft due to inactivity")
		}),
		forgetfulmap.WithForgettableAction(func(key, value any, added time.Time) bool {
			result := true
			if loc, ok := value.(*export.PlaneLocation); ok {
				oldest := time.Now().Add(-10 * time.Minute)
				if loc.OnGround {
					// if a plane is on the ground, remove it 2 minutes after we last saw it
					oldest = time.Now().Add(-2 * time.Minute)
				}
				// TODO: determine if plane is ADS-C more robustly
				if loc.SourceTag == "ADS-C" {
					oldest = time.Now().Add(-time.Hour)
				}
				// remove  the plane from the list if it is older than our oldest allowable
				result = loc.LastMsg.Before(oldest)
			}
			// remove anything that is not a *export.PlaneLocation
			return result
		}),
	)

	return &cl
}

// addClient adds a websocket client to the list
func (cl *ClientList) addClient(c *WsClient) {
	c.log.Debug().Msg("Add Client")
	c.parent = cl
	cl.clients.Store(c, true)
	prometheusNumClients.Inc()
	//c.log.Debug().Msg("Add Client Done")
}

// removeClient removes a websocket client from the list
func (cl *ClientList) removeClient(c *WsClient) {
	c.log.Debug().Msg("Remove Client")
	close(c.outChan)
	cl.clients.Delete(c)
	prometheusNumClients.Dec()
	//log.Debug().Msg("Remove Client Done")
}

func (cl *ClientList) globalListUpdate(loc *export.PlaneLocation) {
	if nil == loc {
		return
	}
	cl.globalList.Store(loc.Icao, loc)
}

// SendLocationUpdate sends an update to each listening client
// todo: make this threaded?
func (cl *ClientList) SendLocationUpdate(highLow, tile string, loc *export.PlaneLocation) {
	// Add our update to our global list
	cl.globalListUpdate(loc)

	// send the update to each of our clients
	cl.clients.Range(func(key, value interface{}) bool {
		defer func() {
			if r := recover(); nil != r {
				log.Error().Msgf("Panic: %v", r)
			}
		}()
		client := key.(*WsClient)
		client.outChan <- loadedResponse{
			out: ws_protocol.WsResponse{
				Type:     ws_protocol.ResponseTypePlaneLocation,
				Location: loc,
			},
			highLow: highLow,
			tile:    tile,
		}
		return true
	})
}

// mustGzipBytes is a helper function that dies if there is an error GZIP'ing a byte stream
func mustGzipBytes(in []byte) []byte {
	// make a gzip version
	buf := bytes.Buffer{}
	gzw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if nil != err {
		log.Fatal().Err(err).Msg("Failed to create gzip writer")
	}
	_, err = gzw.Write(in)
	if nil != err {
		log.Fatal().Err(err).Msg("Failed to compress json")
	}

	if err = gzw.Close(); nil != err {
		log.Fatal().Err(err).Msg("Failed to gzip json")
	}

	return buf.Bytes()
}
