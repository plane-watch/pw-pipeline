package stunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/nettest"
)

var (
	testKeyFile, testCertFile *os.File
)

func GenerateSelfSignedTLSCertAndKey(keyFile, certFile *os.File) error {
	// Thanks to: https://go.dev/src/crypto/tls/generate_cert.go

	// prep certificate info
	hosts := []string{"localhost"}
	ipAddrs := []net.IP{net.IPv4(127, 0, 0, 1)}
	notBefore := time.Now()
	notAfter := time.Now().Add(time.Minute * 15)
	//isCA := true

	// generate private key
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}

	keyUsage := x509.KeyUsageDigitalSignature

	// generate serial number
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	// prep cert template
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"plane.watch"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              keyUsage,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// add hostname(s)
	for _, host := range hosts {
		template.DNSNames = append(template.DNSNames, host)
	}

	// add ip(s)
	for _, ip := range ipAddrs {
		template.IPAddresses = append(template.IPAddresses, ip)
	}

	// if self-signed, include CA
	//if isCA {
	template.IsCA = true
	template.KeyUsage |= x509.KeyUsageCertSign
	//}

	// create certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, priv.Public().(ed25519.PublicKey), priv)
	if err != nil {
		return err
	}

	// encode certificate
	err = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if err != nil {
		return err
	}

	// marhsal private key
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}

	// write private key
	err = pem.Encode(keyFile, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	if err != nil {
		return err
	}

	return nil
}

func TestMain(m *testing.M) {
	var err error

	// prep cert file
	testCertFile, err = os.CreateTemp("", "stunnel_unit_testing_*_cert.pem")
	if err != nil {
		log.Fatal(err)
	}

	// prep key file
	testKeyFile, err = os.CreateTemp("", "stunnel_unit_testing_*_key.pem")
	if err != nil {
		log.Fatal(err)
	}

	// generate cert/key for testing
	err = GenerateSelfSignedTLSCertAndKey(testKeyFile, testCertFile)
	if err != nil {
		log.Fatal(err)
	}

	// close cert & key files
	_ = testCertFile.Close()
	_ = testKeyFile.Close()

	// run tests
	exitCode := m.Run()

	// clean-up after tests
	_ = os.Remove(testCertFile.Name())
	_ = os.Remove(testKeyFile.Name())

	// return exit code
	os.Exit(exitCode)
}

func TestStunnel(t *testing.T) {

	// get listener addr
	listener, err := nettest.NewLocalListener("tcp4")
	require.NoError(t, err, "test setup: nettest.NewLocalListener failed")
	err = listener.Close()
	require.NoError(t, err, "test setup: listener.Close() failed")

	testSNI := uuid.New().String()
	testData := []byte("The quick brown fox jumps over the lazy dog 9876543210 times")

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
	}()

	L, err := NewListener(
		WithHostPort(listener.Addr().String()),
		WithAuthenticator(func(apiKey string) (bool, error) {
			if apiKey != testSNI {
				require.Equal(t, testSNI, apiKey, "apiKey passed to AuthenticationHandler did not match testSNI")
				return false, nil
			}
			return true, nil
		}),
		WithConnectionHandler(func(conn net.Conn, apiKey string) error {
			require.Equal(t, testSNI, apiKey, "apiKey passed to ConnectionHandler did not match testSNI")
			buf := make([]byte, 1024)
			n, err := conn.Read(buf)
			require.NoError(t, err, "error reading from connection in ConnectionHandler")
			require.Equal(t, testData, buf[:n], "read returned wrong data from connection in ConnectionHandler")
			return nil
		}),
		WithTLSCertificate(testCertFile.Name(), testKeyFile.Name()),
	)
	require.NoError(t, err, "error creating listener")

	testWg := sync.WaitGroup{}

	testWg.Go(func() {
		err = L.Listen(ctx)
		require.NoError(t, err, "error listening on listener")
	})
	time.Sleep(1 * time.Second) // wait for listener to listen

	D, err := NewDialler(
		WithAddress(listener.Addr().String()),
		WithSni(testSNI),
		WithTimeout(1*time.Second),
	)
	require.NoError(t, err, "error creating dialler")

	conn, err := D.Dial()
	require.NoError(t, err, "error dialing listener")

	_, err = conn.Write(testData)
	require.NoError(t, err, "error writing to connection")

	defer func() {
		_ = conn.Close()
	}()

	cancel()
	testWg.Wait()
}
