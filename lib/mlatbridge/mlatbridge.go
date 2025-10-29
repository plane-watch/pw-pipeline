package mlatbridge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

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
		log: log.With().Str("Section", "MLATBridge").Logger(),
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
		nats_io.WithServer(mb.natsURL, "borderforce-atc-client-MLAT"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to setup nats connection: %w", err)
	}

	// now let's start listening for connections!

	mb.listener, err = stunnel.New(
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
	mb.feeders.SetConnected(apiKey, feedercache.MLAT)

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

	// create a context for this connection
	connCtx, connCtxCancel := context.WithCancel(context.Background())

	// spin up goroutines to handle bridging
	eg := errgroup.Group{}
	eg.Go(func() error {
		return mb.bridge(connCtx, connCtxCancel, feederConn, mlatConn)
	})
	eg.Go(func() error {
		return mb.bridge(connCtx, connCtxCancel, mlatConn, feederConn)
	})

	// spin up goroutine for poison pill
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

func (mb *MLATBridge) bridge(ctx context.Context, cancel context.CancelFunc, from, to net.Conn) error {

	defer func() {
		_ = from.Close()
	}()

	defer func() {
		_ = to.Close()
	}()

	var (
		err error
		n   int
	)

	buf := make([]byte, 65745) // todo(mikenye): set to tcp maximum segment size - is this realistic?

	for {

		// check for context closure
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled: %w", ctx.Err())
		default:
		}

		// set deadlines
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

		// copy bytes from "from" to "to" connections
		n, err = from.Read(buf)
		if err != nil {
			cancel()
			return fmt.Errorf("read error: %w", err)
		}
		_, err = to.Write(buf[:n])
		if err != nil {
			cancel()
			return fmt.Errorf("write error: %w", err)
		}
	}
}
