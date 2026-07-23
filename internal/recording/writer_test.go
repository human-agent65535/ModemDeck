package recording

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestMixMonoS16LESaturatesBothDirections(t *testing.T) {
	downlink := make([]byte, 8)
	uplink := make([]byte, 8)
	values := [][2]int16{
		{20_000, 20_000},
		{-20_000, -20_000},
		{12_000, -2_000},
		{-2_000, 12_000},
	}
	for index, pair := range values {
		binary.LittleEndian.PutUint16(downlink[index*2:], uint16(pair[0]))
		binary.LittleEndian.PutUint16(uplink[index*2:], uint16(pair[1]))
	}
	mixed := mixMonoS16LE(downlink, uplink)
	want := []int16{math.MaxInt16, math.MinInt16, 10_000, 10_000}
	for index, expected := range want {
		got := int16(binary.LittleEndian.Uint16(mixed[index*2:]))
		if got != expected {
			t.Fatalf("mixed sample %d = %d, want %d", index, got, expected)
		}
	}
}
