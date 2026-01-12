package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/feederauth"
	"plane.watch/lib/haproxy"
	"plane.watch/lib/timing"
)

type (

	// ConnType: BEAST or MLAT
	ConnType uint8

	// Connection holds information about a BEAST or MLAT connection
	Connection struct {
		connType      ConnType
		Src           net.TCPAddr   // Source IP & Port
		srcIP         string        // internal
		srcPort       int           // internal
		BytesIn       uint64        // Bytes sent from client to server
		BytesOut      uint64        // Bytes sent from server to client
		Duration      time.Duration // Connection duration since established
		Since         time.Time     // Connection time established
		FrontendName  string        // Name of HAProxy Frontend for connection
		BackendName   string        // Name of HAProxy Backend for connection
		BackendServer string        // Name of HAProxy backend server for connection
	}

	// Feeder stats
	Feeder struct {
		Connections    map[string][]Connection // List of BEAST and MLAT connections
		BeastSince     time.Time               // Time of latest BEAST connection
		MLATSince      time.Time               // Time of latest MLAT connection
		Updated        time.Time               // Time this entry was last updated
		BeastConnected bool                    // Does the feeder have a BEAST connection
		MLATConnected  bool                    // Does the feeder have an MLAT connection
	}
)

const (
	ConnTypeBEAST ConnType = iota
	ConnTypeMLAT
)

var (
	version = "dev"

	// reParseTableKey is a regular expression (duh) that splits the stick table key
	// output into the following capture groups:
	//
	//  - Group 1: the feeder api key
	//  - Group 2: the src ip
	//  - Group 3: the src port
	//
	reParseTableKey = regexp.MustCompile(`^([[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12})\|([:a-fA-F0-9.]+?):(\d+)$`)

	// reParseSrc is a regular expression (duh) that splits the src
	// output into the following capture groups:
	//
	//  - Group 1: the src ip
	//  - Group 2: the src port
	//
	reParseSrc = regexp.MustCompile(`^([:a-fA-F0-9.]+?):(\d+)$`)

	// matchUrlSingleFeeder is a regex to match api request for single feeder stats
	matchUrlSingleFeeder = regexp.MustCompile(`^/api/v1/feeder/[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}/?$`)

	// regex to match UUID
	matchUUID = regexp.MustCompile(`[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}`)

	// Feeders is the main map of feeders, key is the feeder api key.
	Feeders map[string]Feeder

	// FeedersMu is our mutex for shared access to Feeders
	FeedersMu sync.RWMutex

	// dummyCounters is just a simple counter required for the sni_allowlist stick table to store api keys
	dummyCounters = map[string]uint64{"data.gpc0": 1}
)

func main() {

	app := cli.NewApp()
	app.Version = version
	app.Name = "Plane Watch HAProxy API Server"
	app.Usage = "Listens for HTTP requests and responds to them"
	app.Description = `
This tool has two purposes:

 - Allows querying feeder status via HTTP API: /api/v1/feeder/<api key>
 - Populates stick table "sni_allowlist" with valid api keys

The tool expects that stick tables sni_allowlist, fe_runway_adsb and fe_runway_mlat have been defined:

 - fe_runway_adsb & fe_runway_mlat must have counters conn_cur,bytes_in_cnt,bytes_out_cnt
 - sni_allowlist must have counter gpc0 and 24d (the max) expiry
`
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "listen",
			Value:   "0.0.0.0:80",
			Usage:   "The address and port to listen on for HTTP requests.",
			EnvVars: []string{"API_LISTEN"},
		},
		&cli.StringFlag{
			Name:     "nats",
			Usage:    "Nats.io URL for fetching and publishing updates. nats://guest:guest@host:4222/",
			EnvVars:  []string{"NATS"},
			Required: true,
		},
		&cli.StringFlag{
			Category: "HAProxy",
			Name:     "network",
			Value:    "tcp",
			Usage:    "The network to connect to HAProxy runtime API on. Known networks are \"tcp\", \"tcp4\" (IPv4-only), \"tcp6\" (IPv6-only), \"udp\", \"udp4\" (IPv4-only), \"udp6\" (IPv6-only), \"ip\", \"ip4\" (IPv4-only), \"ip6\" (IPv6-only), \"unix\", \"unixgram\" and \"unixpacket\".",
			EnvVars:  []string{"HAPROXY_NETWORK"},
		},
		&cli.StringFlag{
			Category: "HAProxy",
			Name:     "address",
			Value:    "haproxy:9999",
			Usage:    "The address to connect to HAProxy runtime API on. For TCP and UDP networks, the address has the form \"host:port\". The host must be a literal IP address, or a host name that can be resolved to IP addresses. The port must be a literal port number or a service name. If the host is a literal IPv6 address it must be enclosed in square brackets, as in \"[2001:db8::1]:80\" or \"[fe80::1%zone]:80\". The zone specifies the scope of the literal IPv6 address as defined in RFC 4007.",
			EnvVars:  []string{"HAPROXY_ADDRESS"},
		},
		&cli.DurationFlag{
			Category: "HAProxy",
			Name:     "interval",
			Value:    time.Second * 10,
			Usage:    "How long to wait between refreshing data from HAProxy runtime API",
			EnvVars:  []string{"HAPROXY_INTERVAL"},
		},
	}
	app.Action = runApp
	if err := app.Run(os.Args); nil != err {
		log.Error().Err(err).Send()
	}
}

