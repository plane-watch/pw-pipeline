# ClickHouse Database Client

## Overview

The `clickhouse` package provides a lightweight wrapper around the ClickHouse database client, offering connection management, automatic retries, and structured logging for database operations.

## Why ClickHouse?

### The Use Case

Aircraft tracking generates massive time-series data:
- Position updates: 2-5 Hz per aircraft
- 1000 aircraft = 2,000-5,000 inserts/sec
- 24 hours = 172M-432M rows/day

**Requirements**:
- Fast bulk inserts (batch writes)
- Efficient time-range queries
- Column-oriented storage (most queries touch few columns)
- Good compression (position data has patterns)

### Considered Alternatives

**PostgreSQL**:
- ❌ Row-oriented (poor compression)
- ❌ Slower on bulk inserts
- ❌ Time-range queries less optimized
- ✓ Better for transactional workloads

**TimescaleDB**:
- ✓ PostgreSQL with time-series extensions
- ❌ Still row-oriented underlying
- ❌ More complex setup

**InfluxDB**:
- ✓ Purpose-built for time-series
- ❌ InfluxQL learning curve
- ❌ Less mature ecosystem

**ClickHouse**:
- ✓ Column-oriented (10-100x compression)
- ✓ Insanely fast bulk inserts (millions/sec)
- ✓ SQL-like query language
- ✓ Proven at scale (Cloudflare, Uber, eBay)
- ❌ Not good for updates/deletes
- ❌ Eventual consistency in distributed mode

**ClickHouse wins for**: Append-only time-series analytics

## Connection Management

### URL Format

```
clickhouse://username:password@host:port/database
```

**Example**:
```
clickhouse://default:password@localhost:9000/plane_watch
```

**Defaults**:
- Port: 9000 (native protocol, not HTTP)
- Database: `plane_watch` (if not specified in path)

### Retry on Startup

```go
func New(url string) (*Server, error) {
    for i := 0; i < 5; i++ {
        err := server.Connect()
        if err == nil {
            return server, nil
        }
    }
    return nil, err
}
```

**Why 5 retries**: ClickHouse might be starting in Docker compose

**Why no backoff**: Fast retry loop (~5s total) acceptable at startup

**Trade-off**: Fail fast vs. wait for slow-starting DB

**Production note**: In k8s/compose, use init containers or health checks instead

### Connection Pooling

```go
MaxOpenConns: 100
MaxIdleConns: 50
```

**Why 100 open connections**:
- Handles burst insert traffic
- ClickHouse can handle high connection count
- Each connection is lightweight (native protocol)

**Why 50 idle connections**:
- Keep warm connections for consistent latency
- Balance between memory and connection setup time

**Tuning**:
- **Low traffic** (<100 inserts/sec): 10 open, 5 idle
- **Medium** (100-1000 inserts/sec): 50 open, 25 idle (default would be fine)
- **High** (>1000 inserts/sec): 200 open, 100 idle

## Batch Inserts

### Why Batching?

**Individual inserts** (anti-pattern):
```go
for _, row := range rows {
    INSERT INTO table VALUES (...)  // 1000 network roundtrips
}
```

**Batched inserts**:
```go
INSERT INTO table VALUES
    (row1...),
    (row2...),
    ...
    (row1000...)  // 1 network roundtrip
```

**Performance difference**:
- Individual: ~100-500 inserts/sec
- Batched: ~50,000-500,000+ inserts/sec

**Why ClickHouse loves batches**:
- Amortizes network overhead
- Can compress entire batch together
- Optimizes merge tree insertion

### Batch API

```go
func (chs *Server) Inserts(table string, data []any, max int) error {
    batch, err := chs.conn.PrepareBatch(ctx, "INSERT INTO "+table)

    for i := 0; i < max; i++ {
        err = batch.AppendStruct(data[i])
    }

    return batch.Send()
}
```

**Parameters**:
- `table`: Table name (e.g., "aircraft_positions")
- `data`: Slice of structs matching table schema
- `max`: Number of rows to insert from data slice

**Struct mapping**:
```go
type AircraftPosition struct {
    Timestamp time.Time  `ch:"timestamp"`
    ICAO      uint32     `ch:"icao"`
    Lat       float64    `ch:"latitude"`
    Lon       float64    `ch:"longitude"`
    Altitude  int32      `ch:"altitude"`
}

positions := []any{&AircraftPosition{...}, ...}
err := server.Inserts("aircraft_positions", positions, len(positions))
```

**Why `[]any` instead of `[]AircraftPosition`**:
- Allows different struct types for different tables
- Trade-off: Type safety at call site vs. flexibility

### Batch Sizing

**Too small** (< 100 rows):
- Overhead of network roundtrips
- ClickHouse can't optimize compression
- Lower throughput

**Too large** (> 100k rows):
- Memory spikes (batch held in RAM)
- Long latency (all-or-nothing commit)
- If fails, lose entire batch

**Sweet spot**: 1,000-10,000 rows per batch

