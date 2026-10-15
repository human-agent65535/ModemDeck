package recording

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The fixture was generated with the unmodified, pinned upstream writer before
// removing its dependency. Only random stream serial and dependent CRC differ.
func TestOggMatchesPinnedUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/ogg_upstream_v4.2.18.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Vectors []struct {
			Name       string   `json:"name"`
			Packets    [][]byte `json:"packets"`
			Normalized []byte   `json:"normalized_ogg"`
			DurationMS uint64   `json:"duration_ms"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Vectors) != 5 {
		t.Fatal("missing fixed upstream cases")
	}
	for _, v := range fixture.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), v.Name+".ogg")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			writer, err := newOpusOggWriter(writerOnly{file: file})
			if err != nil {
				t.Fatal(err)
			}
			var last int64
			for _, packet := range v.Packets {
				last, err = file.Seek(0, 2)
				if err != nil {
					t.Fatal(err)
				}
				if err = writer.WriteOpus(packet); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := markOggPageEndOfStream(file, last); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			normalized := append([]byte(nil), data...)
			var granule uint64
			var serial uint32
			for offset, index := 0, uint32(0); offset < len(data); index++ {
				if len(data)-offset < 27 || string(data[offset:offset+4]) != "OggS" {
					t.Fatal("invalid page")
				}
				n := int(data[offset+26])
				size := 27 + n
				for _, length := range data[offset+27 : offset+27+n] {
					size += int(length)
				}
				page := append([]byte(nil), data[offset:offset+size]...)
				crc := binary.LittleEndian.Uint32(page[22:26])
				clear(page[22:26])
				if oggChecksum(page) != crc {
					t.Fatal("invalid page CRC")
				}
				if index == 0 {
					serial = binary.LittleEndian.Uint32(page[14:18])
				}
				if serial != binary.LittleEndian.Uint32(page[14:18]) || index != binary.LittleEndian.Uint32(page[18:22]) {
					t.Fatal("stream continuity changed")
				}
				granule = binary.LittleEndian.Uint64(page[6:14])
				if offset+size == len(data) && page[5]&4 == 0 {
					t.Fatal("missing EOS")
				}
				clear(normalized[offset+14 : offset+18])
				clear(normalized[offset+22 : offset+26])
				offset += size
			}
			if granule != v.DurationMS*48 {
				t.Fatalf("duration granule=%d want=%d", granule, v.DurationMS*48)
			}
			if !bytes.Equal(normalized, v.Normalized) {
				t.Fatal("headers, lacing, payload, granules or EOS differ from fixed upstream")
			}
			if dir := os.Getenv("MODEMDECK_TEST_OGG_OUTPUT_DIR"); dir != "" && v.Name != "lacing_boundaries" {
				if err := os.WriteFile(filepath.Join(dir, v.Name+".ogg"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestOggInvalidPacketDoesNotAdvanceGranule(t *testing.T) {
	var output bytes.Buffer
	writer, err := newOpusOggWriter(&output)
	if err != nil {
		t.Fatal(err)
	}
	before := output.Len()
	for _, packet := range [][]byte{nil, {0xfb}, {0xfb, 0}, {0xfb, 63}} {
		if err := writer.WriteOpus(packet); err == nil {
			t.Fatalf("accepted %x", packet)
		}
	}
	if output.Len() != before || writer.granule != 0 {
		t.Fatal("invalid payload changed stream")
	}
}
