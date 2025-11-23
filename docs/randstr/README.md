# Random String Generator

## Overview

The `randstr` package provides a simple utility function for generating random alphanumeric strings. It's used throughout the codebase for creating unique identifiers, queue names, and temporary tokens.

## Why This Package?

### The Need

**Unique identifiers required for**:
- NATS queue names (prevent collisions)
- Temporary session IDs
- Debug identifiers
- Test data generation

**Without helper**:
```go
// Scattered random string generation logic
chars := "abc...XYZ012...789"
b := make([]byte, 20)
for i := range b {
    b[i] = chars[rand.Intn(len(chars))]
}
id := string(b)
```

**With helper**:
```go
id := randstr.RandString(20)
```

## Usage

### Basic Generation

```go
import "plane.watch/lib/randstr"

// Generate 20-character random string
queueName := "pw-ingest-tap-" + randstr.RandString(20)
// Example: "pw-ingest-tap-AbC123xYz456PqRsT789"
```

### Character Set

**Alphanumeric only**:
```
abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789
```

**62 characters total**:
- 26 lowercase letters (a-z)
- 26 uppercase letters (A-Z)
- 10 digits (0-9)

**Why alphanumeric**: Safe for most contexts
- URL safe (no encoding needed)
- Filename safe (no special characters)
- Database safe (no escaping)
- NATS subject safe

**What's NOT included**: Special characters
- No punctuation: `!@#$%^&*()`
- No symbols: `-_=+[]{}|;:,.<>?/`
- Reduces collision probability slightly but improves compatibility

### Randomness

**Uses `math/rand`**: Pseudo-random, not cryptographic

**Seeding**: Relies on global `rand` seed
```go
// Must be seeded somewhere in main()
rand.Seed(time.Now().UnixNano())
```

**Not cryptographically secure**: Don't use for:
- ❌ Security tokens
- ❌ API keys
- ❌ Passwords
- ❌ Session cookies (authentication)

**Safe for**:
- ✓ Queue names
- ✓ Temporary IDs
- ✓ Debug identifiers
- ✓ Non-security test data

## Collision Probability

### How Unique?

**For 20-character strings**:
```
Possible combinations: 62^20 ≈ 7.04 × 10^35
```

**Birthday paradox** (50% collision probability):
```
~10^18 strings needed for 50% collision chance
```

**Practical impact**:
- 1,000 strings: Effectively zero collision risk
- 1,000,000 strings: Still negligible
- 1,000,000,000 strings: Theoretically possible but astronomically unlikely

**Why 20 characters**: Balance
- 10 chars: 62^10 ≈ 8.4 × 10^17 (good for most uses)
- 20 chars: 62^20 ≈ 7.0 × 10^35 (overkill but safe)
- 32 chars: UUID equivalent

### Common Lengths Used

**In codebase**:
```go
// NATS queue names (uniqueness critical)
"pw-ingest-tap-" + randstr.RandString(20)

// Session IDs (temporary, uniqueness helpful)
sessionID := randstr.RandString(16)

// Debug tags (human-readable length)
debugTag := randstr.RandString(8)
```

**Length guidelines**:
- 8 chars: ~2.2 × 10^14 combinations (short-lived, non-critical)
- 16 chars: ~4.8 × 10^28 combinations (typical session IDs)
- 20 chars: ~7.0 × 10^35 combinations (overkill but safe default)
- 32 chars: ~2.3 × 10^57 combinations (UUID-level uniqueness)

## Use Cases in Codebase

### NATS Queue Names

**IngestTap middleware**:
```go
tap.natsQueue = "pw-ingest-tap-" + randstr.RandString(20)
tap.sub, err = tap.natsServer.SubscribeReply(
    NatsAPIv1PwIngestTap,
    tap.natsQueue,  // Unique queue per instance
    tap.requestHandler,
)
```

**Why random**: Multiple pipeline instances
- Each instance needs unique reply queue
- Prevents cross-instance message delivery
- NATS routes replies to correct instance

### Test Data Generation

```go
func TestFrameProcessing(t *testing.T) {
    icao := randstr.RandString(6)  // Random ICAO
    testFrame := createTestFrame(icao)
    // ...
}
```

**Why random**: Avoid test coupling
- Tests don't interfere with each other
- Parallel test execution safe
- Fresh state per test run

### Temporary Identifiers

```go
tempID := "temp-" + randstr.RandString(12)
cache[tempID] = data
// ... use data ...
delete(cache, tempID)
```

**Why random**: Avoid key collisions in temporary storage

## Performance

### Allocation

**Single allocation**:
```go
b := make([]byte, n)  // One allocation for result
```

**No intermediate strings**: Builds directly in byte slice

**Return converts to string**:
```go
return string(b)  // Allocates string, but necessary
```

### Speed

**Benchmarks** (approximate):
```
RandString(8):   ~150 ns/op
RandString(20):  ~300 ns/op
RandString(64):  ~900 ns/op
```

**Linear scaling**: O(n) where n = length

**Bottleneck**: `rand.Intn()` calls, not string building

### Concurrency

**NOT goroutine-safe**: `math/rand` has global lock

**Under contention**: Can slow down
```go
// Many goroutines calling simultaneously
for i := 0; i < 1000; i++ {
    go func() {
        id := randstr.RandString(20)  // Contends on rand.globalSource
    }()
}
```

**Solution for high concurrency**: Use `rand.New()` with separate source per goroutine

<!--
Maintainers: If you need high-concurrency random strings, consider:
- Thread-local rand.Source per goroutine
- Pre-generate pool of IDs
- Use crypto/rand for truly independent random
Document here if implemented
-->

