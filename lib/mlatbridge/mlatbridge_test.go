package mlatbridge

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type trackingConn struct {
	net.Conn
	closed int32
}

func (c *trackingConn) Close() error {
	atomic.AddInt32(&c.closed, 1)
	return c.Conn.Close()
}

func TestSimplexBridgeDoesNotCloseConnections(t *testing.T) {
	mb := &MLATBridge{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rawA, rawB := net.Pipe()
	connA := &trackingConn{Conn: rawA}
	connB := &trackingConn{Conn: rawB}

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_mlat_bridge_bytes_total",
		Help: "test counter",
	})

	err := mb.simplexBridge(ctx, cancel, connA, connB, counter)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
	require.Equal(t, int32(0), atomic.LoadInt32(&connA.closed))
	require.Equal(t, int32(0), atomic.LoadInt32(&connB.closed))

	_ = connA.Close()
	_ = connB.Close()
}
