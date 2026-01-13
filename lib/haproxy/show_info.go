package haproxy

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type (
	Info struct {

		// Name: Product name
		Name string `haproxy:"Name"`

		// Version: Product version
		Version string `haproxy:"Version"`

		// ReleaseDate: Date of latest source code update
		ReleaseDate time.Time `haproxy:"Release_date"`

		// NbThread: Number of started threads (global.nbthread)
		NbThread uint32 `haproxy:"Nbthread"`

		// NbProc: Number of started run processes (historical, always 1)
		NbProc uint32 `haproxy:"Nbproc"`

		// ProcessNum: Relative run process number (1)
		ProcessNum uint32 `haproxy:"Process_num"`

		// Pid: This run process identifier for the system
		Pid uint32 `haproxy:"Pid"`

		// Uptime: How long ago this run process was started (days+hours+minutes+seconds)
		Uptime time.Duration `haproxy:"Uptime"`

		// UptimeSec: How long ago this run process was started (seconds)
		UptimeSec uint32 `haproxy:"Uptime_sec"`

		// MemMaxMB: Worker process's hard limit on memory usage in MB (-m on command line)
		MemMaxMB uint32 `haproxy:"Memmax_MB"`

		// PoolAllocMB: Amount of memory allocated in pools (in MB)
		PoolAllocMB uint32 `haproxy:"PoolAlloc_MB"`

		// PoolUsedMB: Amount of pool memory currently used (in MB)
		PoolUsedMB uint32 `haproxy:"PoolUsed_MB"`

		// PoolFailed: Number of failed pool allocations since this run was started
		PoolFailed uint32 `haproxy:"PoolFailed"`

		// UlimitN: Hard limit on the number of per-process file descriptors
		UlimitN uint32 `haproxy:"Ulimit-n"`

		// MaxSock: Hard limit on the number of per-process sockets
		MaxSock uint32 `haproxy:"Maxsock"`

		// MaxConn: Hard limit on the number of per-process connections (configured or imposed by Ulimit-n)
		MaxConn uint32 `haproxy:"Maxconn"`

		// HardMaxConn: Hard limit on the number of per-process connections (imposed by Memmax_MB or Ulimit-n)
		HardMaxConn uint32 `haproxy:"Hard_maxconn"`

		// CurrConns: Current number of connections on this run process
		CurrConns uint32 `haproxy:"CurrConns"`

		// CumConns: Total number of connections on this run process since started
		CumConns uint32 `haproxy:"CumConns"`

		// CumReq: Total number of requests on this run process since started
		CumReq uint32 `haproxy:"CumReq"`

		// MaxSslConns: Hard limit on the number of per-process SSL endpoints (front+back), 0=unlimited
		MaxSslConns uint32 `haproxy:"MaxSslConns"`

		// CurrSslConns: Current number of SSL endpoints on this run process (front+back)
		CurrSslConns uint32 `haproxy:"CurrSslConns"`

		// CumSslConns: Total number of SSL endpoints on this run process since started (front+back)
		CumSslConns uint32 `haproxy:"CumSslConns"`

		// MaxPipes: Hard limit on the number of pipes for splicing, 0=unlimited
		MaxPipes uint32 `haproxy:"Maxpipes"`

		// PipesUsed: Current number of pipes in use in this run process
		PipesUsed uint32 `haproxy:"PipesUsed"`

		// PipesFree: Current number of allocated and available pipes in this run process
		PipesFree uint32 `haproxy:"PipesFree"`

		// ConnRate: Number of front connections created on this run process over the last second
		ConnRate uint32 `haproxy:"ConnRate"`

		// ConnRateLimit: Hard limit for ConnRate (global.maxconnrate)
		ConnRateLimit uint32 `haproxy:"ConnRateLimit"`

		// MaxConnRate: Highest ConnRate reached on this run process since started (in connections per second)
		MaxConnRate uint32 `haproxy:"MaxConnRate"`

		// SessRate: Number of sessions created on this run process over the last second
		SessRate uint32 `haproxy:"SessRate"`

		// SessRateLimit: Hard limit for SessRate (global.maxsessrate)
		SessRateLimit uint32 `haproxy:"SessRateLimit"`

		// MaxSessRate: Highest SessRate reached on this run process since started (in sessions per second)
		MaxSessRate uint32 `haproxy:"MaxSessRate"`

		// SslRate: Number of SSL connections created on this run process over the last second
		SslRate uint32 `haproxy:"SslRate"`

		// SslRateLimit: Hard limit for SslRate (global.maxsslrate)
		SslRateLimit uint32 `haproxy:"SslRateLimit"`

		// MaxSslRate: Highest SslRate reached on this run process since started (in connections per second)
		MaxSslRate uint32 `haproxy:"MaxSslRate"`

		// SslFrontendKeyRate: Number of SSL keys created on frontends in this run process over the last second
		SslFrontendKeyRate uint32 `haproxy:"SslFrontendKeyRate"`

		// SslFrontendMaxKeyRate: Highest SslFrontendKeyRate reached on this run process since started (in SSL keys per second)
		SslFrontendMaxKeyRate uint32 `haproxy:"SslFrontendMaxKeyRate"`

		// SslFrontendSessionReusePercent: Percent of frontend SSL connections which did not require a new key
		SslFrontendSessionReusePercent uint32 `haproxy:"SslFrontendSessionReuse_pct"`

		// SslBackendKeyRate: Number of SSL keys created on backends in this run process over the last second
		SslBackendKeyRate uint32 `haproxy:"SslBackendKeyRate"`

		// SslBackendMaxKeyRate: Highest SslBackendKeyRate reached on this run process since started (in SSL keys per second)
		SslBackendMaxKeyRate uint32 `haproxy:"SslBackendMaxKeyRate"`

		// SslCacheLookups: Total number of SSL session ID lookups in the SSL session cache on this run since started
		SslCacheLookups uint32 `haproxy:"SslCacheLookups"`

		// SslCacheMisses: Total number of SSL session ID lookups that didn't find a session in the SSL session cache on this run since started
		SslCacheMisses uint32 `haproxy:"SslCacheMisses"`

		// CompressBpsIn: Number of bytes submitted to the HTTP compressor in this run process over the last second
		CompressBpsIn uint32 `haproxy:"CompressBpsIn"`

		// CompressBpsOut: Number of bytes emitted by the HTTP compressor in this run process over the last second
		CompressBpsOut uint32 `haproxy:"CompressBpsOut"`

		// CompressBpsRateLim: Limit of CompressBpsOut beyond which HTTP compression is automatically disabled
		CompressBpsRateLim uint32 `haproxy:"CompressBpsRateLim"`

		// Tasks: Total number of tasks in the current run process (active + sleeping)
		Tasks uint32 `haproxy:"Tasks"`

		// RunQueue: Total number of active tasks+tasklets in the current run process
		RunQueue uint32 `haproxy:"Run_queue"`

		// IdlePercent: Percentage of last second spent waiting in the current run thread
		IdlePercent uint32 `haproxy:"Idle_pct"`

		// Node: Node name (global.node)
		Node string `haproxy:"node"`

		// Stopping: 1 if the run process is currently stopping, otherwise zero
		Stopping uint32 `haproxy:"Stopping"`

		// Jobs: Current number of active jobs on the current run process (frontend connections, master connections, listeners)
		Jobs uint32 `haproxy:"Jobs"`

		// Unstoppable Jobs: Current number of unstoppable jobs on the current run process (master connections)
		UnstoppableJobs uint32 `haproxy:"Unstoppable Jobs"`

		// Listeners: Current number of active listeners on the current run process
		Listeners uint32 `haproxy:"Listeners"`

		// ActivePeers: Current number of verified active peers connections on the current run process
		ActivePeers uint32 `haproxy:"ActivePeers"`

		// ConnectedPeers: Current number of peers having passed the connection step on the current run process
		ConnectedPeers uint32 `haproxy:"ConnectedPeers"`

		// DroppedLogs: Total number of dropped logs for current run process since started
		DroppedLogs uint32 `haproxy:"DroppedLogs"`

		// BusyPolling: 1 if busy-polling is currently in use on the run process, otherwise zero (config.busy-polling)
		BusyPolling uint32 `haproxy:"BusyPolling"`

		// FailedResolutions: Total number of failed DNS resolutions in current run process since started
		FailedResolutions uint32 `haproxy:"FailedResolutions"`

		// TotalBytesOut: Total number of bytes emitted by current run process since started
		TotalBytesOut uint64 `haproxy:"TotalBytesOut"`

		// TotalSplicedBytesOut: Total number of bytes emitted by current run process through a kernel pipe since started
		TotalSplicedBytesOut uint64 `haproxy:"TotalSplicedBytesOut"`

		// BytesOutRate: Number of bytes emitted by current run process over the last second
		BytesOutRate uint64 `haproxy:"BytesOutRate"`

		// DebugCommandsIssued: Number of debug commands issued on this process (anything > 0 is unsafe)
		DebugCommandsIssued uint32 `haproxy:"DebugCommandsIssued"`

		// CumRecvLogs: Total number of log messages received by log-forwarding listeners on this run process since started
		CumRecvLogs uint32 `haproxy:"CumRecvLogs"`

		// Build info: Build info
		BuildInfo string `haproxy:"Build info"`

		// MemMaxBytes: Worker process's hard limit on memory usage in byes (-m on command line)
		MemMaxBytes uint32 `haproxy:"Memmax_bytes"`

		// PoolAllocBytes: Amount of memory allocated in pools (in bytes)
		PoolAllocBytes uint64 `haproxy:"PoolAlloc_bytes"`

		// PoolUsedBytes: Amount of pool memory currently used (in bytes)
		PoolUsedBytes uint64 `haproxy:"PoolUsed_bytes"`

		// StartTimeSec: Start time in seconds
		StartTimeSec uint32 `haproxy:"Start_time_sec"`

		// Tainted: Experimental features used
		Tainted string `haproxy:"Tainted"`

		// TotalWarnings: Total warnings issued
		TotalWarnings uint32 `haproxy:"TotalWarnings"`

		// MaxConnReached: Number of times an accepted connection resulted in Maxconn being reached
		MaxConnReached uint32 `haproxy:"MaxconnReached"`

		// BootTimeMillis: How long ago it took to parse and process the config before being ready (milliseconds)
		BootTimeMillis uint32 `haproxy:"BootTime_ms"`

		// Niced_tasks: Total number of active tasks+tasklets in the current run process (Run_queue) that are niced
		NicedTasks uint32 `haproxy:"Niced_tasks"`

		// CurrStreams: Current number of streams on this run process
		CurrStreams uint64 `haproxy:"CurrStreams"`

		// CumStreams: Total number of streams created on this run process since started
		CumStreams uint64 `haproxy:"CumStreams"`

		// BlockedTrafficWarnings: Total number of warnings issued about traffic being blocked by too slow a task
		BlockedTrafficWarnings uint32 `haproxy:"BlockedTrafficWarnings"`

		// PatternsAdded: Total number of patterns added (acl/map entries)
		PatternsAdded uint64 `haproxy:"PatternsAdded"`

		// PatternsFreed: Total number of patterns freed (acl/map entries)
		PatternsFreed uint64 `haproxy:"PatternsFreed"`
	}
)

