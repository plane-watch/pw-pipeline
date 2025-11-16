package tracker

import (
	"strings"
	"testing"

	"golang.org/x/exp/slices"
	"plane.watch/lib/producer"
	"plane.watch/lib/tracker"
)

func TestTroublesomeInput(t *testing.T) {

	// use the troublesome test data in lib/producer/testdata/beast-radarcape.beast
	// feed it through the pipeline
	// make sure we have only the planes we expect

	// toggling this disables the mode_s ICAO filter and should make the test fail
	//mode_s.DisableICAOChecking()

	trk := tracker.NewTracker(tracker.WithDecodeWorkerCount(1))

	trk.AddProducer(
		producer.New(
			producer.WithFiles([]string{"producer/testdata/beast-radarcape.beast"}),
			producer.WithType(producer.Beast),
		),
	)

	trk.Wait()

	expectedICAO := []string{
		"7C47B9",
		"7CAE09",
		"8A017C",
		"7C6DDD",
		"7C4A07",
		"7C4329",
		"7C8064",
		"7C7A4D",
		"7CAC31",
		"7C7B80",
		"7CF7C4",
		"7CF7C5",
		"7CF7C7",
		"7CF7C6",
		"7C146F",
	}
	slices.Sort(expectedICAO)
	numPlanes := 0

	var gotIcao []string

	trk.EachPlane(func(p *tracker.Plane) bool {
		numPlanes++
		//t.Logf("plane %s has %d frames", p.IcaoIdentifierStr(), p.MsgCount())
		gotIcao = append(gotIcao, p.IcaoIdentifierStr())
		if !slices.Contains(expectedICAO, p.IcaoIdentifierStr()) {
			t.Errorf("Did not expect ICAO %s to be in the tracker list", p.IcaoIdentifierStr())
		}
		return true
	})

	if numPlanes == 0 {
		t.Error("Did not process any planes, does the input exist?")
	}
	if numPlanes != len(expectedICAO) {
		slices.Sort(gotIcao)

		t.Error("Unexpected planes in the tracking area...")
		t.Errorf(" Expected: %s", strings.Join(expectedICAO, ","))
		t.Errorf(" Have: %s", strings.Join(gotIcao, ","))
	}
	trk.Stop()
}
