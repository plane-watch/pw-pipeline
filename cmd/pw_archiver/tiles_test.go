package main

import (
	"encoding/json"
	"strings"
	"testing"

	"plane.watch/lib/rawarchive"
	"plane.watch/lib/tile_grid"
)

// The pre-calculated grid is filled by iterating a Go map, whose order changes from
// run to run, so LookupTile is only stable while no two tiles claim the same cell.
// This pins that, and that the cell map matches the tiles' own bounds test.
func TestTileCellsUnambiguous(t *testing.T) {
	names := tile_grid.GridLocationNames()
	cells := tileCells()
	for lat := -90; lat < 90; lat++ {
		for lon := -180; lon < 180; lon++ {
			var owners []string
			for _, name := range names {
				if tile_grid.InGridLocation(float64(lat), float64(lon), name) {
					owners = append(owners, name)
				}
			}
			got := cells[lat+90][lon+180]
			switch len(owners) {
			case 0:
				if got != rawarchive.TileUnknown {
					t.Errorf("cell %d,%d: no tile contains it but LookupTile says %d", lat, lon, got)
				}
			case 1:
				if want := rawarchive.TileNumber(owners[0]); got != want {
					t.Errorf("cell %d,%d: LookupTile %d, bounds say %d", lat, lon, got, want)
				}
			default:
				t.Errorf("cell %d,%d claimed by %v; LookupTile is unstable here", lat, lon, owners)
			}
		}
	}
	if cells[-32+90][115+180] != 35 {
		t.Errorf("Perth's cell is tile %d", cells[-32+90][115+180])
	}
}

func TestRLERow(t *testing.T) {
	var row [360]uint8
	for i := range row {
		row[i] = 255
	}
	for i := 111 + 180; i < 129+180; i++ {
		row[i] = 35
	}
	got := rleRow(row)
	if want := "255x291,35x18,255x51"; got != want {
		t.Errorf("rleRow = %q, want %q", got, want)
	}
}

func TestTilesJSON(t *testing.T) {
	b, err := buildTilesJSON("20261101")
	if err != nil {
		t.Fatal(err)
	}
	var f tilesFile
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Grids) != 1 {
		t.Fatalf("%d grids", len(f.Grids))
	}
	g := f.Grids[0]
	if g.Version != gridVersion || g.FirstDay != "20261101" || g.Source == "" {
		t.Errorf("grid %+v", g)
	}
	if len(g.Tiles) != len(tile_grid.GetGrid()) {
		t.Errorf("%d tiles, grid has %d", len(g.Tiles), len(tile_grid.GetGrid()))
	}
	if len(g.Cells) != 180 {
		t.Errorf("%d cell rows", len(g.Cells))
	}
	var perth *tileInfo
	for i := range g.Tiles {
		if g.Tiles[i].Number == 35 {
			perth = &g.Tiles[i]
		}
		if i > 0 && g.Tiles[i].Number <= g.Tiles[i-1].Number {
			t.Errorf("tiles not sorted at %d", i)
		}
	}
	if perth == nil || perth.Name != "tile35" || perth.Centre != [2]float64{-31.95, 115.86} || perth.Bounds.West != 111 {
		t.Errorf("tile35: %+v", perth)
	}
	// The row for 32°S has tile35 from 111°E to 129°E.
	if row := g.Cells[-32+90]; !strings.Contains(row, "35x18") {
		t.Errorf("row for 32S: %s", row)
	}
	// Every tile with a centre inside its bounds (pole tiles clamped).
	for _, ti := range g.Tiles {
		if ti.Centre[0] > ti.Bounds.North || ti.Centre[0] < ti.Bounds.South || ti.Centre[1] < ti.Bounds.West || ti.Centre[1] > ti.Bounds.East {
			t.Errorf("%s centre %v outside bounds %+v", ti.Name, ti.Centre, ti.Bounds)
		}
	}
}

func TestParseTileList(t *testing.T) {
	got, err := parseTileList(" 35, tile36 ,0")
	if err != nil || len(got) != 3 || !got[35] || !got[36] || !got[0] {
		t.Errorf("got %v %v", got, err)
	}
	if got, err = parseTileList(""); err != nil || got != nil {
		t.Errorf("empty: %v %v", got, err)
	}
	if _, err = parseTileList("35,tile999"); err == nil {
		t.Error("unknown tile accepted")
	}
}
