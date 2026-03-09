# Stunnel (Secure Tunnel)

## Overview

The `stunnel` package provides TLS wrappers for TCP connections, offering both listener (server) and dialer (client) functionality. It handles TLS handshakes, certificate management, SNI-based authentication, and automatic certificate reloading.

## Why This Package?

### The TLS Complexity

**TLS setup is repetitive**:
- Certificate loading
- TLS configuration
- Handshake management
- Deadline handling
- Error handling

**Without abstraction**:
```go
cert, _ := tls.LoadX509KeyPair(certFile, keyFile)
config := &tls.Config{Certificates: []tls.Certificate{cert}}
listener, _ := tls.Listen("tcp", ":443", config)
for {
    conn, _ := listener.Accept()
    go handleConnection(conn.(*tls.Conn))
}
```

**With stunnel**:
```go
listener, _ := stunnel.NewListener(
    stunnel.WithHostPort(":443"),
    stunnel.WithTLSCertificate("cert.pem", "key.pem"),
    stunnel.WithConnectionHandler(handleConnection),
    stunnel.WithAuthenticator(authenticate),
)
listener.Listen(ctx)
```

## Listener (Server Side)

### Basic Setup

```go
listener, err := stunnel.NewListener(
    stunnel.WithHostPort("0.0.0.0:30105"),
    stunnel.WithTLSCertificate("/path/to/cert.pem", "/path/to/key.pem"),
    stunnel.WithConnectionHandler(handler),
    stunnel.WithAuthenticator(auth),
)

err = listener.Listen(context.Background())
```

### Required Options

**Host and port**:
```go
stunnel.WithHostPort("0.0.0.0:30105")
```

**TLS certificate**:
```go
stunnel.WithTLSCertificate("/path/to/cert.pem", "/path/to/key.pem")
```

**Connection handler**:
```go
type ConnectionHandler func(conn net.Conn, apiKey string) error

handler := func(conn net.Conn, apiKey string) error {
    // Handle authenticated connection
    // API key extracted from SNI
    return processConnection(conn, apiKey)
}

stunnel.WithConnectionHandler(handler)
```

**Authenticator** (optional but recommended):
```go
type AuthenticationHandler func(apiKey string) (bool, error)

auth := func(apiKey string) (bool, error) {
    return feederCache.Authenticate(apiKey, protocol)
}

stunnel.WithAuthenticator(auth)
```

### Connection Flow

**Complete flow**:
```
1. Client connects → TLS handshake (10s deadline)
2. Extract API key from SNI (ServerName)
3. Authenticate API key
4. Pass to handler if valid
5. Handler processes connection
6. Connection closes
```

**Handshake sequence**:
```go
// 1. Set handshake deadline
conn.SetDeadline(time.Now().Add(10 * time.Second))

// 2. Perform TLS handshake
conn.(*tls.Conn).Handshake()

// 3. Verify handshake completed
if !conn.(*tls.Conn).ConnectionState().HandshakeComplete {
    conn.Close()
    return
}

// 4. Remove deadline (connection established)
conn.SetDeadline(time.Time{})

// 5. Extract API key from SNI
apiKey := conn.(*tls.Conn).ConnectionState().ServerName
```

**Why 10-second handshake deadline**:
- Prevents slow handshake attacks
- Most handshakes complete <1 second
- Slow networks: 10s is generous

### SNI-Based Authentication

**Server Name Indication (SNI)**: TLS extension carrying hostname

**Client sends**:
```go
// Client sets SNI to API key
tlsConfig := &tls.Config{
    ServerName: "feeder-api-key-abc123",
}
conn, _ := tls.Dial("tcp", "server:30105", tlsConfig)
```

**Server extracts**:
```go
apiKey := conn.(*tls.Conn).ConnectionState().ServerName
// apiKey = "feeder-api-key-abc123"
```

**Why SNI for API keys**:
- Available before application data sent
- Part of TLS handshake
- Can reject before reading payload
- No custom protocol needed

**Security consideration**: SNI is unencrypted (visible to network observers)
- Don't use SNI for secrets
- OK for API keys (authentication, not encryption)
- TLS encrypts payload after handshake

### Authentication

