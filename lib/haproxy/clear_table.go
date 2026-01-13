package haproxy

import (
	"fmt"
	"strings"
)

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
