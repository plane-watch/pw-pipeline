package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"plane.watch/lib/rawarchive"
	"plane.watch/lib/tile_grid"
)

const (
	// tilesKey is where the tile grid description lives: outside raw/, so the
	// lifecycle rule on partitions never deletes it.
	tilesKey = "meta/tiles.json"
	// gridVersion identifies lib/tile_grid as compiled into this archiver. Bump it,
	// and GridSource, when the tiles change.
	gridVersion uint16 = 1
	gridSource         = "pw-pipeline lib/tile_grid 216c1ce"
)

type (
	tilesFile struct {
		Grids []tileGrid `json:"grids"`
	}
	tileGrid struct {
		Version  uint16     `json:"version"`
		Source   string     `json:"source"`
		FirstDay string     `json:"first_day,omitempty"`
		Tiles    []tileInfo `json:"tiles"`
		// Cells is what the archiver applies: 180 rows from 90°S north, each the 360
		// one-degree cells from 180°W east, run-length encoded "<tile>x<count>,...".
		// A point at (lat, lon) is in the tile of cell [floor(lat)+90][floor(lon)+180].
		Cells []string `json:"cells"`
	}
	tileInfo struct {
		Number uint8      `json:"number"`
		Name   string     `json:"name"`
		Bounds tileBounds `json:"bounds"`
		// Centre is a reference point inside the tile, for readers that need one (the
		// surface-position quadrant). Any point in the tile serves.
		Centre [2]float64 `json:"centre"`
	}
	tileBounds struct {
		South float64 `json:"south"`
		North float64 `json:"north"`
		West  float64 `json:"west"`
		East  float64 `json:"east"`
	}
)

// referencePoints are better reference points than a tile's middle, where known:
// near the tile's feeders.
var referencePoints = map[uint8][2]float64{
	35: {-31.95, 115.86}, // Perth
}

// tileCells is the one-degree cell map as LookupTile answers it.
func tileCells() [180][360]uint8 {
	var cells [180][360]uint8
	for lat := -90; lat < 90; lat++ {
		for lon := -180; lon < 180; lon++ {
			cells[lat+90][lon+180] = rawarchive.TileNumber(tile_grid.LookupTile(float64(lat), float64(lon)))
		}
	}
	return cells
}

func rleRow(row [360]uint8) string {
	var b strings.Builder
	for i := 0; i < len(row); {
		j := i
		for j < len(row) && row[j] == row[i] {
			j++
		}
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(int(row[i])))
		b.WriteByte('x')
		b.WriteString(strconv.Itoa(j - i))
		i = j
	}
	return b.String()
}

// buildTilesJSON describes the tile grid compiled into this archiver.
func buildTilesJSON(firstDay string) ([]byte, error) {
	grid := tile_grid.GetGrid()
	tiles := make([]tileInfo, 0, len(grid))
	for name, t := range grid {
		n := rawarchive.TileNumber(name)
		if n == rawarchive.TileUnknown {
			return nil, fmt.Errorf("tile %q has no number", name)
		}
		centre, ok := referencePoints[n]
		if !ok {
			lat := (t.North + t.South) / 2
			// A tile that runs to a pole gets a point nearer its populated part.
			lat = max(-70, min(70, lat))
			centre = [2]float64{lat, (t.West + t.East) / 2}
		}
		tiles = append(tiles, tileInfo{
			Number: n,
			Name:   name,
			Bounds: tileBounds{South: t.South, North: t.North, West: t.West, East: t.East},
			Centre: centre,
		})
	}
	sort.Slice(tiles, func(i, j int) bool { return tiles[i].Number < tiles[j].Number })
	cells := tileCells()
	rows := make([]string, len(cells))
	for i := range cells {
		rows[i] = rleRow(cells[i])
	}
	f := tilesFile{Grids: []tileGrid{{
		Version:  gridVersion,
		Source:   gridSource,
		FirstDay: firstDay,
		Tiles:    tiles,
		Cells:    rows,
	}}}
	return json.MarshalIndent(f, "", " ")
}

// parseTileList reads "35,36" or "tile35,tile36" into a set; empty means every tile.
func parseTileList(s string) (map[uint8]bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	out := make(map[uint8]bool)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.HasPrefix(part, "tile") {
			part = "tile" + part
		}
		if _, ok := tile_grid.GetGrid()[part]; !ok {
			return nil, fmt.Errorf("unknown tile %q", part)
		}
		n := rawarchive.TileNumber(part)
		if n == rawarchive.TileUnknown {
			return nil, fmt.Errorf("tile %q has no number", part)
		}
		out[n] = true
	}
	return out, nil
}
