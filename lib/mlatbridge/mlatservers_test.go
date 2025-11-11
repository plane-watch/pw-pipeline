package mlatbridge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMLATRegionByFID_NoDupes(t *testing.T) {
	tmp := make(map[int]int, 0)
	for _, fids := range MLATRegionByFID {
		for _, fid := range fids {
			if _, ok := tmp[fid]; !ok {
				tmp[fid] = 0
			}
			tmp[fid]++
		}
	}
	for fid, count := range tmp {
		assert.Lessf(t, count, 2, "fid %d has count of %d", fid, count)
	}
}
