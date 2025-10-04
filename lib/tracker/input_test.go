package tracker

import (
	"testing"
	"time"
)

type (
	addRemoveTestProducer struct {
		ch chan FrameEvent
	}
)

func (p *addRemoveTestProducer) HealthCheckName() string { return "test-add-remove-testing" }
func (p *addRemoveTestProducer) HealthCheck() bool       { return true }
func (p *addRemoveTestProducer) String() string          { return "test-add-remove-testing" }
func (p *addRemoveTestProducer) Source() *FrameSource    { return &FrameSource{} }
func (p *addRemoveTestProducer) Stop()                   { close(p.ch) }
func (p *addRemoveTestProducer) Listen() chan FrameEvent {
	p.ch = make(chan FrameEvent)

	return p.ch
}

func TestAddRemoveProducer(t *testing.T) {
	trk := NewTracker(
		WithDecodeWorkerCount(1),
	)

	if len(trk.producers) != 0 {
		t.Error("Expected there to be 0 producers")
	}

	tp := &addRemoveTestProducer{}
	trk.AddProducer(tp)

	if len(trk.producers) != 1 {
		t.Error("Expected there to be 1 producer")
	}
	for tp.ch == nil {
		time.Sleep(1)
	}

	t.Log("here")
	tp.Stop()
	t.Log("and now here")
	time.Sleep(1 * time.Millisecond)

	if len(trk.producers) != 0 {
		t.Error("Expected there to be 0 producers after stopping")
	}
}
