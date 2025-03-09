package ws_protocol

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"net/http"
	"time"
)

const (
	ProdSourceURL = "https://plane.watch/planes"
)

type (
	WsClient struct {
		sourceURL string
		conn      *websocket.Conn

		responseHandlers []ResponseHandler

		logger zerolog.Logger
	}

	Option          func(client *WsClient)
	ResponseHandler func(response *WsResponse)
)

var (
	ErrUnexpectedProtocol = errors.New("Unexpected WS Protocol")
)

func WithSourceURL(sourceURL string) Option {
	return func(client *WsClient) {
		client.sourceURL = sourceURL
	}
}

func WithResponseHandler(f ResponseHandler) Option {
	return func(client *WsClient) {
		client.responseHandlers = append(client.responseHandlers, f)
	}
}

func WithLogger(logger zerolog.Logger) Option {
	return func(client *WsClient) {
		client.logger = logger
	}
}

func NewClient(opts ...Option) *WsClient {
	c := &WsClient{
		sourceURL:        ProdSourceURL,
		responseHandlers: make([]ResponseHandler, 0),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *WsClient) Connect() error {
	var err error
	customTransport := http.DefaultTransport.(*http.Transport).Clone()
	customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	httpClient := &http.Client{Transport: customTransport}
	cfg := &websocket.DialOptions{
		HTTPClient:   httpClient,
		Subprotocols: []string{WsProtocolPlanes},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	c.conn, _, err = websocket.Dial(ctx, c.sourceURL, cfg)

	if nil != err {
		return errors.Wrap(err, "failed to connect to Plane.Watch websocket")
	}

	if c.conn.Subprotocol() != WsProtocolPlanes {
		return ErrUnexpectedProtocol
	}

	c.conn.SetReadLimit(1_048_576) // 1MiB

	go c.reader()
	return nil
}

func (c *WsClient) Disconnect() error {
	return c.conn.Close(websocket.StatusNormalClosure, "Going away")
}

func (c *WsClient) reader() {
	for {
		mType, msg, err := c.conn.Read(context.Background())
		if mType != websocket.MessageText {
			// c.logger.Error().Str("body", string(msg)).Msg("Incorrect Protocol, expected text, got binary")
			continue
		}
		if nil != err {
			if errors.Is(err, websocket.CloseError{}) {
				return
			}
			c.logger.Error().Err(err).Send()
			continue
		}

		r := &WsResponse{}
		err = json.Unmarshal(msg, r)
		if nil != err {
			c.logger.Error().Err(err).Send()
			continue
		}

		for _, f := range c.responseHandlers {
			f(r)
		}
	}
}

func (c *WsClient) writeRequest(rq *WsRequest) error {
	rqJSON, err := json.Marshal(rq)
	if err != nil {
		return errors.Wrap(err, "failed to subscribe to tile")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageText, rqJSON)
}

func (c *WsClient) Subscribe(gridTile string) error {
	rq := WsRequest{
		Type:     RequestTypeSubscribe,
		GridTile: gridTile,
	}

	return c.writeRequest(&rq)
}

func (c *WsClient) SubscribeAllLow() error {
	rq := WsRequest{
		Type:     RequestTypeSubscribe,
		GridTile: GridTileAllLow,
	}

	return c.writeRequest(&rq)
}

func (c *WsClient) SubscribeAllHigh() error {
	rq := WsRequest{
		Type:     RequestTypeSubscribe,
		GridTile: GridTileAllHigh,
	}

	return c.writeRequest(&rq)
}