func runApp(ctx *cli.Context) error {

	// configure feeder cache
	fc, err := feederauth.New(
		feederauth.WithLogger(log.Logger),
		feederauth.WithNatsURL(ctx.String("nats")),
	)
	if nil != err {
		return fmt.Errorf("feederauth.New: %w", err)
	}
	defer func() {
		_ = fc.Close()
	}()

	// prep our main data structure
	Feeders = make(map[string]Feeder)

	// establish connection to haproxy
	conn, err := haproxy.New(ctx.String("network"), ctx.String("address"))
	if err != nil {
		return fmt.Errorf("failed to create haproxy connection: %w", err)
	}

	// define function to update sni_allowlist stick table
	updateHAProxyAllowedFeeders := func() error {
		allowedFeeders := fc.ListAllAPIKeys()
		table, err := conn.ShowTable("sni_allowlist")
		if err != nil {
			return fmt.Errorf("conn.ShowTable: %w", err)
		}

		// add feeders in allowedFeeders to stick table
		for _, apiKey := range allowedFeeders {
			counters, ok := table[apiKey]

			// if feeder in stick table, only re-add if nearing expiry
			if ok {
				expiry, ok := counters["exp"]
				if !ok {
					log.Warn().Str("key", apiKey).Any("counters", counters).Msg("no expiry!")
					continue
				}
				if expiry > 200000000 {
					continue
				} else {
					log.Debug().Str("apiKey", apiKey).Msg("entry in stick-table approaching expiry, re-adding")
				}
			}

			log.Info().Str("apiKey", apiKey).Msg("adding feeder to HAProxy stick-table sni_allowlist")
			err = conn.SetTable("sni_allowlist", apiKey, dummyCounters)
			if err != nil {
				return fmt.Errorf("conn.SetTable: %w", err)
			}
		}

		// remove feeders from stick table that are no longer allowed
		for apiKey := range table {
			if slices.Contains(allowedFeeders, apiKey) {
				continue
			}
			log.Info().Str("apiKey", apiKey).Msg("removing feeder to HAProxy stick-table sni_allowlist")
			err = conn.ClearTableEntry("sni_allowlist", apiKey)
			if err != nil {
				return fmt.Errorf("conn.ClearTableEntry: %w", err)
			}
		}

		return nil
	}

	// first run updateHAProxyAllowedFeeders
	err = updateHAProxyAllowedFeeders()
	if err != nil {
		log.Error().Err(err).Send()
	}

	// set up scheduled updates to haproxy stick table
	_ = timing.RunOnTicker(log.Logger, time.Minute, updateHAProxyAllowedFeeders)

	// define function to update cached data from haproxy
	updateFromHAProxyOnTicker := func() error {

		// pull data from haproxy & create new Feeder map
		newF, err := updateFromHAProxy(conn)
		if err != nil {
			return fmt.Errorf("update from haproxy failed: %w", err)
		}

		// update "live" Feeder map
		FeedersMu.Lock()
		defer FeedersMu.Unlock()
		Feeders = *newF
		log.Info().Msg("updated live feeders from haproxy")

		return nil
	}

	// first run updateFromHAProxyOnTicker
	err = updateFromHAProxyOnTicker()
	if err != nil {
		log.Error().Err(err).Send()
	}

	// set up scheduled updates to cached data
	_ = timing.RunOnTicker(log.Logger, ctx.Duration("interval"), updateFromHAProxyOnTicker)

	// configure http mux
	mux := http.NewServeMux()

	// stats http server routes
	mux.HandleFunc("/api/v1/feeder/", apiReturnSingleFeeder)
	mux.HandleFunc("/healthcheck", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		return
	})

	// prep http server
	srv := &http.Server{
		Addr:    ctx.String("listen"),
		Handler: mux,
	}

	// start http server
	log.Info().Str("addr", srv.Addr).Msg("starting http server")
	err = srv.ListenAndServe()
	if err != nil {
		if err != http.ErrServerClosed {
			return fmt.Errorf("http server stopped: %w", err)
		}
	}

	return nil
}