var (

	// reShowInfo is a regular expression (duh) that splits each line of the "show info typed"
	// output into the following capture groups:
	//
	//  - Group 1: the numeric position of the field in the list starting at zero
	//  - Group 2: the field name
	//  - Group 3: the process number starting at 1 (legacy)
	//  - Group 4: three letters that correspond to the field’s origin, nature, and scope of the variable
	//  - Group 5: the field’s type (e.g. str for string and u32 for unsigned 32-bit integer)
	//  - Group 6: the value itself
	//
	// See: https://www.haproxy.com/documentation/haproxy-runtime-api/reference/show-info/#typed-format
	reShowInfo = regexp.MustCompile(`^(\d+)\.([A-Za-z0-9_\- ]+)\.(\d+):(\w+):(\w+):(.*)$`)
)

func (hap *HAProxy) ShowInfo() (Info, error) {
	var out Info

	rawOut, err := hap.Command(cmdShowInfoTyped)
	if err != nil {
		return Info{}, err
	}

	// Type info (tags) + settable value (fields)
	t := reflect.TypeOf(out)
	v := reflect.ValueOf(&out).Elem()

	// Build tag to field index map once
	tagToIndex := make(map[string]int, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if tag := t.Field(i).Tag.Get("haproxy"); tag != "" {
			tagToIndex[tag] = i
		}
	}

	// process raw output
	for _, rawLine := range strings.Split(rawOut, "\n") {

		// remove leading & trailing spaces
		rawLine = strings.TrimSpace(rawLine)

		// skip blank lines
		if rawLine == "" {
			continue
		}

		// match regex
		m := reShowInfo.FindStringSubmatch(rawLine)
		if m == nil {
			return out, fmt.Errorf("invalid haproxy '%s' line: %q", cmdShowInfoTyped, rawLine)
		}

		fieldName := m[2]
		fieldType := m[5]
		fieldValue := m[6]

		idx, ok := tagToIndex[fieldName]
		if !ok {
			continue // struct doesn't care about this field
		}

		fv := v.Field(idx)
		sf := t.Field(idx)

		if !fv.CanSet() {
			return out, fmt.Errorf("field %s cannot be set (is it exported?)", sf.Name)
		}

		// Special cases where HAProxy says "str" but you want a richer Go type
		switch fv.Type() {
		case reflect.TypeOf(time.Time{}):
			// Release_date: 2025/12/19
			tt, err := time.Parse("2006/01/02", fieldValue)
			if err != nil {
				return out, fmt.Errorf("%s: parse date: %w", fieldName, err)
			}
			fv.Set(reflect.ValueOf(tt))
			continue

		case reflect.TypeOf(time.Duration(0)):
			// Uptime: 0d 0h58m52s  (not a standard Go duration)
			d, err := parseHAProxyAge(fieldValue)
			if err != nil {
				return out, fmt.Errorf("%s: parse uptime: %w", fieldName, err)
			}
			fv.SetInt(int64(d))
			continue
		}

		// Otherwise set based on HAProxy's declared type
		switch fieldType {
		case "str":
			if fv.Kind() != reflect.String {
				return out, fmt.Errorf("%s: got str but dest is %s", fieldName, fv.Type())
			}
			fv.SetString(fieldValue)

		case "u32":
			u, err := strconv.ParseUint(fieldValue, 10, 32)
			if err != nil {
				return out, fmt.Errorf("%s: parse u32: %w", fieldName, err)
			}
			if fv.OverflowUint(u) {
				return out, fmt.Errorf("%s: u32 overflows %s", fieldName, fv.Type())
			}
			fv.SetUint(u)

		case "u64":
			u, err := strconv.ParseUint(fieldValue, 10, 64)
			if err != nil {
				return out, fmt.Errorf("%s: parse u64: %w", fieldName, err)
			}
			if fv.OverflowUint(u) {
				return out, fmt.Errorf("%s: u64 overflows %s", fieldName, fv.Type())
			}
			fv.SetUint(u)

		default:
			return out, fmt.Errorf("%s: unknown field type %q", fieldName, fv.Type())
		}
	}

	return out, nil
}
