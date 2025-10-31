package stunnel

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
	"time"
)

type (
	Dialer struct {
		address   string
		sni       string
		insecure  bool
		dialer    *net.Dialer
		tlsConfig *tls.Config
	}

	DialerOption func(*Dialer)
)

func WithTimeout(timeout time.Duration) DialerOption {
	return func(d *Dialer) {
		d.dialer.Timeout = timeout
	}
}

func WithAddress(address string) DialerOption {
	return func(d *Dialer) {
		d.address = address
	}
}

func WithSni(sni string) DialerOption {
	return func(d *Dialer) {
		d.sni = sni
	}
}

func WithInsecure() DialerOption {
	return func(d *Dialer) {
		d.insecure = true
	}
}

func NewDialler(opts ...DialerOption) (*Dialer, error) {

	D := &Dialer{
		dialer: new(net.Dialer),
	}
	for _, opt := range opts {
		opt(D)
	}

	// sanity checks
	if D.address == "" {
		return nil, fmt.Errorf("%w: Host/Port information is not configured", MissingOption)
	}

	// split host/port from addr
	remoteHost := strings.Split(D.address, ":")[0]

	// define custom cert verification function
	customVerify := func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {

		// for each cert in chain sent by server
		for _, rawCert := range rawCerts {

			// parse the cert
			cert, err := x509.ParseCertificate(rawCert)
			if err != nil {
				return err
			}

			// if the certificate is not a CA, then check it
			if !D.insecure && !cert.IsCA {

				// ensure the certificate hostname matches the host we're trying to connect to
				err := cert.VerifyHostname(remoteHost)
				if err != nil {
					return err
				}

				// load system cert pool CAs
				scp, err := x509.SystemCertPool()
				if err != nil {
					return err
				}

				// verify server cert
				vo := x509.VerifyOptions{}
				vo.Roots = scp
				vo.Intermediates = scp
				vo.DNSName = remoteHost
				_, err = cert.Verify(vo)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}

	// load root CAs
	scp, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}

	// set up tls config
	D.tlsConfig = &tls.Config{
		RootCAs:               scp,
		ServerName:            D.sni,
		InsecureSkipVerify:    true,
		VerifyPeerCertificate: customVerify,
	}

	return D, err
}

func (D *Dialer) Dial() (*tls.Conn, error) {
	// dial remote
	conn, err := tls.DialWithDialer(D.dialer, "tcp", D.address, D.tlsConfig)
	if err != nil {
		return conn, err
	}

	// perform handshake
	err = conn.Handshake()
	if err != nil {
		return conn, err
	}
	return conn, nil
}
