package tracker

const PlaneLocationEventType = "plane-location-event"

type (
	// Event is something that we want to know about. This is the base of our sending of data
	Event interface {
		Type() string
		String() string
	}

	// PlaneLocationEvent is sent whenever a planes information has been updated
	PlaneLocationEvent struct {
		p       *Plane
		new     bool
		removed bool
	}

	// FrameEvent is for whenever we get a frame of data from our producers
	FrameEvent struct {
		frame  Frame
		source *FrameSource
	}

	FrameSource struct {
		RefLat           *float64
		RefLon           *float64
		OriginIdentifier string
		Name             string
		Tag              string
		VelocityCheck    bool
	}
)

func NewPlaneLocationEvent(p *Plane) *PlaneLocationEvent {
	return &PlaneLocationEvent{p: p}
}

func newPlaneActionEvent(p *Plane, isNew, isRemoved bool) *PlaneLocationEvent {
	return &PlaneLocationEvent{p: p, new: isNew, removed: isRemoved}
}

func (p *PlaneLocationEvent) Type() string {
	return PlaneLocationEventType
}
func (p *PlaneLocationEvent) String() string {
	return p.p.String()
}
func (p *PlaneLocationEvent) Plane() *Plane {
	return p.p
}
func (p *PlaneLocationEvent) New() bool {
	return p.new
}
func (p *PlaneLocationEvent) Removed() bool {
	return p.removed
}

func NewFrameEvent(f Frame, s *FrameSource) FrameEvent {
	return FrameEvent{frame: f, source: s}
}

func (f *FrameEvent) Type() string {
	return PlaneLocationEventType
}

func (f *FrameEvent) String() string {
	return f.frame.IcaoStr()
}

func (f *FrameEvent) Frame() Frame {
	return f.frame
}

func (f *FrameEvent) Source() *FrameSource {
	return f.source
}
