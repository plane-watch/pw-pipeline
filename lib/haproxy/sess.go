package haproxy

import (
	"fmt"
	"strings"
	"time"
)

type (
	SessionResult struct {
		Proto    string        // The connection type / address family HAProxy is seeing for the client side.
		Source   string        // The source address for the client side of the stream (IP:port, or unix:1 for a unix socket connection).
		Frontend string        // The frontend name handling the stream
		Backend  string        // The backend name selected for the stream, or <NONE> if not applicable.
		Server   string        // The server within the backend that the stream is attached to, or <none>.
		Age      time.Duration // How long the stream has existed (roughly “time since created”).
	}
)

func (hap *HAProxy) ShowSess() ([]SessionResult, error) {

	rawOut, err := hap.Command(cmdShowSess)
	if err != nil {
		return nil, err
	}

	info, err := hap.ShowInfo()
	if err != nil {
		return nil, fmt.Errorf("error parsing HAProxy info: %v", err)
	}

	// allocate output slice with num current sessions +10%
	out := make([]SessionResult, 0, info.CurrStreams+(info.CurrStreams/10))

	// process raw output
	for _, rawLine := range strings.Split(rawOut, "\n") {

		// remove leading & trailing spaces
		rawLine = strings.TrimSpace(rawLine)

		// skip blank lines
		if rawLine == "" {
			continue
		}

		si := SessionResult{}
		for _, rawLinePart := range strings.Split(rawLine, " ") {
			switch {
			case reShowSessProto.MatchString(rawLinePart):
				match := reShowSessProto.FindStringSubmatch(rawLinePart)
				if len(match) < 2 {
					return nil, fmt.Errorf("error parsing session proto: %v", rawLine)
				}
				si.Proto = match[1]
			case reShowSessSrc.MatchString(rawLinePart):
				match := reShowSessSrc.FindStringSubmatch(rawLinePart)
				if len(match) < 2 {
					return nil, fmt.Errorf("error parsing session src: %v", rawLine)
				}
				si.Source = match[1]
			case reShowSessFe.MatchString(rawLinePart):
				match := reShowSessFe.FindStringSubmatch(rawLinePart)
				if len(match) < 2 {
					return nil, fmt.Errorf("error parsing session fe: %v", rawLine)
				}
				si.Frontend = match[1]
			case reShowSessBe.MatchString(rawLinePart):
				match := reShowSessBe.FindStringSubmatch(rawLinePart)
				if len(match) < 2 {
					return nil, fmt.Errorf("error parsing session be: %v", rawLine)
				}
				si.Backend = match[1]
			case reShowSessSrv.MatchString(rawLinePart):
				match := reShowSessSrv.FindStringSubmatch(rawLinePart)
				if len(match) < 2 {
					return nil, fmt.Errorf("error parsing session srv: %v", rawLine)
				}
				si.Server = match[1]
			case reShowSessAge.MatchString(rawLinePart):
				match := reShowSessAge.FindStringSubmatch(rawLinePart)
				if len(match) < 2 {
					return nil, fmt.Errorf("error parsing session age: %v", rawLine)
				}
				si.Age, err = parseHAProxyAge(match[1])
				if err != nil {
					return nil, fmt.Errorf("error parsing time duration from session age: %v", err)
				}
			}
		}
		out = append(out, si)
	}
	return out, nil
}
