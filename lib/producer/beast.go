package producer

import (
	"bufio"
	"bytes"
	"time"

	"plane.watch/lib/tracker/beast"
)

const tokenBufSize = 1000
const tokenBufLen = 50

const (
	// tickResetThreshold is the threshold for detecting a readsb restart.
	// If ticks go backwards by more than this, we assume the device restarted.
	tickResetThreshold = 10 * time.Second

	// maxDrift is the maximum allowed drift between calculated frame time and arrival time.
	// If drift exceeds this, we re-sync the epoch.
	maxDrift = 5 * time.Second
)

// calculateFrameTime calculates the wall-clock time for a Beast frame based on its MLAT ticks.
// This enables proper temporal ordering of frames from different feeders with varying latencies.
func (p *Producer) calculateFrameTime(frame *beast.Frame) time.Time {
	// MLAT-derived positions have magic timestamp - use arrival time
	if frame.IsMlat() {
		return time.Now()
	}

	currentTicks := frame.BeastTicksNs()
	now := time.Now()

	// First frame - establish epoch
	if !p.hasEpoch {
		p.mlatEpoch = currentTicks
		p.wallEpoch = now
		p.lastMlatTicks = currentTicks
		p.hasEpoch = true
		return now
	}

	// Detect tick reset (readsb restart behind feeder client)
	// Large backwards jump indicates device restart
	if currentTicks+tickResetThreshold < p.lastMlatTicks {
		p.log.Info().
			Dur("oldTicks", p.lastMlatTicks).
			Dur("newTicks", currentTicks).
			Msg("MLAT tick reset detected, re-establishing epoch")
		if p.epochResets != nil {
			p.epochResets.Inc()
		}
		p.mlatEpoch = currentTicks
		p.wallEpoch = now
		p.lastMlatTicks = currentTicks
		return now
	}

	// Calculate frame time from epoch
	frameTime := p.wallEpoch.Add(currentTicks - p.mlatEpoch)

	// Sanity check: if calculated time drifts too far from arrival time,
	// re-sync (handles gradual clock drift, network path changes, etc.)
	drift := now.Sub(frameTime)

	// Record drift for observability
	if p.driftGauge != nil {
		p.driftGauge.Set(float64(drift.Microseconds()))
	}

	if drift < -maxDrift || drift > maxDrift {
		p.log.Debug().
			Dur("drift", drift).
			Msg("Epoch drift exceeded threshold, re-syncing")
		if p.driftCorrections != nil {
			p.driftCorrections.Inc()
		}
		p.mlatEpoch = currentTicks
		p.wallEpoch = now
		frameTime = now
	}

	// Track last ticks for reset detection
	// Allow small backwards jitter without updating (network reordering)
	if currentTicks > p.lastMlatTicks {
		p.lastMlatTicks = currentTicks
	}

	return frameTime
}

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

		// Calculate accurate timestamp from MLAT ticks
		frameTime := p.calculateFrameTime(frame)
		frame.SetTimeStamp(frameTime)

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
