package ws_client

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"plane.watch/lib/export"
	"plane.watch/lib/tile_grid"
	"plane.watch/lib/ws_protocol"
	"sync"
	"time"
)

type (
	Client struct {
		conn          *websocket.Conn
		secure        bool
		host          string // expected format "plane.watch:8080"
		locationChan  chan *export.PlaneLocation
		gridTilesChan chan []string
		ackSubChan    chan []string
		ackUnsubChan  chan []string

		subLock, unsubLock, gridLock sync.Mutex
	}
)

func NewClient(host string) *Client {
	return &Client{
		host:         host,
		secure:       true,
		locationChan: make(chan *export.PlaneLocation, 100),
		// response channels
		gridTilesChan: make(chan []string),
		ackSubChan:    make(chan []string),
		ackUnsubChan:  make(chan []string),
	}
}

// Secure sets whether we use TLS to connect to the host
func (c *Client) Secure(isSecure bool) {
	c.secure = isSecure
}

func (c *Client) Connect() (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	conf := websocket.DialOptions{
		Subprotocols:    []string{"planes"},
		CompressionMode: websocket.CompressionContextTakeover,
	}
	c.conn, _, err = websocket.Dial(ctx, "ws://"+c.host+"/planes", &conf)
	if nil == err {
		go c.listen()
	}
	return err
}

func (c *Client) Disconnect() error {
	close(c.locationChan)
	if nil != c.conn {
		return c.conn.Close(websocket.StatusNormalClosure, "Closing...")
	}
	return nil
}

func (c *Client) listen() {
	for {
		ctx := context.Background()
		msg := ws_protocol.WsResponse{}
		err := wsjson.Read(ctx, c.conn, &msg)
		if nil != err {
			log.Debug().Err(err).Msg("Failed to understand WS message")
			return
		}
		switch msg.Type {
		case ws_protocol.ResponseTypePlaneLocation:
			c.locationChan <- msg.Location
		case ws_protocol.ResponseTypeAckSub:
			c.ackSubChan <- msg.Tiles
		case ws_protocol.ResponseTypeAckUnsub:
			c.ackUnsubChan <- msg.Tiles
		case ws_protocol.ResponseTypeError:
			log.Error().Str("Response", msg.Message)
		case ws_protocol.ResponseTypeSubTiles:
			c.gridTilesChan <- msg.Tiles
		}
	}
}

func (c *Client) LocationUpdates() chan *export.PlaneLocation {
	return c.locationChan
}

func (c *Client) Grid() (tile_grid.GridLocations, error) {
	rqUrl := "http"
	if c.secure {
		rqUrl += "s"
	}
	rqUrl += "://" + c.host + "/grid"

	rs, err := http.Get(rqUrl)
	if nil != err {
		log.Error().Err(err).Msg("Unable to fetch the grid array")
		return nil, err
	}
	body, err := io.ReadAll(rs.Body)
	if nil != err {
		return nil, err
	}
	if err = rs.Body.Close(); err != nil {
		return nil, err
	}

	grid := tile_grid.GridLocations{}
	if err = json.Unmarshal(body, &grid); nil != err {
		return nil, err
	}

	return grid, nil
}

func (c *Client) SubscribedTileList() ([]string, error) {
	c.gridLock.Lock()
	defer c.gridLock.Unlock()
	rq := ws_protocol.WsRequest{
		Type: ws_protocol.RequestTypeSubscribeList,
	}
	err := wsjson.Write(context.Background(), c.conn, &rq)
	if nil != err {
		return nil, err
	}

	return <-c.ackSubChan, nil
}
func (c *Client) Subscribe(tileName string) error {
	c.subLock.Lock()
	defer c.subLock.Unlock()
	rq := ws_protocol.WsRequest{
		Type:     ws_protocol.RequestTypeSubscribe,
		GridTile: tileName,
	}
	err := wsjson.Write(context.Background(), c.conn, &rq)
	if nil != err {
		return err
	}
	<-c.ackSubChan
	return nil
}

func (c *Client) Unsubscribe(tileName string) error {
	c.unsubLock.Lock()
	defer c.unsubLock.Unlock()
	rq := ws_protocol.WsRequest{
		Type:     ws_protocol.RequestTypeUnsubscribe,
		GridTile: tileName,
	}
	err := wsjson.Write(context.Background(), c.conn, &rq)
	if nil != err {
		return err
	}
	<-c.ackSubChan
	return nil
}