// updateFromHAProxy retrieves the following from HAProxy:
//   - "show table fe_runway_adsb"
//   - "show table fe_runway_mlat"
//   - "show sess"
func updateFromHAProxy(conn *haproxy.Conn) (*map[string]Feeder, error) {

	// get `show sess` data form haproxy
	sessions, err := conn.ShowSess()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve sessions from haproxy: %w", err)
	}

	// get `show table fe_runway_adsb` data form haproxy
	beastTable, err := conn.ShowTable("fe_runway_adsb")
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve table fe_runway_adsb from haproxy: %w", err)
	}

	// get `show table fe_runway_mlat` data form haproxy
	mlatTable, err := conn.ShowTable("fe_runway_mlat")
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve table fe_runway_mlat from haproxy: %w", err)
	}

	// prepare new feeder map
	feeders := make(map[string][]Connection)

	// process table fe_runway_adsb
	for tableKey, tableCounters := range beastTable {

		// parse table
		apiKey, c, err := parseTable(tableKey, tableCounters, ConnTypeBEAST, "fe_runway_adsb")
		if err != nil {
			return nil, fmt.Errorf("failed to parse table fe_runway_adsb: %w", err)
		}

		// if feeder not yet seen, alloc slice
		if _, ok := feeders[apiKey]; !ok {
			feeders[apiKey] = make([]Connection, 0, 1)
		}

		// add connection to feeder
		feeders[apiKey] = append(feeders[apiKey], c)
	}

	// process table fe_runway_mlat
	for tableKey, tableCounters := range mlatTable {

		// parse table
		apiKey, c, err := parseTable(tableKey, tableCounters, ConnTypeMLAT, "fe_runway_mlat")
		if err != nil {
			return nil, fmt.Errorf("failed to parse table fe_runway_mlat: %w", err)
		}

		// if feeder not yet seen, alloc slice
		if _, ok := feeders[apiKey]; !ok {
			feeders[apiKey] = make([]Connection, 0, 1)
		}

		// add connection to feeder
		feeders[apiKey] = append(feeders[apiKey], c)
	}

	// process sessions
	for _, sessionInfo := range sessions {

		// parse source from session
		m := reParseSrc.FindStringSubmatch(sessionInfo.Source)
		if m == nil || len(m) != 3 {
			return nil, fmt.Errorf("could not parse IP & port from session source: %v", sessionInfo.Source)
		}
		sessionSrcIP := m[1]
		sessionSrcPort, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("could not parse port from session source: %v", sessionInfo.Source)
		}

		// match session to feeder connections & populate connection duration, frontend, backend, server
		for apiKey, connections := range feeders {
			for n, connection := range connections {
				if sessionSrcIP == connection.srcIP && int(sessionSrcPort) == connection.srcPort {
					c := feeders[apiKey][n]
					c.Duration = sessionInfo.Age
					c.Since = time.Now().Add(-sessionInfo.Age)
					c.FrontendName = sessionInfo.Frontend
					c.BackendName = sessionInfo.Backend
					c.BackendServer = sessionInfo.Server
					feeders[apiKey][n] = c
					break
				}
			}
		}
	}

	// prep & populate output map
	F := make(map[string]Feeder)
	for apiKey := range feeders {
		f := Feeder{
			Connections: make(map[string][]Connection),
		}
		f.Connections["BEAST"] = make([]Connection, 0, 1)
		f.Connections["MLAT"] = make([]Connection, 0, 1)
		for _, c := range feeders[apiKey] {
			switch c.connType {
			case ConnTypeBEAST:
				f.Connections["BEAST"] = append(f.Connections["BEAST"], c)
				if c.Since.After(f.BeastSince) {
					f.BeastSince = c.Since
				}
			case ConnTypeMLAT:
				f.Connections["MLAT"] = append(f.Connections["MLAT"], c)
				if c.Since.After(f.MLATSince) {
					f.MLATSince = c.Since
				}
			}
		}
		if len(f.Connections["BEAST"]) > 0 {
			f.BeastConnected = true
		}
		if len(f.Connections["MLAT"]) > 0 {
			f.MLATConnected = true
		}
		f.Updated = time.Now()
		F[apiKey] = f
	}

	return &F, nil
}

