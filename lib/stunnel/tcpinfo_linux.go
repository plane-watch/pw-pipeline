//go:build linux

package stunnel

import (
	"crypto/tls"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// GetTCPInfo returns RTT from TCP_INFO for a connection.
// Works with both plain TCP and TLS connections (unwraps TLS automatically).
// Returns 0 duration if the connection type doesn't support TCP_INFO.
func GetTCPInfo(conn net.Conn) (rtt time.Duration, err error) {
	// Unwrap TLS if needed
	if tlsConn, ok := conn.(*tls.Conn); ok {
		conn = tlsConn.NetConn()
	}

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return 0, nil // Not a TCP connection, skip
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return 0, err
	}

	var info unix.TCPInfo
	var controlErr error
	err = rawConn.Control(func(fd uintptr) {
		controlErr = unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO, &info)
	})
	if err != nil {
		return 0, err
	}
	if controlErr != nil {
		return 0, controlErr
	}

	// RTT is in microseconds
	return time.Duration(info.Rtt) * time.Microsecond, nil
}
