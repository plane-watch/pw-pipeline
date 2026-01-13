package haproxy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
	"unicode"

	"github.com/rs/zerolog"
)

type (
	request struct {
		command   string
		replyChan chan<- response
	}

	response struct {
		out string
		err error
	}

	HAProxy struct {
		network, address string
		ctx              context.Context
		cancel           context.CancelFunc
		wg               *sync.WaitGroup
		conn             net.Conn
		commandChan      chan request
		logger           zerolog.Logger
		scanner          *bufio.Scanner
	}
)

var (
	delim         = []byte{0x0a, 0x3e, 0x20}
	keepAliveTime = time.Second
	backOffTime   = time.Second
)

func New(network string, address string, logger zerolog.Logger) *HAProxy {
	ctx, cancel := context.WithCancel(context.Background())
	hap := &HAProxy{
		network:     network,
		address:     address,
		ctx:         ctx,
		cancel:      cancel,
		wg:          new(sync.WaitGroup),
		commandChan: make(chan request),
		logger:      logger,
	}
	hap.wg.Go(hap.run)
	return hap
}

func (hap *HAProxy) run() {
	var err error

	logger := hap.logger.With().
		Str("network", hap.network).
		Str("address", hap.address).
		Str("source", "run").
		Logger()

	// loop forever
	for {

		// ensure previous connection is closed
		_ = hap.closeConn()

		// connect to haproxy
		hap.conn, err = net.Dial(hap.network, hap.address)
		if err != nil {
			logger.Error().Err(err).Msg("failed to connect to haproxy")
			_ = hap.closeConn()
			time.Sleep(backOffTime)
			continue
		}
		logger.Info().Msg("connected to haproxy")

		// set up scanner
		hap.scanner = bufio.NewScanner(hap.conn)
		hap.scanner.Split(splitUntilPrompt)
		hap.scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		// enter prompt mode
		res := hap.exec(cmdPrompt)
		if res.err != nil {
			logger.Error().Err(res.err).Str("command", cmdPrompt).Msg("failed to execute command")
			_ = hap.closeConn()
			time.Sleep(backOffTime)
			continue
		}

		if keepRunning := hap.sessionLoop(); !keepRunning {
			_ = hap.closeConn()
			return
		}
		time.Sleep(backOffTime)
	}
}

func (hap *HAProxy) sessionLoop() bool {
	logger := hap.logger.With().
		Str("network", hap.network).
		Str("address", hap.address).
		Str("source", "sessionLoop").
		Logger()

	defer func() {
		_ = hap.closeConn()
	}()

	// command loop
	for {
		select {

		// handle context closure
		case <-hap.ctx.Done():
			_ = hap.closeConn()
			return false

		// keep connection open
		case <-time.After(keepAliveTime):
			res := hap.exec(cmdEcho)
			if res.err != nil {
				logger.Error().Err(res.err).Str("command", cmdEcho).Msg("haproxy command error")
				_ = hap.closeConn()
				return true
			}

		// handle commands
		case cmd := <-hap.commandChan:
			res := hap.exec(cmd.command)
			cmd.replyChan <- res
		}
	}
}

func (hap *HAProxy) exec(cmd string) response {

	res := response{}

	// send command
	n, err := hap.conn.Write([]byte(cmd))
	if err != nil {
		res.err = err
	}
	if n != len(cmd) {
		res.err = io.ErrShortWrite
	}

	// if there was a problem sending return the error and finish
	if res.err != nil {
		return res
	}

	// read output

	if !hap.scanner.Scan() {
		// Scan() returned false: either EOF or error
		if err := hap.scanner.Err(); err != nil {
			res.err = err
			return res
		}
		res.err = io.EOF
		return res
	}

	res.out = hap.scanner.Text()

	// return result
	return res
}

func (hap *HAProxy) Close() {
	hap.cancel()
	hap.wg.Wait()
	close(hap.commandChan)
}

func splitUntilPrompt(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// Look for delimiter in the buffered data
	if i := bytes.Index(data, delim); i >= 0 {
		// Return everything up to the delimiter (excluding it)
		return i + len(delim), data[:i], nil
	}

	// If we're at EOF, return what's left (if any)
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}

	// Ask for more data
	return 0, nil, nil
}

func (hap *HAProxy) Command(cmd string) (string, error) {
	outChan := make(chan response)
	command := request{
		command:   cmd,
		replyChan: outChan,
	}
	hap.commandChan <- command
	res := <-outChan
	return res.out, res.err
}

// parseHAProxyAge parses HAProxy "human" durations like:
//
//	16s, 10m, 4h29m, 59m55s, 1d5h, 2d, 0s
//
// Supported units: d, h, m, s
// Format: one or more <number><unit> chunks with no separators.
func parseHAProxyAge(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty age")
	}

	var (
		total time.Duration
		i     int
	)

	for i < len(s) {

		// skip space
		if unicode.IsSpace(rune(s[i])) {
			i++
			continue
		}

		// parse number
		if !unicode.IsDigit(rune(s[i])) {
			return 0, fmt.Errorf("expected digit at pos %d in %q", i, s)
		}
		start := i
		for i < len(s) && unicode.IsDigit(rune(s[i])) {
			i++
		}
		n64, err := strconv.ParseInt(s[start:i], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("bad number %q in %q: %w", s[start:i], s, err)
		}
		if i >= len(s) {
			return 0, fmt.Errorf("missing unit after %q in %q", s[start:i], s)
		}

		// parse unit
		unit := s[i]
		i++

		switch unit {
		case 'd':
			total += time.Duration(n64) * 24 * time.Hour
		case 'h':
			total += time.Duration(n64) * time.Hour
		case 'm':
			total += time.Duration(n64) * time.Minute
		case 's':
			total += time.Duration(n64) * time.Second
		default:
			return 0, fmt.Errorf("unknown unit %q in %q", string(unit), s)
		}
	}

	return total, nil
}

func (hap *HAProxy) closeConn() error {
	if hap.conn != nil {
		return hap.conn.Close()
	}
	return nil
}
