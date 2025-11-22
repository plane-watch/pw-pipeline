# NATS Client Wrapper

## Overview

The `nats_io` package provides a thin wrapper around the NATS messaging client, offering separate incoming/outgoing connections, channel-based subscriptions, error handling, and observability. It simplifies NATS integration while exposing enough control for performance tuning and debugging.

## Why NATS?

### Messaging Requirements

Aircraft tracking pipeline needs:
- **Pub/sub**: One publisher, many subscribers (position updates)
- **Request/reply**: Synchronous queries (feeder stats)
- **Queue groups**: Load balancing work across instances
- **Low latency**: Real-time position updates
- **High throughput**: 1000s of messages/second

### Alternatives Considered

**RabbitMQ**:
- ✓ Feature-rich (routing, persistence, etc.)
- ❌ Heavy (Erlang runtime, complexity)
- ❌ Slower (disk persistence overhead)
- ❌ Complex setup/clustering

**Kafka**:
- ✓ Very high throughput
- ✓ Persistence (log replay)
- ❌ Overkill (zookeeper, partitions)
- ❌ Higher latency
- ❌ Complex operations

**Redis Pub/Sub**:
- ✓ Simple, fast
- ❌ No persistence (messages lost if disconnected)
- ❌ No request/reply pattern
- ❌ Not purpose-built for messaging

**NATS**:
- ✓ Very simple (single binary)
- ✓ Very fast (millions msg/sec)
- ✓ Supports pub/sub, request/reply, queue groups
- ✓ Easy clustering/HA
- ✓ Fire-and-forget (appropriate for real-time data)
- ❌ No persistence (use JetStream for persistence)

**NATS wins for**: Simplicity + performance + appropriate delivery guarantees

## Dual Connection Pattern

### Incoming vs Outgoing

**Two separate connections**:
```go
incoming *nats.Conn  // For subscriptions (receiving)
outgoing *nats.Conn  // For publishing (sending)
```

**Why separate connections?**:

1. **Failure isolation**:
   ```
   Outgoing connection fails → Can still receive messages
   Incoming connection fails → Can still publish messages
   ```

2. **Monitoring clarity**:
   ```
   Incoming connection metrics → Subscription health
   Outgoing connection metrics → Publishing health
   ```

3. **Resource allocation**:
   ```
   Incoming: Large receive buffers (subscriptions)
   Outgoing: Small send buffers (publishes)
   ```

4. **Connection naming**:
   ```
   connectionName+"+incoming"  // Visible in NATS monitoring
   connectionName+"+outgoing"
   ```

**When to use one vs both**:
```go
// Publisher only
WithConnections(false, true)  // incoming=false, outgoing=true

// Subscriber only
WithConnections(true, false)  // incoming=true, outgoing=false

// Both (default)
WithConnections(true, true)   // Both enabled
```

