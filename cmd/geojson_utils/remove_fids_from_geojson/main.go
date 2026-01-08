package main

import (
	"fmt"
	"io"
	"os"

	"github.com/paulmach/orb/geojson"
	"golang.org/x/exp/slices"
	"plane.watch/lib/mlatbridge"
)

func main() {

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to read stdin: %v\n", err)
		os.Exit(1)
	}

	fc, err := geojson.UnmarshalFeatureCollection(data)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to unmarshal feature collection: %v\n", err)
		os.Exit(1)
	}

	for _, r := range mlatbridge.MLATRegionByFID {
		for _, fid := range r {
			for i, f := range fc.Features {
				if f.Properties.MustInt("fid") == fid {
					fc.Features = slices.Delete(fc.Features, i, i+1)
					break
				}
			}
		}
	}

	jb, err := fc.MarshalJSON()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to marshal GeoJSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(jb))
}
