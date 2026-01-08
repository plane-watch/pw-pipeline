package producer

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"plane.watch/lib/tracker"
)

type testFrame struct {
	icao    uint32
	icaoStr string
	ts      time.Time
}

func (t testFrame) Icao() uint32 {
	return t.icao
}

func (t testFrame) IcaoStr() string {
	return t.icaoStr
}

func (t testFrame) Decode() error {
	return nil
}

func (t testFrame) TimeStamp() time.Time {
	return t.ts
}

func (t testFrame) Raw() []byte {
	return []byte{}
}

func TestRepeater(t *testing.T) {

	timeout := time.NewTicker(time.Second)
	successChan := make(chan bool)

	p := &Producer{}
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	p.repeater = newKeepAliveRepeater()
	p.repeater.frequency = time.Millisecond
	p.repeater.duration = time.Millisecond * 50
	p.out = make(chan tracker.FrameEvent)

	count := 0

	go func() {
		for range p.out {
			count++
			// first event is delayed (50-1)
			// last event is not fired (49-1)
			// test variance -1
			if count >= 47 {
				successChan <- true
				break
			}
		}
	}()

	f := testFrame{icao: 0x000001, icaoStr: "000001"}
	fs := tracker.FrameSource{}
	fe := tracker.NewFrameEvent(&f, &fs)

	go p.repeater.processor(p)

	p.repeater.chanFrame <- fe

	select {
	case <-timeout.C:
		t.Errorf("Failed to repeat enough times")
	case <-successChan:
		t.Logf("Successfully repeated %d times", count)
	}

	p.repeater.stop()
}
