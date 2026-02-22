package haproxy

import (
	"fmt"
	"strconv"
	"strings"
)

type (
	TableResult struct {
		Name string
		Type string
		Size uint64
		Used uint64
	}
)

func (hap *HAProxy) ShowTables() (map[string]TableResult, error) {

	rawOut, err := hap.Command(cmdShowTables)
	if err != nil {
		return nil, err
	}

	out := make(map[string]TableResult)

	// process raw output
	for _, rawLine := range strings.Split(rawOut, "\n") {

		// remove leading & trailing spaces
		rawLine = strings.TrimSpace(rawLine)

		// skip blank lines
		if rawLine == "" {
			continue
		}

		var ti TableResult
		ti, err = getTableInfo(rawLine)
		if err != nil {
			return out, fmt.Errorf("error parsing table info: %v", err)
		}

		// table name as key
		out[ti.Name] = ti
	}

	return out, nil
}

func getTableInfo(rawLine string) (TableResult, error) {
	var err error
	ti := TableResult{}

	// match regex
	m := reShowTables.FindStringSubmatch(rawLine)
	if m == nil {
		return ti, fmt.Errorf("invalid haproxy '%s' line: %q", cmdShowTables, rawLine)
	}

	// table name
	ti.Name = m[1]

	// table type
	ti.Type = m[2]

	// table size
	ti.Size, err = strconv.ParseUint(m[3], 10, 64)
	if err != nil {
		return ti, fmt.Errorf("%s: parse u64: %w", "size", err)
	}

	// table used
	ti.Used, err = strconv.ParseUint(m[4], 10, 64)
	if err != nil {
		return ti, fmt.Errorf("%s: parse u64: %w", "used", err)
	}
	return ti, nil
}

func (hap *HAProxy) ShowTable(table string) (map[string]map[string]uint64, error) {
	var err error

	rawOut, err := hap.Command(fmt.Sprintf(cmdShowTable, table))
	if err != nil {
		return nil, err
	}

	out := make(map[string]map[string]uint64)
	ti := TableResult{}
	numKeys := uint64(0)

	// process raw output
	for i, rawLine := range strings.Split(rawOut, "\n") {

		// remove leading & trailing spaces
		rawLine = strings.TrimSpace(rawLine)

		// skip blank lines
		if rawLine == "" {
			continue
		}

		// handle first line
		if i == 0 {
			ti, err = getTableInfo(rawLine)
			if err != nil {
				return out, fmt.Errorf("error parsing table header: %v", err)
			}
			continue
		}

		m := reShowTableLine.FindStringSubmatch(rawLine)
		if m == nil {
			return out, fmt.Errorf("invalid haproxy '%s' line: %q", cmdShowTables, rawLine)
		}

		key := m[1]
		numKeys++
		rawCounters := m[2]

		counters := make(map[string]uint64)

		for _, rawCounter := range strings.Split(rawCounters, " ") {
			kv := strings.SplitN(rawCounter, "=", 2)
			counters[kv[0]], err = strconv.ParseUint(kv[1], 10, 64)
			if err != nil {
				return out, fmt.Errorf("counter value '%s': parse u32: %w", kv[0], err)
			}
		}

		out[key] = counters
	}

	// sanity check
	if numKeys != ti.Used {
		return out, fmt.Errorf("expected %d table rows, got %d", ti.Used, numKeys)
	}

	return out, nil

}

func (hap *HAProxy) SetTable(table, key string, counters map[string]uint64) error {
	cmd := fmt.Sprintf(cmdSetTable, table, key)
	for k, v := range counters {
		cmd = strings.Join([]string{cmd, k, fmt.Sprintf("%d", v)}, " ")
	}
	out, err := hap.Command(cmd)
	if err != nil {
		return fmt.Errorf("SetTable failed: %w", err)
	}
	out = strings.TrimSpace(out)
	if out != "" {
		return fmt.Errorf("SetTable failed: %s", out)
	}
	return nil
}

func (hap *HAProxy) ClearTableEntry(table, key string) error {
	cmd := fmt.Sprintf(cmdClearTableByKey, table, key)
	out, err := hap.Command(cmd)
	if err != nil {
		return fmt.Errorf("ClearTableEntry failed: %w", err)
	}
	out = strings.TrimSpace(out)
	if out != "" {
		return fmt.Errorf("ClearTableEntry failed: %s", out)
	}
	return nil
}