## Security Considerations

### Not Cryptographically Secure

**math/rand is predictable**:
- Deterministic from seed
- Given seed, can predict all outputs
- Attacker can guess future values

**Example vulnerability**:
```go
// DON'T use for tokens!
authToken := randstr.RandString(32)  // INSECURE
// Attacker might predict token if they know seed or see other random values
```

**Use crypto/rand instead**:
```go
import "crypto/rand"
import "encoding/base64"

func SecureToken(n int) (string, error) {
    b := make([]byte, n)
    _, err := rand.Read(b)  // crypto/rand
    if err != nil {
        return "", err
    }
    return base64.URLEncoding.EncodeToString(b), nil
}
```

### Appropriate Uses

**Safe for randomstr.RandString**:
- Unique queue names (no security implication)
- Debug identifiers (visibility, not secrecy)
- Test data (predictability actually helpful)
- Temporary cache keys (not exposed)

**Unsafe for randomstr.RandString**:
- API keys (predictable = hackable)
- Session tokens (authentication bypass risk)
- CSRF tokens (attacker can forge)
- Encryption keys/IVs (catastrophic if predictable)

## Alternatives Considered

### UUID Package

**Standard `github.com/google/uuid`**:
```go
import "github.com/google/uuid"

id := uuid.New().String()
// "f47ac10b-58cc-4372-a567-0e02b2c3d479"
```

**Pros**:
- Standardized format
- Guaranteed uniqueness (version 4)
- Cryptographically random (uses crypto/rand)

**Cons**:
- 36 characters (longer)
- Hyphens (annoying in some contexts)
- Overkill for simple use cases
- External dependency

**Why randstr**: Simpler, no dependency, sufficient for use cases

### Custom Charset

**Could allow custom characters**:
```go
func RandStringCharset(n int, charset string) string {
    b := make([]byte, n)
    for i := range b {
        b[i] = charset[rand.Intn(len(charset))]
    }
    return string(b)
}
```

**Use case**: Specific character restrictions
```go
// Hex only
RandStringCharset(16, "0123456789abcdef")

// Uppercase only
RandStringCharset(8, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
```

**Why not included**: YAGNI (You Aren't Gonna Need It)
- Single charset sufficient for all current uses
- Can add if needed

<!--
Maintainers: If you add charset customization, document here
-->

## Common Patterns

### Prefixed Identifiers

```go
queueName := "myapp-" + randstr.RandString(16)
// "myapp-aB3xY9zPqR5sT8uV"
```

**Why prefix**: Human-readable context
- Know what the ID is for
- Easier debugging
- Filter in logs/metrics

### Suffixed Identifiers

```go
filename := randstr.RandString(12) + ".tmp"
// "aB3xY9zPqR5s.tmp"
```

**Why suffix**: File type or category indication

### Validation Pattern

```go
const expectedLength = 20

func isValidQueueID(id string) bool {
    if len(id) != expectedLength {
        return false
    }
    for _, c := range id {
        if !isAlphanumeric(c) {
            return false
        }
    }
    return true
}
```

**Use case**: Verify ID came from randstr (not user input)

## Testing Implications

### Deterministic Tests

**Problem**: Random IDs make tests non-deterministic
```go
func TestCache(t *testing.T) {
    id := randstr.RandString(10)  // Different every run
    cache.Set(id, "value")
    // ...
}
```

**Solution 1**: Seed rand for tests
```go
func init() {
    rand.Seed(1)  // Fixed seed = same random sequence
}
```

**Solution 2**: Use fixed IDs in tests
```go
func TestCache(t *testing.T) {
    id := "test-id-123"  // Hardcoded, not random
    cache.Set(id, "value")
    // ...
}
```

**Solution 3**: Mock randstr
```go
var randStringFunc = randstr.RandString  // Variable for mocking

func TestCache(t *testing.T) {
    randStringFunc = func(n int) string {
        return "mock-id"
    }
    defer func() { randStringFunc = randstr.RandString }()
    // ...
}
```

## Future Enhancements

<!--
Maintainers: Document enhancements as implemented
-->

### Cryptographically Secure Variant

**Proposed**:
```go
func SecureRandString(n int) (string, error) {
    b := make([]byte, n)
    _, err := crypto_rand.Read(b)
    if err != nil {
        return "", err
    }
    // Convert to alphanumeric
    for i := range b {
        b[i] = letterBytes[int(b[i])%len(letterBytes)]
    }
    return string(b), nil
}
```

**Use case**: When uniqueness MUST be guaranteed

### Performance-Optimized Builder

**Proposed**: Pre-allocate and reuse
```go
var bufferPool = sync.Pool{
    New: func() interface{} {
        return make([]byte, 64)
    },
}

func RandStringFast(n int) string {
    b := bufferPool.Get().([]byte)[:n]
    defer bufferPool.Put(b)

    for i := range b {
        b[i] = letterBytes[rand.Intn(len(letterBytes))]
    }
    return string(b)
}
```

**Benefit**: Reduce allocations in hot paths

## File Guide

| File | Purpose |
|------|---------|
| `randstr.go` | Random alphanumeric string generation |

## See Also

- `crypto/rand` - Cryptographically secure random (for security-sensitive uses)
- `github.com/google/uuid` - UUID generation (standardized format)
- `math/rand` - Pseudo-random number generator (underlying implementation)

## References

- Birthday paradox: https://en.wikipedia.org/wiki/Birthday_problem
- Cryptographically secure randomness: https://pkg.go.dev/crypto/rand