**Adaptive strategy**:
```go
const maxBatchSize = 10000
const maxBatchWait = 1 * time.Second

buffer := []AircraftPosition{}
ticker := time.NewTicker(maxBatchWait)

for {
    select {
    case pos := <-positions:
        buffer = append(buffer, pos)
        if len(buffer) >= maxBatchSize {
            flush(buffer)
            buffer = buffer[:0]
        }
    case <-ticker.C:
        if len(buffer) > 0 {
            flush(buffer)
            buffer = buffer[:0]
        }
    }
}
```

**Why time-based flush**: Ensures low-traffic periods still get persisted

## Query Performance Monitoring

### Slow Query Logging

```go
defer func() {
    duration := time.Since(queryStart)
    if duration > 500*time.Millisecond {
        log.Warn().
            Str("Query", query).
            Dur("Time Taken", duration).
            Msg("Slow query detected")
    }
}()
```

**Why 500ms threshold**:
- ClickHouse queries should be fast (<100ms for most)
- 500ms+ suggests:
  - Missing index
  - Full table scan
  - Inefficient query structure
  - Disk I/O bottleneck

**What to do when you see slow queries**:

1. **Check the query plan**:
   ```sql
   EXPLAIN SELECT ... FROM aircraft_positions WHERE ...
   ```

2. **Look for full scans**:
   ```
   → ReadFromMergeTree (aircraft_positions)
     Indexes:
       PrimaryKey (no index used)  ← Problem!
   ```

3. **Add appropriate index**:
   ```sql
   ALTER TABLE aircraft_positions ADD INDEX idx_icao (icao) TYPE minmax
   ```

4. **Use ClickHouse-optimized patterns**:
   - Filter on primary key first
   - Use `PREWHERE` for early filtering
   - Avoid `SELECT *` (column-oriented DB)

### Insert Performance Logging

```go
defer func() {
    log.Debug().
        TimeDiff("Time Taken", time.Now(), startTime).
        Str("table", table).
        Int("Num Rows", max).
        Msg("Insert Batch")
}()
```

**Always logs** (at DEBUG level): Useful for:
- Monitoring batch insert rate
- Detecting insert degradation
- Capacity planning

**Expected timings**:
- 1,000 rows: ~10-50ms
- 10,000 rows: ~50-200ms
- 100,000 rows: ~500-2000ms

**If slower**:
- ClickHouse under load
- Network latency
- Disk I/O saturation
- Complex table schema (many indices)

## Error Handling

### Connection Errors

**Startup failure**:
```go
server, err := New("clickhouse://...")
if err != nil {
    // Failed after 5 retries
    // Cannot proceed without database
    log.Fatal().Err(err).Msg("Cannot connect to ClickHouse")
}
```

**Why fatal**: Pipeline data loss without storage

**Alternative**: Queue in memory, retry periodically (risky - unbounded memory)

### Insert Errors

**Batch insert failure**:
```go
err := server.Inserts(table, data, max)
if err != nil {
    // Entire batch lost
    // Options:
    // 1. Log and drop (data loss)
    // 2. Retry with exponential backoff
    // 3. Write to dead letter queue
}
```

