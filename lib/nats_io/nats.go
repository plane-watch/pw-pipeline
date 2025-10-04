package nats_io

import (
	"errors"
	"net"
	"net/url"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const DefaultQueueDepth = 2048

type (
	healthItem struct {
		name, subject string
		ch            chan *nats.Msg
	}

	Server struct {
		url            string
		connectionName string
		QueueDepth     int

		incoming *nats.Conn
		outgoing *nats.Conn

		channels []healthItem

		log zerolog.Logger

		connectIncoming bool
		connectOutgoing bool

		droppedMessageCounter prometheus.Counter
	}

	Option func(*Server)
)

func WithConnections(incoming, outgoing bool) Option {
	return func(server *Server) {
		server.connectIncoming = incoming
		server.connectOutgoing = outgoing
	}
}

func WithServer(serverURL, connectionName string) Option {
	return func(server *Server) {
		server.SetURL(serverURL)
		server.connectionName = connectionName
	}
}

func NewServer(opts ...Option) (*Server, error) {
	n := &Server{
		log:             log.With().Str("section", "nats.io").Logger(),
		QueueDepth:      DefaultQueueDepth,
		connectIncoming: true,
		connectOutgoing: true,
	}
	for _, opt := range opts {
		opt(n)
	}
	if err := n.Connect(); nil != err {
		return nil, err
	}
	return n, nil
}

func (n *Server) SetURL(serverURL string) {
	serverURLParts, err := url.Parse(serverURL)
	if nil == err {
		if serverURLParts.Port() == "" {
			serverURLParts.Host = net.JoinHostPort(serverURLParts.Hostname(), "4222")
		}
	} else {
		log.Error().Err(err).Msg("invalid url")
	}
	n.url = serverURLParts.String()
}
func (n *Server) DroppedCounter(counter prometheus.Counter) {
	n.droppedMessageCounter = counter
}
func (n *Server) NatsErrHandler(conn *nats.Conn, sub *nats.Subscription, err error) {
	l := n.log.Error().Err(err)

	for _, c := range n.channels {
		if c.subject == sub.Subject {
			l.Int(c.name+" len", len(c.ch)).
				Int(c.name+" capacity", cap(c.ch))
		}
	}
	if nil != conn {
		l.Str("addr", conn.ConnectedUrl())
	}
	if nil != sub {
		l.Str("subscription", sub.Subject+"["+sub.Queue+"]")
	}
	l.Send()

	if nil != n.droppedMessageCounter && errors.Is(err, nats.ErrSlowConsumer) {
		n.droppedMessageCounter.Inc()
	}
}

func (n *Server) Connect() error {
	var err error
	n.log.Debug().Str("url", n.url).Msg("connecting to server...")
	if n.connectIncoming {
		n.incoming, err = nats.Connect(
			n.url,
			nats.ErrorHandler(n.NatsErrHandler),
			nats.Name(n.connectionName+"+incoming"),
		)
		if nil != err {
			n.log.Error().
				Err(err).
				Str("dir", "incoming").
				Str("url", n.url).
				Msg("Unable to connect to NATS server")
			return err
		}
	}
	if n.connectOutgoing {
		n.outgoing, err = nats.Connect(
			n.url,
			nats.ErrorHandler(n.NatsErrHandler),
			nats.Name(n.connectionName+"+outgoing"),
		)
		if nil != err {
			n.log.Error().Err(err).Str("dir", "outgoing").Msg("Unable to connect to NATS server")
			return err
		}
	}
	n.log.Debug().Str("url", n.url).Msg("Connected")
	return nil
}

// Publish is our simple message publisher
func (n *Server) Publish(queue string, msg []byte) error {
	if nil == n.outgoing {
		return errors.New("outgoing nats connection not set up")
	}
	err := n.outgoing.Publish(queue, msg)
	if nil != err {
		if errors.Is(err, nats.ErrInvalidConnection) || errors.Is(err, nats.ErrConnectionClosed) || errors.Is(err, nats.ErrConnectionDraining) {
			n.log.Error().Err(err).Msg("Connection not in a valid state")
		}
	}
	return err
}
func (n *Server) PublishWithHeaders(queue string, msg []byte, headers map[string]string) error {
	if nil == n.outgoing {
		return errors.New("outgoing nats connection not set up")
	}
	m := nats.NewMsg(queue)
	for k, v := range headers {
		m.Header.Add(k, v)
	}
	m.Data = msg
	err := n.outgoing.PublishMsg(m)
	if nil != err {
		if nats.ErrInvalidConnection == err || nats.ErrConnectionClosed == err || nats.ErrConnectionDraining == err {
			n.log.Error().Err(err).Msg("Connection not in a valid state")
		}
	}
	return err
}

func (n *Server) Close() {
	if nil != n.incoming && n.incoming.IsConnected() {
		if err := n.incoming.Drain(); nil != err {
			n.log.Error().Err(err).Str("dir", "incoming").Msg("failed to drain connection")
		}
	}
	if nil != n.outgoing {
		n.outgoing.Close()
	}
}

func (n *Server) Subscribe(subject string) (chan *nats.Msg, error) {
	if nil == n.incoming {
		return nil, errors.New("incoming nats connection not set up")
	}
	ch := make(chan *nats.Msg, n.QueueDepth)
	n.channels = append(n.channels, healthItem{
		name:    "sub-" + subject,
		subject: subject,
		ch:      ch,
	})
	_, err := n.incoming.ChanSubscribe(subject, ch)
	if nil != err {
		return nil, err
	}
	n.log.Info().Str("subject", subject).Msg("subscribed")
	return ch, nil
}

// SubscribeQueueGroup allows many workers to feed of a single queue
func (n *Server) SubscribeQueueGroup(subject, queueGroup string) (chan *nats.Msg, error) {
	if nil == n.incoming {
		return nil, errors.New("incoming nats connection not set up")
	}
	ch := make(chan *nats.Msg, n.QueueDepth)
	n.channels = append(n.channels, healthItem{
		name:    "sub-queue-" + subject,
		subject: subject,
		ch:      ch,
	})
	_, err := n.incoming.ChanQueueSubscribe(subject, queueGroup, ch)
	if nil != err {
		return nil, err
	}
	n.log.Info().Str("subject", subject).Str("queue-group", queueGroup).Msg("subscribed")
	return ch, nil
}

func (n *Server) Request(subject string, data []byte, headers map[string]string, timeout time.Duration) ([]byte, error) {
	if nil == n.outgoing {
		return nil, errors.New("outgoing nats connection not set up")
	}
	msg := nats.NewMsg(subject)
	msg.Data = data
	for k, v := range headers {
		msg.Header.Add(k, v)
	}
	msg, err := n.outgoing.RequestMsg(msg, timeout)
	if nil != err {
		n.log.Error().Err(err).Str("subject", subject).Msg("Failed to request")
		return nil, err
	}
	// TODO: instrument so we know how long replies take and how many are successfully served
	return msg.Data, nil
}

func (n *Server) SubscribeReply(subject, queue string, handler func(msg *nats.Msg)) (*nats.Subscription, error) {
	if nil == n.outgoing {
		return nil, errors.New("outgoing nats connection not set up")
	}
	return n.outgoing.QueueSubscribe(subject, queue, handler)
}

func (n *Server) HealthCheckName() string {
	return "Nats"
}

func (n *Server) HealthCheck() bool {
	incomingConnected := n.incoming != nil && n.incoming.IsConnected()
	outgoingConnected := n.outgoing != nil && n.outgoing.IsConnected()
	n.log.Info().
		Int("Num Channels", len(n.channels)).
		Bool("Incoming Wanted", n.connectIncoming).
		Bool("Incoming Connected", incomingConnected).
		Bool("Outgoing Wanted", n.connectOutgoing).
		Bool("Outgoing Connected", outgoingConnected).
		Send()
	for _, item := range n.channels {
		l := len(item.ch)
		c := cap(item.ch)
		p := (float32(l) / float32(c)) * 100
		n.log.Info().
			Int("# items", l).
			Int("max items", c).
			Float32("%", p).
			Str("channel", item.name).
			Msg("Channel Check")
	}
	incomingOk := incomingConnected == n.connectIncoming
	outgoingOk := outgoingConnected == n.connectOutgoing
	return incomingOk && outgoingOk
}
