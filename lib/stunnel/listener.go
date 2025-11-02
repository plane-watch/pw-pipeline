package stunnel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/timing"
)

type (
	ConnectionHandler     func(conn net.Conn, apiKey string) error
	AuthenticationHandler func(apiKey string) (bool, error)

	Listener struct {
		hostPort string

		certPath, keyPath string
		cert              *tls.Certificate
		muCert            sync.Mutex

		connHandler ConnectionHandler
		authHandler AuthenticationHandler

		log zerolog.Logger
	}

	ListenerOption func(*Listener)
)

func WithHostPort(hostPort string) ListenerOption {
	return func(listener *Listener) {
		listener.hostPort = hostPort
	}
}

func WithTLSCertificate(cert, key string) ListenerOption {
	return func(listener *Listener) {
		listener.certPath = cert
		listener.keyPath = key
	}
}

func WithConnectionHandler(h ConnectionHandler) ListenerOption {
	return func(listener *Listener) {
		listener.connHandler = h
	}
}

func WithAuthenticator(h AuthenticationHandler) ListenerOption {
	return func(listener *Listener) {
		listener.authHandler = h
	}
}

// NewListener creates a new Listener
func NewListener(opts ...ListenerOption) (*Listener, error) {
	l := &Listener{
		log: log.With().Str("Section", "stunnel").Logger(),
	}

	for _, opt := range opts {
		opt(l)
	}

	// sanity check
	if l.hostPort == "" {
		return nil, fmt.Errorf("%w: Host/Port information is not configured", MissingOption)
	}

	if l.certPath == "" || l.keyPath == "" {
		return nil, fmt.Errorf("%w: TLS Certificate and Key information is not configured", MissingOption)
	}
	err := l.ReloadCertificate()
	if err != nil {
		return nil, fmt.Errorf("%w: could not load certificate: %w", MissingOption, err)
	}

	if l.connHandler == nil {
		return nil, fmt.Errorf("%w: Connection Handler is not configured", MissingOption)
	}

	return l, nil
}

func (l *Listener) handleIncoming(conn net.Conn) {
	var err error

	// set a 10-second deadline for tls handshake
	err = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err != nil {
		l.log.Error().Msg("failed to set deadline on connection")
		_ = conn.Close()
		return
	}

	// perform tls handshake
	l.log.Debug().Msg("before handshake")
	if err = conn.(*tls.Conn).Handshake(); err != nil {
		l.log.Error().
			Err(err).
			Msg("Failed to complete TLS handshake with client")
		_ = conn.Close()
		return
	}

	// ensure tls handshake completed ok
	l.log.Debug().Msg("testing handshake complete")
	if conn.(*tls.Conn).ConnectionState().HandshakeComplete == false {
		l.log.Error().
			Msg("Handshake is not complete, bailing")
		_ = conn.Close()
		return
	}
	l.log.Debug().Msg("tls connection established")

	// remove connection deadline now tls handshake complete
	err = conn.SetDeadline(time.Time{})
	if err != nil {
		l.log.Error().Msg("failed to remove deadline on connection")
		_ = conn.Close()
		return
	}

	// pull feeder api key from SNI
	apiKey := conn.(*tls.Conn).ConnectionState().ServerName
	l.log.Debug().Str("APIKey", apiKey).Msg("client api key")

	// authenticate client
	valid, errAuth := l.authHandler(apiKey)
	if errAuth != nil {
		l.log.Error().
			Str("APIKey", apiKey).
			Err(errAuth).
			Msg("authentication failure, closing connection")
		_ = conn.Close()
		return
	}
	if !valid {
		l.log.Debug().Msg("API Key is not valid, closing connection")
		_ = conn.Close()
		return
	}

	// pass authenticated connection to connHandler
	l.log.Debug().Str("APIKey", apiKey).Msg("Handling connection")
	errConn := l.connHandler(conn, apiKey)
	if errConn != nil {
		l.log.Error().
			Err(errConn).
			Msg("connection failure, closing")
		_ = conn.Close()
	}
}

func (l *Listener) Listen(ctx context.Context) error {
	config := &tls.Config{
		GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
			l.log.Info().
				Str("RemoteAddr", info.Conn.RemoteAddr().String()).
				Str("APIKey", info.ServerName).
				Msg("Incoming Connection")

			l.muCert.Lock()
			defer l.muCert.Unlock()
			return l.cert, nil
		},
	}

	// reload our certificate every 5 minutes
	cancelTicker := timing.RunOnTicker(
		l.log.With().Str("what", "stunnel reloading certificate").Logger(),
		5*time.Minute,
		l.ReloadCertificate,
	)
	defer cancelTicker()

	// listen for incoming connections
	netListener, errListener := tls.Listen("tcp", l.hostPort, config)
	if errListener != nil {
		return fmt.Errorf("failed to listen: %s - %w", l.hostPort, errListener)
	}
	l.log.Info().
		Str("addr", netListener.Addr().String()).
		Msg("started listening for connections")

	// accept incoming connections
	wg := &sync.WaitGroup{}
	wg.Go(func() {
		for {

			// accept the connection
			conn, errAccept := netListener.Accept()
			if errAccept != nil {
				if errors.Is(errAccept, net.ErrClosed) {
					l.log.Error().Err(errAccept).Msg("Listener closed, exiting accept loop")
					return
				}
				if conn == nil {
					l.log.Error().
						Err(errAccept).
						Msg("connection accept failure (nil connection)")
				} else {
					l.log.Error().
						Str("RemoteAddr", conn.RemoteAddr().String()).
						Err(errAccept).
						Msg("connection accept failure")
				}
				continue
			}
			l.log.Info().
				Str("RemoteAddr", conn.RemoteAddr().String()).
				Msg("accepted connection")

			// hand the connection off so we aren't blocking accept of new connections
			go l.handleIncoming(conn)
		}
	})

	// wait for context closure
	select {
	case <-ctx.Done():
		l.log.Debug().Msg("context has finished")
	}

	// shutdown
	l.log.Info().
		Str("addr", netListener.Addr().String()).
		Msg("shutting down listener")

	_ = netListener.Close()
	wg.Wait()

	l.log.Info().
		Str("addr", netListener.Addr().String()).
		Msg("stopped listening for connections")

	return nil
}

func (l *Listener) ReloadCertificate() error {
	l.log.Info().Str("certPath", l.certPath).Str("keyPath", l.keyPath).Msg("Loading TLS Certificate")

	cert, err := tls.LoadX509KeyPair(l.certPath, l.keyPath)
	if err != nil {
		return err
	}

	l.muCert.Lock()
	defer l.muCert.Unlock()
	l.cert = &cert

	return nil
}