**Common causes**:
- Schema mismatch (struct doesn't match table)
- Constraint violation
- ClickHouse unavailable
- Network partition

**Production pattern**:
```go
for attempt := 1; attempt <= 3; attempt++ {
    err := server.Inserts(table, data, len(data))
    if err == nil {
        break
    }
    if attempt == 3 {
        // Failed 3 times, send to DLQ
        deadLetterQueue <- data
    }
    time.Sleep(time.Second * time.Duration(attempt))
}
```

### Query Errors

**Select failure**:
```go
err := server.Select(ctx, &results, query, args...)
if err != nil {
    // Query failed
    // Usually safe to retry (read-only)
}
```

**ErrNotConnected**: Connection lost
```go
if errors.Is(err, clickhouse.ErrNotConnected) {
    // Reconnect and retry
    server.Connect()
}
```

## ClickHouse Best Practices

### Schema Design

**Primary key choice**: ClickHouse sorts data by primary key

**Good primary keys**:
```sql
-- Time-series: Always partition by time first
PRIMARY KEY (timestamp, icao)

-- Enables fast time-range queries
SELECT * FROM positions WHERE timestamp BETWEEN ... AND ...
```

**Bad primary keys**:
```sql
-- Random UUID: Destroys sort order
PRIMARY KEY (id)

-- High cardinality first: Poor compression
PRIMARY KEY (icao, timestamp)
```

### Table Engine

**MergeTree** (default, recommended):
```sql
ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (timestamp, icao)
```

**Why partition by day**:
- Fast old data deletion (drop partition)
- Parallel query execution
- Manageable partition size

**ReplicatedMergeTree** (HA setups):
```sql
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/positions', '{replica}')
```

### Data Types

**Use smallest type that fits**:
```sql
-- Bad: String for ICAO (24 bytes)
icao String

-- Good: UInt32 (4 bytes)
icao UInt32

-- Bad: Float64 for altitude (8 bytes)
altitude Float64

-- Good: Int16 for altitude in feet (2 bytes, -32k to +32k)
altitude Int16
```

**Why**: Column-oriented = type size × row count for each column

### Compression

**ClickHouse compresses automatically** but help it:

**Sorted columns compress better**:
```sql
ORDER BY (timestamp, icao, altitude)
-- timestamp: monotonic increasing = 90%+ compression
-- icao: repeated values grouped = 80%+ compression
-- altitude: changes gradually = 70%+ compression
```

**Random data compresses poorly**:
```sql
-- UUIDs: ~0-10% compression (random)
-- Hashes: ~0% compression (random)
```

**Use codecs for known patterns**:
```sql
altitude Int16 CODEC(DoubleDelta, LZ4)
-- DoubleDelta: Great for gradually changing values
-- LZ4: Fast general-purpose compression
```

## Health Checks

**Connection state**:
```go
func (chs *Server) HealthCheck() bool {
    return chs.connected && chs.conn.Ping(context.Background()) == nil
}
```

**Production health check**:
```go
func DetailedHealthCheck(server *clickhouse.Server) error {
    // 1. Connection alive?
    if err := server.conn.Ping(ctx); err != nil {
        return fmt.Errorf("ping failed: %w", err)
    }

    // 2. Can write?
    testRow := []any{&TestStruct{...}}
    if err := server.Inserts("health_check", testRow, 1); err != nil {
        return fmt.Errorf("insert failed: %w", err)
    }

    // 3. Can read?
    var result []TestStruct
    if err := server.Select(ctx, &result, "SELECT * FROM health_check LIMIT 1"); err != nil {
        return fmt.Errorf("select failed: %w", err)
    }

    return nil
}
```

## Common Issues

### Schema Mismatch

**Symptom**: Insert fails with "type mismatch" error

**Cause**:
```go
type Position struct {
    Altitude int64  `ch:"altitude"`  // Struct says int64
}

// Table schema:
CREATE TABLE positions (altitude Int32)  // Table says Int32
```

**Solution**: Match struct types to table types exactly

### Connection Pool Exhaustion

**Symptom**: Inserts block, errors about "too many connections"

**Cause**: All 100 connections in use

**Solutions**:
1. Increase `MaxOpenConns`
2. Faster batch processing (clear connections quicker)
3. Check for connection leaks (unclosed resources)

### Slow Inserts

**Symptom**: Batch inserts taking seconds instead of milliseconds

**Causes**:
1. **Too many indices**: Each index = write overhead
   ```sql
   -- Excessive
   INDEX idx1, INDEX idx2, INDEX idx3, ...
   ```

2. **Synchronous inserts**: Waiting for replication
   ```sql
   SET insert_quorum = 2;  -- Waits for 2 replicas
   ```

3. **Disk I/O saturation**: Check `iostat`, add SSDs

4. **ClickHouse under-resourced**: Check CPU, RAM

### Memory Spikes

**Symptom**: OOM kills during large batch inserts

**Cause**: Entire batch held in RAM before sending

**Solution**:
```go
// Instead of one huge batch
server.Inserts(table, millionRows, 1000000)

// Split into smaller batches
for i := 0; i < len(millionRows); i += 10000 {
    end := min(i+10000, len(millionRows))
    server.Inserts(table, millionRows[i:end], end-i)
}
```

## Production Lessons

> **Note to maintainers**: Add your ClickHouse war stories here

### Typical Insert Rates

**Single pipeline instance**:
- 100 aircraft: ~500-1000 inserts/sec
- 1000 aircraft: ~5k-10k inserts/sec

**Batching patterns**:
- Small deployments: 1-second batches
- Large deployments: 10-second batches, 10k row limit

<!--
Maintainers: Add your observed rates:
- Aircraft count:
- Batch size:
- Insert rate:
- ClickHouse latency:
-->

### When ClickHouse Struggles

**Excessive indices**: Saw 10x slowdown with 20+ indices on single table

**Insufficient RAM**: ClickHouse caches aggressively, <4GB = slow

**HDD instead of SSD**: 100x slower inserts

<!--
Maintainers: Document performance issues you've encountered:
- Symptom:
- Root cause:
- Solution:
- Performance improvement:
-->

## Configuration Examples

### Local Development

```go
server, err := clickhouse.New("clickhouse://default:@localhost:9000/dev_db")
```

### Production

```go
// With authentication
server, err := clickhouse.New("clickhouse://analytics_user:secret@clickhouse.prod:9000/plane_watch")

// With replica set (load balanced)
server, err := clickhouse.New("clickhouse://user:pass@clickhouse-1.prod:9000,clickhouse-2.prod:9000/plane_watch")
```

## File Guide

| File | Purpose |
|------|---------|
| `clickhouse.go` | Connection management, insert/query wrappers |

## See Also

- [Sink](../sink/README.md) - Event publishing before storage
- ClickHouse documentation: https://clickhouse.com/docs/
- ClickHouse Go driver: https://github.com/ClickHouse/clickhouse-go

## References

- MergeTree table engine: https://clickhouse.com/docs/en/engines/table-engines/mergetree-family/mergetree
- Compression codecs: https://clickhouse.com/docs/en/sql-reference/statements/create/table#column-compression-codecs
- Query optimization: https://clickhouse.com/docs/en/guides/improving-query-performance/
