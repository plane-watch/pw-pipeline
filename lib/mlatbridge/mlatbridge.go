package mlatbridge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
	"plane.watch/lib/feedercache"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/stunnel"
	"plane.watch/lib/timing"
)

type (
	MLATBridge struct {

		// hostPort contains the IP and port to listen on, in the same format as the address argument to net.Listen
		hostPort string

		// certPath contains the file to use for the server certificate
		certPath string

		// keyPath contains the file to use for the server certificate's private key
		keyPath string

		log      zerolog.Logger
		listener *stunnel.Listener

		natsServer *nats_io.Server
		natsURL    string

		feeders *feedercache.FeederCache
	}

	Option func(manifest *MLATBridge)
)

var (
	MissingOption = errors.New("option is required")

	prometheusConnectedMLATFeeders = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "runway",
		Subsystem: "mlat",
		Name:      "feeders-connected",
		Help:      "The total number of mlat feeders connected.",
	})
)

func WithFeederCache(feeders *feedercache.FeederCache) Option {
	return func(mb *MLATBridge) {
		mb.feeders = feeders
	}
}

func WithListenHostPort(listen string) Option {
	return func(mb *MLATBridge) {
		mb.hostPort = listen
	}
}

func WithTLSCertificate(cert, key string) Option {
	return func(mb *MLATBridge) {
		mb.certPath = cert
		mb.keyPath = key
	}
}

func WithNatsURL(natsURL string) Option {
	return func(mb *MLATBridge) {
		mb.natsURL = natsURL
	}
}

func ListenForIncomingPlaneWatchMLAT(ctx context.Context, opts ...Option) (*MLATBridge, error) {
	// listen for incoming connection, validate them and then accept incoming MLAT
	var err error

	// create our MLATBridge and apply our options to it
	mb := &MLATBridge{
		log: log.With().Str("listener", "mlat").Logger(),
	}
	for _, opt := range opts {
		opt(mb)
	}

	// let's do some sanity checking...
	if mb.natsURL == "" {
		return nil, fmt.Errorf("%w: Please specify the Nats URL (sink)", MissingOption)
	}
	if mb.feeders == nil {
		return nil, fmt.Errorf("%w: You need to configure the *feederauth.FeederCache", MissingOption)
	}

	// setup our nats connection
	mb.natsServer, err = nats_io.NewServer(
		nats_io.WithConnections(false, true),
		nats_io.WithServer(mb.natsURL, "runway-atc-client-MLAT"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to setup nats connection: %w", err)
	}

	// now let's start listening for connections!

	mb.listener, err = stunnel.NewListener(
		stunnel.WithHostPort(mb.hostPort),
		stunnel.WithTLSCertificate(mb.certPath, mb.keyPath),
		stunnel.WithConnectionHandler(mb.handler),
		stunnel.WithAuthenticator(mb.authenticator),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to setup stunnel listener: %w", err)
	}

	err = mb.listener.Listen(ctx)
	if err != nil {
		mb.log.Error().Err(err).Msg("failed to listen for plane.watch mlat")
	}

	return mb, nil
}

func (mb *MLATBridge) authenticator(apiKey string) (bool, error) {
	return mb.feeders.Authenticate(apiKey, feedercache.MLAT)
}

func (mb *MLATBridge) handler(feederConn net.Conn, apiKey string) error {

	// failsafe to close feeder connection
	defer func() {
		_ = feederConn.Close()
	}()

	// get feeder details
	feeder, err := mb.feeders.Get(apiKey)
	if err != nil {
		return fmt.Errorf("failed to get feeder for %s: %w", apiKey, err)
	}

	// update feeder cache
	mb.feeders.SetConnected(apiKey, feedercache.MLAT)
	defer func() {
		mb.feeders.SetDisconnected(apiKey, feedercache.MLAT)
	}()

	// lookup which mlat server to use
	mlatHost, ok := muxes[feeder.Mux]
	if !ok {
		return fmt.Errorf("could not find mux %q", feeder.Mux)
	}

	// establish a connection to mlat server
	mlatConn, err := net.Dial("tcp", mlatHost)
	if err != nil {
		return fmt.Errorf("could not connect to mlat server: %w", err)
	}

	// failsafe to close mlat connection
	defer func() {
		_ = mlatConn.Close()
	}()

	// register prom metrics
	prometheusMLATBytesRx := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "runway",
		Subsystem: "mlat",
		Name:      "input-bytes-total",
		Help:      "The total number of MLAT bytes received from the feeder.",
		ConstLabels: map[string]string{
			"feeder_id":    strconv.FormatInt(int64(feeder.Id), 10),
			"feeder_label": feeder.Label,
			"feeder_user":  feeder.User,
			"feeder_mux":   feeder.Mux,
		},
	})
	err = prometheus.Register(prometheusMLATBytesRx)
	if err != nil {
		return fmt.Errorf("failed to register prometheus counter: %w", err)
	}
	defer func() {
		_ = prometheus.Unregister(prometheusMLATBytesRx)
	}()
	prometheusMLATBytesTx := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "runway",
		Subsystem: "mlat",
		Name:      "output-bytes-total",
		Help:      "The total number of MLAT bytes sent to the feeder.",
		ConstLabels: map[string]string{
			"feeder_id":    strconv.FormatInt(int64(feeder.Id), 10),
			"feeder_label": feeder.Label,
			"feeder_user":  feeder.User,
			"feeder_mux":   feeder.Mux,
		},
	})
	err = prometheus.Register(prometheusMLATBytesTx)
	if err != nil {
		return fmt.Errorf("failed to register prometheus counter: %w", err)
	}
	defer func() {
		_ = prometheus.Unregister(prometheusMLATBytesTx)
	}()
	prometheusConnectedMLATFeeders.Inc()
	defer func() {
		prometheusConnectedMLATFeeders.Dec()
	}()

	// create a context for this connection
	connCtx, connCtxCancel := context.WithCancel(context.Background())

	// spin up goroutines to handle bridging
	eg := errgroup.Group{}
	eg.Go(func() error {
		return mb.simplexBridge(connCtx, connCtxCancel, feederConn, mlatConn, prometheusMLATBytesRx)
	})
	eg.Go(func() error {
		return mb.simplexBridge(connCtx, connCtxCancel, mlatConn, feederConn, prometheusMLATBytesTx)
	})

	// spin up goroutine for poison pill if feeder no longer valid
	eg.Go(func() error {
		timing.RunOnTickerWithContext(connCtx, mb.log, time.Second*5, func() error {
			if !mb.feeders.IsValid(apiKey) {
				connCtxCancel()
			}
			return nil
		})
		return nil
	})

	// wait for goroutines to finish
	err = eg.Wait()

	if err != nil {
		return fmt.Errorf("finished MLAT bridge for %s: %w", apiKey, err)
	}
	return nil
}

