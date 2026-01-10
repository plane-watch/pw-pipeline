//go:build !linux

package stunnel

import (
	"net"
	"time"
)

// GetTCPInfo returns 0 on non-Linux platforms where TCP_INFO is not available.
func GetTCPInfo(_ net.Conn) (rtt time.Duration, err error) {
	return 0, nil
}
