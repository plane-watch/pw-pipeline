package producer

import (
	"bufio"
	"bytes"
	"time"

	"plane.watch/lib/tracker/beast"
)

const tokenBufSize = 1000
const maxBeastMessageLen = 23

func (p *Producer) beastScanner(scan *bufio.Scanner) error {
	lastTimeStamp := time.Duration(0)
	// make our best lib allocate out of a sync.Pool
	beast.UsePoolAllocator = true
	p.log.Debug().Msg("entering scan.Scan() loop")
	for scan.Scan() && scan.Err() == nil {
		frame, err := beast.NewFrame(scan.Bytes(), p.isRadarCape)
		if nil != err {
			continue
		}

		if p.beastDelay {
			currentTs := frame.BeastTicksNs()
			if lastTimeStamp > 0 && lastTimeStamp < currentTs {
				time.Sleep(currentTs - lastTimeStamp)
			}
			lastTimeStamp = currentTs
		}
		p.addFrame(frame, &p.FrameSource)

		if nil != p.stats.beast {
			p.stats.beast.Inc()
		}
	}
	p.log.Debug().Msg("exited scan.Scan() loop")
	return scan.Err()
}

// ScanBeast is a splitter for BEAST format messages
func ScanBeast(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	// skip until we get our first 0x1A (message start)
	i := bytes.IndexByte(data, 0x1A)
	if i == -1 || len(data) < i+11 {
		// we do not even have the smallest message, let's get some more data
		return 0, nil, nil
	}
	// byte 2 is our message type, so it tells us how long this message is
	msgLen := 0
	switch data[i+1] {
	case 0x31:
		// mode-ac 11 bytes (2+8)
		// 1(esc), 1(type), 6(mlat), 1(signal), 2(mode-ac)
		msgLen = 11
	case 0x32:
		// mode-s short 16 bytes
		// 1(esc), 1(type), 6(mlat), 1(signal), 7(mode-s short)
		msgLen = 16
	case 0x33:
		// mode-s long 23 bytes
		// 1(esc), 1(type), 6(mlat), 1(signal), 14(mode-s extended squitter)
		msgLen = 23
	case 0x34:
		// Config Settings and Stats
		// 1(esc), 1(type), 6(mlat), 1(unused), (1)DIP Config, (1)timestamp error ticks
		msgLen = 11
	case 0x1A:
		// found an escaped 0x1A, skip that too
		return i + 2, nil, nil

	default:
		// unknown? assume we got an out of sequence and skip
		return i + 1, nil, nil
	}

	// Fast path: if the full frame is present and there are no escaped bytes
	// inside it, return a direct slice into scanner buffer (no copy).
	if len(data) >= i+msgLen {
		frame := data[i : i+msgLen]
		if bytes.IndexByte(frame[1:], 0x1A) == -1 {
			return i + msgLen, frame, nil
		}
	}

	// Slow path: copy and unescape doubled 0x1A bytes.
	// If we run out of input, ask scanner for more data.
	token := [maxBeastMessageLen]byte{}
	dataIndex := i
	tokenIndex := 0
	bufferAdvance := i
	for tokenIndex < msgLen {
		if dataIndex >= len(data) {
			return 0, nil, nil
		}

		b := data[dataIndex]
		token[tokenIndex] = b
		tokenIndex++
		dataIndex++
		bufferAdvance++

		// The initial 0x1A is frame start. Any later 0x1A in the encoded stream
		// must be doubled.
		if b == 0x1A && tokenIndex > 1 {
			if dataIndex >= len(data) {
				return 0, nil, nil
			}
			if data[dataIndex] != 0x1A {
				// malformed escape sequence - skip this marker and continue
				return i + 1, nil, nil
			}
			dataIndex++
			bufferAdvance++
		}
	}
	return bufferAdvance, token[:msgLen], nil
}