// simplexBridge reads from connection "from" and writes to connection "to".
// It continues doing this until the context is cancelled, or if there is a read/write error.
// Connections will be closed and context will be cancelled when this method exits.
func (mb *MLATBridge) simplexBridge(ctx context.Context, cancel context.CancelFunc, from, to net.Conn, counter prometheus.Counter) error {

	var (
		err error
		n   int
	)

	// close both sides of the bridge when done
	defer func() {
		_ = from.Close()
	}()
	defer func() {
		_ = to.Close()
	}()

	// make buffer to hold data in flight
	buf := make([]byte, 65745) // todo(mikenye): set to tcp maximum segment size - is this realistic?

	for {

		// check for context closure
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled: %w", ctx.Err())
		default:
		}

		// set/extend read/write deadlines
		err = from.SetReadDeadline(time.Now().Add(1 * time.Second))
		if err != nil {
			cancel()
			return fmt.Errorf("failed to set read deadline: %w", err)
		}
		err = to.SetWriteDeadline(time.Now().Add(1 * time.Second))
		if err != nil {
			cancel()
			return fmt.Errorf("failed to set write deadline: %w", err)
		}

		// Copy bytes from "from" to "to" connections.
		// We don't bail out on os.ErrDeadlineExceeded as this may be due to
		// read/write deadlines being hit.
		// The deadlines allow us to loop & check for context closure for graceful stop.
		n, err = from.Read(buf)
		if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
			cancel()
			return fmt.Errorf("read error: %w", err)
		}
		counter.Add(float64(n))
		_, err = to.Write(buf[:n])
		if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
			cancel()
			return fmt.Errorf("write error: %w", err)
		}
	}
}