// parseTableKey parses the table key and returns the feeder api key and Connection
func parseTableKey(key string, connType ConnType) (string, Connection, error) {

	// parse table key
	m := reParseTableKey.FindStringSubmatch(key)
	if m == nil || len(m) != 4 {
		return "", Connection{}, fmt.Errorf("could not parse table key: %s", key)
	}
	port, err := strconv.ParseInt(m[3], 10, 64)
	if err != nil {
		return "", Connection{}, fmt.Errorf("could not parse port from table key: %s", key)
	}

	c := Connection{
		connType: connType,
		Src: net.TCPAddr{
			IP:   net.ParseIP(m[2]),
			Port: int(port),
		},
		srcIP:   m[2],
		srcPort: int(port),
	}

	return m[1], c, nil
}

// parseTable returns apiKey & Connection from each table line
func parseTable(key string, counters map[string]uint64, connType ConnType, name string) (string, Connection, error) {
	// parse table key
	apiKey, c, err := parseTableKey(key, connType)
	if err != nil {
		return apiKey, c, fmt.Errorf("failed to parse table key from %s: %w", name, err)
	}

	// update byte counters from table counters
	c.BytesIn = counters["bytes_in_cnt"]
	c.BytesOut = counters["bytes_out_cnt"]

	return apiKey, c, nil
}

// apiReturnSingleFeeder returns statistics data for a single feeder in JSON format
func apiReturnSingleFeeder(w http.ResponseWriter, r *http.Request) {

	logger := log.With().
		Str("RemoteAddr", r.RemoteAddr).
		Str("url", r.URL.Path).
		Logger()

	// try to match the path for the api query for single feeder by uuid, eg:
	// /api/v1/feeder/xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	if matchUrlSingleFeeder.Match([]byte(strings.ToLower(r.URL.Path))) {

		// try to extract uuid from path
		clientApiKey, err := uuid.Parse(string(matchUUID.Find([]byte(strings.ToLower(r.URL.Path)))))
		if err != nil {
			logger.Err(err).
				Int("status_code", http.StatusBadRequest).
				Msg("could not get api key from url")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// look up feeder by uuid
		FeedersMu.RLock()
		defer FeedersMu.RUnlock()
		_, ok := Feeders[clientApiKey.String()]
		if !ok {
			logger.Error().
				Int("status_code", http.StatusBadRequest).
				Any("apikey", clientApiKey.String()).
				Msg("feeder not found")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// prepare response
		output, err := json.MarshalIndent(Feeders[clientApiKey.String()], "", "  ")
		if err != nil {
			logger.Error().
				Int("status_code", http.StatusInternalServerError).
				Any("apikey", clientApiKey.String()).
				Msg("error marshalling response into json")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "application/json")
		_, err = w.Write(output)
		if err != nil {
			logger.Err(err).
				Msg("error writing response")
		} else {
			logger.Info().
				Int("status_code", http.StatusOK).
				Send()
		}
		return

	} else {
		logger.Error().
			Int("status_code", http.StatusBadRequest).
			Msg("path did not match single feeder")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
}
