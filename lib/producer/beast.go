package producer

import (
	"bufio"
	"bytes"
	"time"

	"plane.watch/lib/tracker/beast"
)

const tokenBufSize = 1000
const tokenBufLen = 50

func (p *Producer) beastScanner(scan *bufio.Scanner) error {
	lastTimeStamp := time.Duration(0)
	// make our best lib allocate out of a sync.Pool
	beast.UsePoolAllocator = true
	p.log.Debug().Msg("entering scan.Scan() loop")
	for scan.Scan() && scan.Err() == nil {
		msg := bytes.Clone(scan.Bytes())

		frame, err := beast.NewFrame(msg, p.isRadarCape)
		if nil != err {
			continue
		}

		// Extract MLAT ticks and detect epoch
		// BeastTicksNs() returns nanoseconds for Beast, but raw ticks for RadarCape.
		// ProcessTicks expects nanoseconds, so convert RadarCape if needed.
		mlatTicks := frame.BeastTicksNs()
		if p.isRadarCape {
			// RadarCape returns raw ticks, convert to nanoseconds: ticks * 1000 / 12
			mlatTicks = time.Duration(int64(mlatTicks) * 1000 / 12)
		}
		epochID := p.getEpochDetector(p.Tag).ProcessTicks(mlatTicks)
		frame.SetEpochID(epochID)

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
	bufLen := len(data) - i
	// println("type", data[i+1], "input len", bufLen, "msg len",msgLen)
	if bufLen >= tokenBufLen {
		// we have enough in our buffer
		// account for double escapes
		bufferAdvance := i + msgLen

		token := [tokenBufLen]byte{}

		dataIndex := i // start at the <esc>/0x1a
		tokenIndex := 0
		for tokenIndex < msgLen && dataIndex < i+tokenBufLen {
			token[tokenIndex] = data[dataIndex]

			// if the next byte is an escaped 0x1A, jump it
			if data[dataIndex] == 0x1A && data[dataIndex+1] == 0x1A { // skip over the second <esc>
				bufferAdvance++
				dataIndex++
			}

			dataIndex++
			tokenIndex++
		}
		return bufferAdvance, token[0:msgLen], nil
	}
	// we want more data!
	return 0, nil, nil
}
