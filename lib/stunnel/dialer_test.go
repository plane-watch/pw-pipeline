package stunnel

import (
	"context"
	"crypto/tls"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/nettest"
)

func TestDialler(t *testing.T) {

	// prep tlsConfig for listener
	cert, err := tls.LoadX509KeyPair(testCertFile.Name(), testKeyFile.Name())
	require.NoError(t, err, "load cert & key from file")
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	// get listener addr
	listener, err := nettest.NewLocalListener("tcp4")
	require.NoError(t, err)

	// prep listener
	tlsListener := tls.NewListener(listener, tlsConfig)

	// prep test config
	testCtx, testCancel := context.WithCancel(context.Background())
	testSNI := uuid.New()
	testData := []byte("the quick brown fox jumps over the lazy dog 9876543210 times")

	wgOuter := sync.WaitGroup{}

	// launch listener accepter
	wgOuter.Go(func() {

		buf := make([]byte, 1000)

		for {
			select {
			case <-testCtx.Done():
				return
			default:
				err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second))
				require.NoError(t, err)

				c, err := tlsListener.Accept()
				if err != nil {
					if strings.Contains(err.Error(), "timeout") {
						continue
					} else {
						require.NoError(t, err)
					}
				}

				n, err := c.Read(buf)
				require.NoError(t, err)

				assert.True(t, c.(*tls.Conn).ConnectionState().HandshakeComplete)
				assert.Equal(t, testSNI.String(), c.(*tls.Conn).ConnectionState().ServerName)

				_, err = c.Write(buf[:n])
				require.NoError(t, err)

				_ = c.Close()
			}
		}
	})

	D, err := NewDialler(
		WithAddress(listener.Addr().String()),
		WithSni(testSNI.String()),
		WithInsecure(),
	)
	require.NoError(t, err)

	conn, err := D.Dial()
	require.NoError(t, err)

	// test write
	n, err := conn.Write(testData)
	require.NoError(t, err)
	assert.Equal(t, len(testData), n)

	// test read
	buf := make([]byte, 1000)
	n, err = conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, len(testData), n)
	assert.Equal(t, testData, buf[:n])

	// close
	_ = conn.Close()

	// clean up
	testCancel()
	wgOuter.Wait()

}

func TestDialler_Error_CantConnect(t *testing.T) {

	// get listener addr
	listener, err := nettest.NewLocalListener("tcp4")
	require.NoError(t, err)

	// introduce error
	_ = listener.Close()

	// test
	D, err := NewDialler(
		WithAddress(listener.Addr().String()),
		WithSni(uuid.New().String()),
		WithInsecure(),
	)
	require.NoError(t, err)
	_, err = D.Dial()
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "connection refused"))
}

func TestDialler_Error_TLSError(t *testing.T) {

	// get listener addr
	listener, err := nettest.NewLocalListener("tcp4")
	require.NoError(t, err)

	// prep test config
	testCtx, testCancel := context.WithCancel(context.Background())

	wgOuter := sync.WaitGroup{}

	// launch listener accepter
	wgOuter.Go(func() {

		buf := make([]byte, 1000)

		for {
			select {
			case <-testCtx.Done():
				return
			default:
				err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second))
				require.NoError(t, err)

				c, err := listener.Accept()
				if err != nil {
					if strings.Contains(err.Error(), "timeout") {
						continue
					} else {
						require.NoError(t, err)
					}
				}

				n, err := c.Read(buf)
				require.NoError(t, err)

				_, err = c.Write(buf[:n])
				require.NoError(t, err)

				_ = c.Close()
			}
		}
	})

	// test
	D, err := NewDialler(
		WithAddress(listener.Addr().String()),
		WithSni(uuid.New().String()),
		WithInsecure(),
	)
	require.NoError(t, err)
	_, err = D.Dial()
	require.Error(t, err)

	// clean up
	testCancel()
	wgOuter.Wait()

}