**Handler called with API key**:
```go
valid, err := authHandler(apiKey)
if err != nil || !valid {
    conn.Close()  // Reject connection
    return
}
// Connection authenticated, proceed
```

**Authentication failures**:
- Invalid API key → Connection closed immediately
- Auth error → Connection closed, error logged
- Missing authenticator → Connections accepted (unsafe!)

**Production recommendation**: Always provide authenticator

### Automatic Certificate Reloading

**Every 5 minutes**:
```go
cancelTicker := timing.RunOnTicker(
    l.log,
    5*time.Minute,
    l.ReloadCertificate,
)
defer cancelTicker()
```

**Why reload**: Certificate renewal (Let's Encrypt)
- Certificates expire (typically 90 days)
- Renewal happens while server running
- Auto-reload picks up new cert
- No restart required

**Reload process**:
```go
func (l *Listener) ReloadCertificate() error {
    cert, err := tls.LoadX509KeyPair(l.certPath, l.keyPath)
    if err != nil {
        return err  // Old cert still in use
    }

    l.muCert.Lock()
    defer l.muCert.Unlock()
    l.cert = &cert  // Atomic swap

    return nil
}
```

**Thread-safe**: Mutex protects certificate pointer

**Failure handling**: Old certificate continues if reload fails
- Logged but not fatal
- Next reload (5 min) will retry
- Service continues with existing cert

**GetCertificate callback**:
```go
config := &tls.Config{
    GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
        l.muCert.Lock()
        defer l.muCert.Unlock()
        return l.cert, nil  // Returns current cert
    },
}
```

**Per-connection**: Called for each new connection
- Always gets latest certificate
- Seamless cert rotation

### Graceful Shutdown

**Context-based**:
```go
ctx, cancel := context.WithCancel(context.Background())
go listener.Listen(ctx)

// Later: shutdown
cancel()
```

**Shutdown sequence**:
```go
select {
case <-ctx.Done():
    l.log.Debug().Msg("context has finished")
}

// 1. Close listener (no new connections)
netListener.Close()

// 2. Wait for active connections to finish
wg.Wait()
```

**Existing connections**: Continue until handler returns

**New connections**: Rejected after Close()

## Dialer (Client Side)

### Basic Setup

```go
dialer, err := stunnel.NewDialler(
    stunnel.WithAddress("server.example.com:30105"),
    stunnel.WithSni("my-api-key"),
    stunnel.WithTimeout(10*time.Second),
)

conn, err := dialer.Dial()
```

### Options

**Server address**:
```go
stunnel.WithAddress("server:30105")
```

**SNI (API key)**:
```go
stunnel.WithSni("feeder-api-key-abc123")
```

**Timeout**:
```go
stunnel.WithTimeout(10 * time.Second)  // Connection timeout
```

**Insecure mode** (skip verification):
```go
stunnel.WithInsecure()  // DON'T USE IN PRODUCTION
```

### Certificate Verification

**Custom verification**:
```go
customVerify := func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
    for _, rawCert := range rawCerts {
        cert, _ := x509.ParseCertificate(rawCert)

        if !cert.IsCA {  // Leaf certificate
            // 1. Verify hostname
            err := cert.VerifyHostname(remoteHost)

            // 2. Verify against system CAs
            scp, _ := x509.SystemCertPool()
            _, err = cert.Verify(x509.VerifyOptions{
                Roots:   scp,
                DNSName: remoteHost,
            })
        }
    }
    return nil
}
```

**Why custom verify**:
- `InsecureSkipVerify: true` to use custom function
- Still validates certificates (not actually insecure)
- Allows hostname verification
- Checks against system CAs

**What it checks**:
1. **Hostname matches**: Certificate valid for server hostname
2. **Trusted CA**: Certificate signed by system-trusted CA
3. **Not expired**: Certificate within validity period
4. **Chain of trust**: All intermediate certificates valid

**Insecure mode**: Skips all verification
```go
if !D.insecure && !cert.IsCA {
    // Verify certificate
}
// If insecure=true, skips this block
```

**When to use insecure**:
- ❌ Never in production
- ✓ Local development (self-signed certs)
- ✓ Testing (controlled environment)

### Connection Establishment

**Dial and handshake**:
```go
func (D *Dialer) Dial() (*tls.Conn, error) {
    // 1. TCP connection
    conn, err := tls.DialWithDialer(D.dialer, "tcp", D.address, D.tlsConfig)

    // 2. TLS handshake
    err = conn.Handshake()

    return conn, nil
}
```

**Automatic**:
- TCP connection
- TLS handshake
- Certificate verification
- Returns ready-to-use connection

**Errors**:
- Connection refused: Server down
- Handshake timeout: Network issue
- Certificate error: Invalid cert or hostname mismatch
- Verification failed: Untrusted CA

## Use Cases

### MLAT Bridge

**From mlatbridge package**:
```go
mb.listener, err = stunnel.NewListener(
    stunnel.WithHostPort(mb.hostPort),
    stunnel.WithTLSCertificate(mb.certPath, mb.keyPath),
    stunnel.WithConnectionHandler(mb.handler),
    stunnel.WithAuthenticator(mb.authenticator),
)

err = mb.listener.Listen(ctx)
```

**Authenticates feeders**: SNI carries API key

**Handler bridges**: Connects to MLAT server

### Feeder Client

```go
dialer, _ := stunnel.NewDialler(
    stunnel.WithAddress("bridge.plane.watch:30105"),
    stunnel.WithSni("my-feeder-api-key"),
)

conn, _ := dialer.Dial()

// Send Beast frames
writer := bufio.NewWriter(conn)
writer.Write(beastFrame)
writer.Flush()
```

**SNI authentication**: Server extracts API key, validates

## Security Considerations

### Certificate Best Practices

**Use valid certificates**:
- Let's Encrypt (free, automated)
- Commercial CA (DigiCert, GlobalSign)
- Not self-signed in production

**Certificate renewal**:
- Auto-reload handles renewal seamlessly
- Monitor expiry dates
- Test renewal process

**Key permissions**:
```bash
chmod 600 /path/to/key.pem  # Owner read/write only
chown appuser:appuser /path/to/key.pem
```

**Key security**:
- Never commit to git
- Restrict filesystem access
- Rotate periodically (annually)

### SNI Visibility

**SNI is unencrypted**: Visible to network observers

**What's exposed**:
- API key in SNI field
- Server hostname

**What's NOT exposed**:
- Connection payload (encrypted)
- Application data

**Mitigation**: Use ESNI (Encrypted SNI)
- Not widely supported yet
- Future enhancement

<!--
Maintainers: If you implement ESNI, document here
-->

### Insecure Mode

**Never use in production**:
```go
// BAD in production
stunnel.WithInsecure()
```

**Allows**:
- Self-signed certificates
- Expired certificates
- Hostname mismatch
- Untrusted CAs

**Opens vulnerabilities**:
- Man-in-the-middle attacks
- Certificate spoofing
- No server identity verification

**Development only**: Use for local testing, disable for deployment

## Performance Characteristics

### TLS Overhead

**Handshake cost**: ~5-10ms per connection
- Certificate verification
- Key exchange
- Cipher negotiation

**Persistent connections**: Handshake once, reuse
```go
conn, _ := dialer.Dial()
defer conn.Close()

// Many operations on same connection
for {
    write(conn, data)
}
```

**Connection pooling**: Reuse connections across requests

### Certificate Reload

**5-minute interval**: Negligible overhead
- Single file read
- Minimal CPU
- Mutex lock brief

**Not triggered by connections**: Runs independently

### Memory

**Per connection**:
- TLS state: ~10 KB
- Buffers: ~16 KB
- **Total**: ~25-30 KB per connection

**1000 connections**: ~25-30 MB

## Common Issues

### Certificate Not Found

**Symptom**: Listener fails to start

**Error**: "no such file or directory"

**Cause**: Incorrect certificate paths
```go
WithTLSCertificate("/wrong/path/cert.pem", "/wrong/path/key.pem")
```

**Solution**: Verify file paths
```bash
ls -l /path/to/cert.pem /path/to/key.pem
```

### Handshake Timeout

**Symptom**: Connections fail after 10 seconds

**Cause**: Slow TLS handshake

**Common reasons**:
1. Network latency
2. CPU-bound server (slow crypto)
3. Large certificate chain

**Solution**: Increase deadline (listener.go line 96)
```go
conn.SetDeadline(time.Now().Add(30 * time.Second))  // Increase to 30s
```

### Hostname Verification Failed

**Symptom**: Dialer fails with "hostname doesn't match"

**Cause**: Certificate hostname ≠ connection hostname
```go
// Certificate for: server.example.com
// Connecting to: 192.168.1.10  ❌
WithAddress("192.168.1.10:30105")
```

**Solution**: Connect using hostname
```go
WithAddress("server.example.com:30105")  ✓
```

**Or**: Add IP to certificate SAN (Subject Alternative Names)

### Authentication Always Fails

**Symptom**: All connections rejected

**Debug**:
```go
auth := func(apiKey string) (bool, error) {
    log.Info().Str("apiKey", apiKey).Msg("Authenticating")
    valid, err := authenticate(apiKey)
    log.Info().Bool("valid", valid).Err(err).Msg("Auth result")
    return valid, err
}
```

**Check**:
1. API key extracted correctly (log shows it)
2. Authentication backend reachable
3. API key in auth database

### Certificate Reload Failures

**Symptom**: Logs show reload errors every 5 minutes

**Cause**: File permissions or renewal issues

**Check permissions**:
```bash
ls -l /path/to/cert.pem /path/to/key.pem
# Should be readable by app user
```

**Check certificate validity**:
```bash
openssl x509 -in /path/to/cert.pem -noout -dates
```

**Impact**: Old certificate continues working until it expires

## Production Patterns

### Let's Encrypt Integration

**Certbot renewal**:
```bash
certbot renew --deploy-hook "systemctl reload app"
```

**Auto-reload handles renewal**: No hook needed with stunnel
- Renewal writes new files
- Auto-reload picks them up (within 5 min)

**Monitoring**: Alert if reload fails
```promql
rate(stunnel_cert_reload_errors[5m]) > 0
```

### Health Monitoring

**Track connections**:
```go
var activeConnections prometheus.Gauge

handler := func(conn net.Conn, apiKey string) error {
    activeConnections.Inc()
    defer activeConnections.Dec()

    return processConnection(conn, apiKey)
}
```

**Track handshake failures**:
```go
var handshakeFailures prometheus.Counter

// In handleIncoming, after handshake
if err != nil {
    handshakeFailures.Inc()
}
```

### Multiple Listeners

**Different ports/certificates**:
```go
// Production listener
prodListener, _ := stunnel.NewListener(
    stunnel.WithHostPort(":443"),
    stunnel.WithTLSCertificate("prod-cert.pem", "prod-key.pem"),
    stunnel.WithConnectionHandler(prodHandler),
)

// Admin listener
adminListener, _ := stunnel.NewListener(
    stunnel.WithHostPort(":8443"),
    stunnel.WithTLSCertificate("admin-cert.pem", "admin-key.pem"),
    stunnel.WithConnectionHandler(adminHandler),
)

go prodListener.Listen(ctx)
go adminListener.Listen(ctx)
```

## Future Enhancements

<!--
Maintainers: Document enhancements as implemented
-->

### Metrics Integration

**Proposed**: Built-in Prometheus metrics
```go
type Metrics struct {
    Connections      prometheus.Counter
    HandshakeErrors  prometheus.Counter
    AuthFailures     prometheus.Counter
    CertReloads      prometheus.Counter
}

WithMetrics(metrics)
```

### ESNI Support

**Encrypted SNI**: Hide SNI field
```go
tlsConfig.EncryptedClientHello = true
```

**Requires**: TLS 1.3, server support

### Client Certificates

**Mutual TLS**: Client also presents certificate
```go
tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
```

**Use case**: Machine-to-machine authentication

## File Guide

| File | Purpose |
|------|---------|
| `listener.go` | TLS server, handshake, auth, cert reload |
| `dialer.go` | TLS client, custom verification |
| `missingoption.go` | Error type for config validation |
| `*_test.go` | Unit tests |

## See Also

- [MLATBridge](../mlatbridge/README.md) - Uses stunnel for secure connections
- [Timing](../timing/README.md) - Certificate reload ticker

## References

- Go TLS package: https://pkg.go.dev/crypto/tls
- SNI RFC: https://tools.ietf.org/html/rfc6066#section-3
- X.509 certificates: https://pkg.go.dev/crypto/x509
- Let's Encrypt: https://letsencrypt.org/
