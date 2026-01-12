package haproxy

import (
	"fmt"
	"strings"
)

func (conn *Conn) SetTable(table, key string, counters map[string]uint64) error {
	cmd := fmt.Sprintf(cmdSetTable, table, key)
	for k, v := range counters {
		cmd = strings.Join([]string{cmd, k, fmt.Sprintf("%d", v)}, " ")
	}
	out, err := conn.doCommand(cmd)
	if err != nil {
		return fmt.Errorf("SetTable failed: %w", err)
	}
	out = strings.TrimSpace(out)
	if out != "" {
		return fmt.Errorf("SetTable failed: %s", out)
	}
	return nil
}
