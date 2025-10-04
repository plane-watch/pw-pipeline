package stunnel

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

var testHostPort = "localhost:34543"

func TestListener_Listen(t *testing.T) {
	authenticatorCalled := false
	handlerCalled := false

	l, err := New(
		WithHostPort(testHostPort),
		WithTLSCertificate("testdata/cert.crt", "testdata/cert.key"),
		WithAuthenticator(func(apiKey string) (bool, error) {
			authenticatorCalled = true
			t.Log("authenticator called")
			if apiKey != "test1234" {
				t.Errorf("Expected `test123` as the API Key, got: `%s`", apiKey)
			}
			return true, nil
		}),
		WithConnectionHandler(func(conn net.Conn, apiKey string) error {
			handlerCalled = true
			t.Log("handler called")
			if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Errorf("failed to set read deadline: %s", err)
			}
			if apiKey != "test1234" {
				t.Errorf("Expected `test123` as the API Key, got: `%s`", apiKey)
			}

			for {
				b := make([]byte, 64)
				n, err := conn.Read(b)
				if err != nil {
					t.Logf("Connection error: %s", err)
					if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
						return nil
					}
				} else {
					t.Logf("Read %d bytes", n)
				}
			}
		}),
	)
	if err != nil {
		t.Errorf("failed to create test listener: %s", err)
		t.Fail()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		err = l.Listen(ctx)
		if err != nil {
			t.Errorf("Failed to listen")
			cancel()
			return
		}
	}()

	time.Sleep(time.Millisecond * 50) // lets our server start
	t.Log("Client: connecting to server")
	conn, err := tls.Dial("tcp", testHostPort, &tls.Config{ServerName: "test1234", InsecureSkipVerify: true})
	if err != nil {
		t.Errorf("failed to connect to our local server: %s", err)
		cancel()
		return
	}

	t.Log("Client: writing test data to server")
	wl, err := conn.Write([]byte("test-data"))
	if err != nil {
		t.Errorf("failed to write to our local server: %s", err)
	}

	if wl != 9 {
		t.Errorf("Wrote the wrong number of bytes: expected 9, got %d", wl)
	}

	time.Sleep(time.Millisecond * 50) // lets our server handle the data
	t.Log("Client: closing connection")
	_ = conn.Close()

	cancel()

	if !authenticatorCalled {
		t.Errorf("Expected our connection to call the authenticator")
	}
	if !handlerCalled {
		t.Errorf("Expected our connection to call the handler")
	}
}