**Example use cases**:
- **Sink**: Outgoing only (publishes positions, no subscriptions)
- **Feederauth**: Both (subscribes to updates, publishes responses)
- **Consumer**: Incoming only (subscribes to positions, doesn't publish)

### Connection Setup

**Initialization**:
```go
server, err := nats_io.NewServer(
    nats_io.WithServer("nats://localhost:4222", "pipeline-instance-1"),
    nats_io.WithConnections(true, true),
)
```

**Automatic connection**:
```go
if err := n.Connect(); nil != err {
    return nil, err
}
```

**No retry**: `NewServer()` fails immediately if NATS unreachable
- Fail fast at startup
- Better than starting with broken messaging

**Reconnection**: NATS client handles automatically
- Built-in exponential backoff
- Transparent to application
- Messages buffered during reconnect (up to limits)

## URL Handling

### Default Port

**Port auto-fill**:
```go
func (n *Server) SetURL(serverURL string) {
    serverURLParts, err := url.Parse(serverURL)
    if serverURLParts.Port() == "" {
        serverURLParts.Host = net.JoinHostPort(serverURLParts.Hostname(), "4222")
    }
    n.url = serverURLParts.String()
}
```

**Convenience**:
```go
// These are equivalent:
SetURL("nats://localhost")
SetURL("nats://localhost:4222")
```

**Why 4222**: NATS default port

**Multiple URLs** (HA): NATS client supports comma-separated
```
nats://server1:4222,nats://server2:4222,nats://server3:4222
```

## Publishing

### Simple Publish

**Basic publishing**:
```go
err := server.Publish("aircraft.positions", jsonData)
```

**Flow**:
1. Check outgoing connection exists
2. Publish to subject
3. Handle connection errors

**Error handling**:
```go
if errors.Is(err, nats.ErrInvalidConnection) ||
   errors.Is(err, nats.ErrConnectionClosed) ||
   errors.Is(err, nats.ErrConnectionDraining) {
    n.log.Error().Err(err).Msg("Connection not in a valid state")
}
```

**Connection states**:
- `ErrInvalidConnection`: Not connected yet
- `ErrConnectionClosed`: Closed by user/server
- `ErrConnectionDraining`: Graceful shutdown in progress

**Delivery guarantee**: At-most-once
- Message may be lost if NATS down
- No retry by default
- Appropriate for real-time data (stale positions don't matter)

### Publish with Headers

**Header support**:
```go
headers := map[string]string{
    "type":      "beast",
    "source":    "feeder-123",
    "timestamp": "2024-11-16T14:23:45Z",
}
err := server.PublishWithHeaders("mlat.frames", beastData, headers)
```

**Why headers**:
- Metadata without touching payload
- Subject filtering on consumer side
- Protocol versioning (Content-Type, etc.)

**Example consumer filtering**:
```go
// Only process Beast frames
if msg.Header.Get("type") != "beast" {
    return
}
```

**Header overhead**: ~50-100 bytes
- Negligible for large payloads (KB+)
- Matters for tiny payloads (<100 bytes)

## Subscribing

### Channel-Based Subscriptions

**Subscribe pattern**:
```go
ch, err := server.Subscribe("aircraft.positions")
for msg := range ch {
    // Process message
    position := decodePosition(msg.Data)
    updateDisplay(position)
}
```

**Why channels**: Idiomatic Go concurrency
- Easy integration with select statements
- Natural backpressure (buffered channels)
- Goroutine-safe

**Channel buffer depth**:
```go
ch := make(chan *nats.Msg, n.QueueDepth)  // Default: 2048
```

**Why 2048**:
- Balance: Memory vs message loss
- At 1000 msg/sec: ~2 second buffer
- Prevents drops during brief processing spikes

**Buffer full**: NATS client drops messages
- Slow consumer problem
- Triggers `ErrSlowConsumer`
- Logged via error handler

### Queue Groups

**Load balancing pattern**:
```go
// Instance 1
ch1, _ := server.SubscribeQueueGroup("work.jobs", "workers")

// Instance 2
ch2, _ := server.SubscribeQueueGroup("work.jobs", "workers")

// Messages distributed round-robin between instances
```

**Use case**: Scale horizontally
- Same queue group name = load balanced
- Different names = fanout (all receive)

**Example**:
```
Publisher → "work.jobs" →  Instance 1 (queue: workers) → Gets msg 1, 3, 5...
                        →  Instance 2 (queue: workers) → Gets msg 2, 4, 6...
```

**No queue group**: All subscribers receive all messages
```go
// Fanout pattern (no queue group)
ch, _ := server.Subscribe("aircraft.positions")
// All subscribers get every position update
```

## Request-Reply Pattern

### Synchronous Requests

**Request with timeout**:
```go
data := []byte(`{"query": "feeder-stats"}`)
headers := map[string]string{"version": "v1"}

response, err := server.Request(
    "api.feeder.stats",
    data,
    headers,
    time.Second,  // Timeout
)
```

**What happens**:
1. Client publishes request with unique reply subject
2. Subscribes to reply subject (one-time)
3. Waits for response (up to timeout)
4. Returns response data or timeout error

**Why timeout required**: Prevent indefinite blocks
- Server might be down
- Handler might be stuck
- Reply might be lost

**Typical timeouts**:
- Fast queries: 100ms - 1s
- Database lookups: 1s - 5s
- Complex operations: 5s - 30s

**Error cases**:
```go
if err != nil {
    // Timeout: No response within deadline
    // or: Connection error
    // or: No responders listening
}
```

### Reply Subscriptions

**Server side** (respond to requests):
```go
handler := func(msg *nats.Msg) {
    // Process request
    data := processRequest(msg.Data)

    // Send reply
    msg.Respond(data)
}

sub, err := server.SubscribeReply("api.feeder.stats", "handlers", handler)
```

**Queue subscription**: Load balanced request handling
- Multiple handlers = higher throughput
- NATS distributes requests round-robin

**Why use outgoing connection**:
```go
func (n *Server) SubscribeReply(...) (*nats.Subscription, error) {
    return n.outgoing.QueueSubscribe(subject, queue, handler)
}
```

**Counter-intuitive**: Subscribing on outgoing connection
- Replies sent on same connection as subscription
- Keeps request/reply traffic together
- Alternative: Could use incoming connection (works either way)

## Error Handling

### Error Handler

**Custom error handler**:
```go
func (n *Server) NatsErrHandler(conn *nats.Conn, sub *nats.Subscription, err error) {
    l := n.log.Error().Err(err)

    // Log channel stats
    for _, c := range n.channels {
        if c.subject == sub.Subject {
            l.Int(c.name+" len", len(c.ch)).
              Int(c.name+" capacity", cap(c.ch))
        }
    }

    // Log connection/subscription details
    if conn != nil {
        l.Str("addr", conn.ConnectedUrl())
    }
    if sub != nil {
        l.Str("subscription", sub.Subject+"["+sub.Queue+"]")
    }

    l.Send()

    // Track slow consumer drops
    if errors.Is(err, nats.ErrSlowConsumer) {
        n.droppedMessageCounter.Inc()
    }
}
```

**Registered during connection**:
```go
nats.Connect(url, nats.ErrorHandler(n.NatsErrHandler))
```

**When called**:
- Slow consumer (buffer full)
- Connection lost
- Permission errors
- Invalid subjects

**Slow consumer handling**:
```go
if errors.Is(err, nats.ErrSlowConsumer) {
    n.droppedMessageCounter.Inc()  // Prometheus counter
}
```

**Why track separately**: Actionable metric
- High drops = Need faster processing or larger buffer
- Zero drops = System keeping up

### Channel Health Tracking

**Tracking subscriptions**:
```go
n.channels = append(n.channels, healthItem{
    name:    "sub-" + subject,
    subject: subject,
    ch:      ch,
})
```

**Why track**: Visibility into buffer utilization

**Error logs include stats**:
```
ERROR slow consumer
  sub-aircraft.positions len=2048
  sub-aircraft.positions capacity=2048
  subscription=aircraft.positions[]
```

**Immediate insight**: Buffer 100% full → Slow consumer

## Queue Depth Configuration

### Default Depth

**Constant**:
```go
const DefaultQueueDepth = 2048
```

**Why 2048**:
- Power of 2 (cache-friendly)
- ~2 seconds buffer at 1000 msg/sec
- ~2 MB RAM per channel (if 1KB messages)

**Trade-offs**:
| Depth | Buffer Duration (1k msg/s) | Memory (1KB msg) | Risk |
|-------|---------------------------|------------------|------|
| 256 | ~250ms | ~256 KB | High drop risk |
| 1024 | ~1s | ~1 MB | Moderate |
| 2048 | ~2s | ~2 MB | Low (default) |
| 8192 | ~8s | ~8 MB | Very low, more RAM |

**Customization**: Setter not exposed, edit `NewServer` to allow

<!--
Maintainers: If you expose QueueDepth setter, document here
-->

### Monitoring Queue Depth

**Health check logs utilization**:
```go
for _, item := range n.channels {
    l := len(item.ch)
    c := cap(item.ch)
    p := (float32(l) / float32(c)) * 100  // Percentage full

    n.log.Info().
        Int("# items", l).
        Int("max items", c).
        Float32("%", p).
        Str("channel", item.name).
        Msg("Channel Check")
}
```

**Example output**:
```
INFO Channel Check # items=50 max items=2048 %=2.4 channel=sub-aircraft.positions
INFO Channel Check # items=1800 max items=2048 %=87.9 channel=sub-work.jobs
```

**High utilization (>80%)**: Warning sign
- Consumer not keeping up
- Might start dropping soon
- Consider faster processing or more workers

## Health Checks

### Connection Health

**Health check implementation**:
```go
func (n *Server) HealthCheck() bool {
    incomingConnected := n.incoming != nil && n.incoming.IsConnected()
    outgoingConnected := n.outgoing != nil && n.outgoing.IsConnected()

    incomingOk := incomingConnected == n.connectIncoming
    outgoingOk := outgoingConnected == n.connectOutgoing

    return incomingOk && outgoingOk
}
```

**Logic**: Connection state matches intended state
- Wanted incoming but not connected → Unhealthy
- Don't want incoming and not connected → Healthy
- Wanted both, only one connected → Unhealthy

**Logging during check**:
```go
n.log.Info().
    Int("Num Channels", len(n.channels)).
    Bool("Incoming Wanted", n.connectIncoming).
    Bool("Incoming Connected", incomingConnected).
    Bool("Outgoing Wanted", n.connectOutgoing).
    Bool("Outgoing Connected", outgoingConnected).
    Send()
```

**Visibility**: See exactly which connection is down

### Integration with Monitoring

**Register health check**:
```go
import "plane.watch/lib/monitoring"

natsServer, _ := nats_io.NewServer(...)
monitoring.AddHealthCheck(natsServer)
```

**Health endpoint** (`/status`) includes NATS status

## Graceful Shutdown

**Close method**:
```go
func (n *Server) Close() {
    // Drain incoming (finish processing pending)
    if n.incoming != nil && n.incoming.IsConnected() {
        if err := n.incoming.Drain(); err != nil {
            n.log.Error().Err(err).Str("dir", "incoming").Msg("failed to drain connection")
        }
    }

    // Close outgoing immediately
    if n.outgoing != nil {
        n.outgoing.Close()
    }
}
```

**Why drain incoming**: Finish processing in-flight messages
- Prevents message loss
- Gives handlers time to complete
- Graceful shutdown

**Why close outgoing**: No pending publishes
- Publishing is fire-and-forget
- No need to drain
- Faster shutdown

**Drain behavior**:
1. Unsubscribe from all subjects
2. Wait for pending messages to be processed
3. Close connection

**Timeout**: NATS client has internal drain timeout (~30s)

## Configuration Patterns

### Basic Publisher

```go
server, err := nats_io.NewServer(
    nats_io.WithServer("nats://nats.example.com:4222", "position-publisher"),
    nats_io.WithConnections(false, true),  // Outgoing only
)

err = server.Publish("positions", data)
```

### Basic Subscriber

```go
server, err := nats_io.NewServer(
    nats_io.WithServer("nats://nats.example.com:4222", "position-consumer"),
    nats_io.WithConnections(true, false),  // Incoming only
)

ch, err := server.Subscribe("positions")
for msg := range ch {
    process(msg.Data)
}
```

### Request-Reply Client

```go
server, err := nats_io.NewServer(
    nats_io.WithServer("nats://nats.example.com:4222", "api-client"),
    nats_io.WithConnections(false, true),  // Outgoing for requests
)

response, err := server.Request("api.query", queryData, nil, time.Second)
```

### Request-Reply Server

```go
server, err := nats_io.NewServer(
    nats_io.WithServer("nats://nats.example.com:4222", "api-server"),
    nats_io.WithConnections(false, true),  // Outgoing for replies
)

handler := func(msg *nats.Msg) {
    result := processQuery(msg.Data)
    msg.Respond(result)
}

sub, err := server.SubscribeReply("api.query", "handlers", handler)
```

## Production Lessons

<!--
Maintainers: Add your NATS experiences:
- Issue encountered:
- Root cause:
- Solution:
- Lessons learned:
-->

### Slow Consumer Debugging

**Scenario**: Frequent `ErrSlowConsumer` errors

**Investigation**:
1. Check channel utilization in health logs
   ```
   Channel Check % =95+ → Near capacity
   ```

2. Profile message handler
   ```go
   go tool pprof http://localhost:8080/debug/pprof/profile
   ```

3. Measure processing time
   ```go
   start := time.Now()
   processMessage(msg)
   duration := time.Since(start)
   if duration > time.Millisecond*100 {
       log.Warn().Dur("duration", duration).Msg("Slow handler")
   }
   ```

**Common causes**:
- Database queries in handler (blocking)
- JSON encoding/decoding overhead
- Mutex contention
- Insufficient workers

**Solutions**:
1. **Increase workers**: More goroutines processing channel
2. **Increase buffer**: Higher QueueDepth (temporary fix)
3. **Optimize handler**: Reduce per-message processing time
4. **Queue groups**: Distribute load across instances

### Connection Flapping

**Scenario**: Connections repeatedly disconnect/reconnect

**Logs show**:
```
Unable to connect to NATS server
Connected
Unable to connect to NATS server
Connected
...
```

**Causes**:
1. **Network instability**: Packet loss, routing issues
2. **NATS server overload**: Too many connections, CPU saturated
3. **Firewall timeouts**: Idle connection killed
4. **DNS issues**: Server hostname resolution failing intermittently

**Solutions**:
1. **Enable NATS ping/pong**: Keep connection alive
   ```go
   nats.Connect(url, nats.PingInterval(time.Second*20))
   ```

2. **Increase max reconnects**: Default is unlimited
   ```go
   nats.Connect(url, nats.MaxReconnects(100))
   ```

3. **Monitor NATS server**: Check CPU, memory, connections

4. **Network diagnostics**: ping, traceroute, check packet loss

### Message Ordering

**Assumption**: NATS preserves message order

**Reality**: Usually yes, but not guaranteed

**When order breaks**:
- Reconnection (buffer drain + replay)
- Multiple publishers (interleaved)
- Cluster mode (different routes)

**If order matters**:
1. **Sequence numbers**: Add incrementing ID to messages
2. **Timestamps**: Allow reordering on consumer
3. **JetStream**: Guarantees order (with overhead)

**Example**:
```go
type PositionUpdate struct {
    Sequence  uint64
    Timestamp time.Time
    Position  Position
}
```

### Request Timeout Tuning

**Too short** (<100ms):
- Timeout during normal processing
- Unnecessary retries
- Load on server

**Too long** (>30s):
- Slow failure detection
- Blocks caller unnecessarily
- Resource waste

**Recommendation**: Measure P99 latency, set timeout = 2× P99

**Example**:
```
P99 latency: 500ms
Timeout: 1000ms (2× P99)
```

**Monitoring**:
```promql
# Track request duration
histogram_quantile(0.99, rate(nats_request_duration_seconds_bucket[5m]))
```

## Common Issues

### "outgoing nats connection not set up"

**Error**: `Publish()` or `Request()` fails

**Cause**: Configured for incoming only
```go
WithConnections(true, false)  // No outgoing
```

**Solution**: Enable outgoing
```go
WithConnections(true, true)   // Both
// or
WithConnections(false, true)  // Outgoing only
```

### "incoming nats connection not set up"

**Error**: `Subscribe()` fails

**Cause**: Configured for outgoing only
```go
WithConnections(false, true)  // No incoming
```

**Solution**: Enable incoming
```go
WithConnections(true, true)   // Both
```

### Messages Not Received

**Symptom**: Subscribe succeeds, but channel empty

**Checks**:

1. **Subject match**: Exact match required
   ```go
   Publish("aircraft.positions")   // Not received by:
   Subscribe("aircraft.position")  // Typo: missing 's'
   ```

2. **Connection status**: Check health check
   ```
   Is incoming connected?
   ```

3. **Publisher exists**: Someone must publish
   ```bash
   # Test with nats CLI
   nats pub aircraft.positions "test"
   ```

4. **Queue group**: Check if using queue subscription
   ```go
   SubscribeQueueGroup("topic", "group1")  // Only group1 members receive
   ```

5. **Permissions**: NATS ACLs might block subject

### High Memory Usage

**Symptom**: Memory grows with NATS activity

**Cause**: Channel buffers + message payloads

**Calculation**:
```
Memory = Channels × QueueDepth × AvgMessageSize

Example:
  10 channels × 2048 depth × 1KB = ~20 MB
  100 channels × 2048 depth × 10KB = ~2 GB (!!)
```

**Solutions**:
1. **Reduce QueueDepth**: Trade-off with drop risk
2. **Reduce channels**: Consolidate subscriptions
3. **Smaller messages**: Compress, reduce fields

**Monitor**:
```promql
# Memory by channel
sum(nats_channel_bytes) by (subject)
```

## Future Enhancements

<!--
Maintainers: Document enhancements as implemented
-->

### Metrics Integration

**Current**: Only `droppedMessageCounter`

**Proposed**: Comprehensive metrics
```go
type Metrics struct {
    PublishCount    prometheus.Counter
    PublishErrors   prometheus.Counter
    SubscribeCount  prometheus.Gauge
    MessageReceived prometheus.Counter
    RequestLatency  prometheus.Histogram
}
```

### Configurable Queue Depth

**Current**: Package constant

**Proposed**: Per-subscription configuration
```go
server.SubscribeWithDepth("high-rate.topic", 8192)  // Large buffer
server.SubscribeWithDepth("low-rate.topic", 256)    // Small buffer
```

### Automatic Reconnection Handling

**Current**: NATS client handles transparently

**Proposed**: Expose reconnect events
```go
server.OnReconnect(func() {
    log.Info().Msg("NATS reconnected, resynchronizing state")
    resyncState()
})
```

## File Guide

| File | Purpose |
|------|---------|
| `nats.go` | NATS client wrapper, pub/sub, request/reply, health checks |

## See Also

- [Sink](../sink/README.md) - Uses NATS for event publishing
- [Middleware](../middleware/README.md) - Accounting uses NATS for updates
- [NATS documentation](https://docs.nats.io/)

## References

- NATS Go client: https://github.com/nats-io/nats.go
- NATS pub/sub: https://docs.nats.io/nats-concepts/core-nats/pubsub
- NATS request/reply: https://docs.nats.io/nats-concepts/core-nats/reqreply
- NATS queue groups: https://docs.nats.io/nats-concepts/core-nats/queue
- JetStream (persistence): https://docs.nats.io/nats-concepts/jetstream
