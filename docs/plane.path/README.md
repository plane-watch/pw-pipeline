# plane.path - Flight Path Renderer

## Overview

`plane.path` converts recorded ADS-B data files (AVR or SBS1 format) into GeoJSON files showing aircraft flight paths. Useful for visualization, analysis, and generating map overlays.

## Purpose

**Visualization**: Convert raw data to map-friendly format

**Use cases**:
- Visualize aircraft paths on maps (Google Maps, Mapbox, etc.)
- Analyze flight patterns
- Generate documentation graphics
- Research and presentations

## Usage

### AVR Files

```bash
# Output to file
plane.path avr output.geojson input.avr

# Output to stdout
plane.path --stdout avr input.avr > output.geojson

# Compressed input
plane.path avr output.geojson input.avr.gz
plane.path avr output.geojson input.avr.bz2
```

### SBS1 Files

```bash
# SBS1/BaseStation format
plane.path sbs output.geojson input.sbs1

# Also accepts aliases
plane.path sbs1 output.geojson input.sbs1
```

## GeoJSON Output

**Format**: LineString per aircraft

```json
{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "geometry": {
        "type": "LineString",
        "coordinates": [
          [-122.3321, 47.6062, 5000],
          [-122.3200, 47.6100, 5500],
          ...
        ]
      },
      "properties": {
        "icao": "A12345",
        "callsign": "UAL123",
        ...
      }
    }
  ]
}
```

**Coordinates**: `[longitude, latitude, altitude]`

## Options

**--stdout**: Output to stdout instead of file
```bash
plane.path --stdout avr input.avr | jq
```

**--profile**: Generate CPU profile
```bash
plane.path --profile avr output.geojson large-file.avr
# Analyze with: go tool pprof -http=:7777 cpuprofile.pprof
```

## Visualization

### With Mapbox

```javascript
map.addSource('flight-path', {
  type: 'geojson',
  data: 'output.geojson'
});

map.addLayer({
  id: 'path',
  type: 'line',
  source: 'flight-path',
  paint: {
    'line-color': '#ff0000',
    'line-width': 2
  }
});
```

### With Google Maps

```javascript
fetch('output.geojson')
  .then(r => r.json())
  .then(data => {
    map.data.addGeoJson(data);
    map.data.setStyle({
      strokeColor: '#ff0000',
      strokeWeight: 2
    });
  });
```

## See Also

- [recorder](../recorder/README.md) - Capture streams to files
- [tracker](../tracker/README.md) - Aircraft tracking and path history
- GeoJSON spec: https://geojson.org/
