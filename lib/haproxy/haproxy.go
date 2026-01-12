package haproxy

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
)

type Conn struct {
	network      string
	address      string
	conn         net.Conn
	connected    bool
	commandMutex sync.Mutex
}

func New(network string, address string) (*Conn, error) {
	return &Conn{
		network: network,
		address: address,
		conn:    nil,
	}, nil
}

func (conn *Conn) connect() error {
	var err error
	conn.conn, err = net.Dial(conn.network, conn.address)
	if err == nil {
		conn.connected = true
	}
	return err
}

func (conn *Conn) disconnect() error {
	if !conn.connected {
		return nil
	}
	err := conn.conn.Close()
	if err == nil {
		conn.connected = false
	}
	return err
}

func (conn *Conn) doCommand(cmd string) (output string, err error) {
	conn.commandMutex.Lock()
	defer conn.commandMutex.Unlock()

	cmd = ensureTrailingNewline(cmd)

	err = conn.connect()
	if err != nil {
		return "", fmt.Errorf("connection error: %w", err)
	}

	defer func() {
		err = conn.disconnect()
		if err != nil {
			err = fmt.Errorf("disconnection error: %w", err)
		}
	}()

	_, err = conn.conn.Write([]byte(cmd))
	if err != nil {
		return "", fmt.Errorf("net.Conn write error: %w", err)
	}

	out, err := io.ReadAll(conn.conn)
	if err != nil {
		return "", fmt.Errorf("net.Conn read error: %w", err)
	}

	return string(out), nil
}

func ensureTrailingNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
