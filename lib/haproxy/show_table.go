package haproxy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type (
	TableInfo struct {
		Name string
		Type string
		Size uint64
		Used uint64
	}
)

var (

	// reShowTables is a regular expression (duh) that splits each line of the "show table"
	// output into the following capture groups:
	//
	//  - Group 1: the table name
	//  - Group 2: the table type
	//  - Group 3: the table size
	//  - Group 4: the table used
	//
	// See: https://www.haproxy.com/documentation/haproxy-runtime-api/reference/show-table/
	reShowTables = regexp.MustCompile(`^#\s+table:\s+(\w+),\s+type:\s+(\w+),\s+size:(\d+),\s+used:(\d+)$`)

	reShowTableLine = regexp.MustCompile(`^0[xX][[:xdigit:]]+:\s+key=(.*?)\s+(.*)$`)
)

func (hap *HAProxy) ShowTables() (map[string]TableInfo, error) {

	rawOut, err := hap.Command(cmdShowTables)
	if err != nil {
		return nil, err
	}

	out := make(map[string]TableInfo)

	// process raw output
	for _, rawLine := range strings.Split(rawOut, "\n") {

		// remove leading & trailing spaces
		rawLine = strings.TrimSpace(rawLine)

		// skip blank lines
		if rawLine == "" {
			continue
		}

		var ti TableInfo
		ti, err = getTableInfo(rawLine)
		if err != nil {
			return out, fmt.Errorf("error parsing table info: %v", err)
		}

		// table name as key
		out[ti.Name] = ti
	}

	return out, nil
}

func getTableInfo(rawLine string) (TableInfo, error) {
	var err error
	ti := TableInfo{}

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
	ti := TableInfo{}
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
