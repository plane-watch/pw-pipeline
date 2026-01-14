package haproxy

import (
	"fmt"
	"strings"
)

func (hap *HAProxy) ShutdownSession(id string) error {
	cmd := fmt.Sprintf(cmdShutdownSession, id)
	out, err := hap.Command(cmd)
	if err != nil {
		return err
	}
	var output string
	for _, line := range strings.Split(out, "\n") {
		// remove leading & trailing spaces
		line = strings.TrimSpace(line)

		// skip blank lines
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		output = strings.Join([]string{line}, " ")
		return fmt.Errorf(output)
	}
	return nil
}
