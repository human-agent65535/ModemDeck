//go:build cgo

package recording

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
)

func TestProductionWriterPublishesPrivateOggOpusAtomically(t *testing.T) {
	files, err := newFileStore(filepath.Join(t.TempDir(), "recordings"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.close() })
	codecs, err := callmedia.NewProductionOpusFactory()
	if err != nil {
		t.Fatal(err)
	}
	format := callmedia.PCMFormat{
		Encoding:      callmedia.PCMEncodingS16LE,
		SampleRate:    16000,
		Channels:      1,
		FrameDuration: 20 * time.Millisecond,
	}
	writer, relative, err := (productionWriterFactory{}).New(
		files,
		"call-ogg",
		"segment-ogg",
		format,
		codecs,
	)
	if err != nil {
		t.Fatal(err)
	}
	for sequence := uint64(1); sequence <= 3; sequence++ {
		downlink := make([]byte, format.FrameBytes())
		uplink := make([]byte, format.FrameBytes())
		for sample := 0; sample < format.FrameSamples(); sample++ {
			binary.LittleEndian.PutUint16(
				downlink[sample*2:],
				uint16(int16(1000+sample%100)),
			)
			binary.LittleEndian.PutUint16(
				uplink[sample*2:],
				uint16(int16(500-sample%100)),
			)
		}
		if err := writer.Write(callmedia.DuplexFrame{
			Sequence:    sequence,
			DownlinkPCM: downlink,
			UplinkPCM:   uplink,
		}); err != nil {
			t.Fatal(err)
		}
	}
	duration, size, err := writer.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if duration != 60*time.Millisecond || size <= 0 {
		t.Fatalf("duration = %s, size = %d", duration, size)
	}
	final := filepath.Join(files.root, filepath.FromSlash(relative))
	info, err := os.Stat(final)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("recording mode = %o, want 600", info.Mode().Perm())
	}
	payload, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(payload, []byte("OggS")) ||
		!bytes.Contains(payload, []byte("OpusHead")) ||
		!bytes.Contains(payload, []byte("OpusTags")) {
		t.Fatal("published file is not an Ogg Opus stream")
	}
	lastPage := lastOggPage(t, payload)
	if lastPage[5]&0x04 == 0 {
		t.Fatal("last Ogg page is not marked end-of-stream")
	}
	wantChecksum := binary.LittleEndian.Uint32(lastPage[22:26])
	checksumInput := append([]byte(nil), lastPage...)
	clear(checksumInput[22:26])
	if got := oggChecksum(checksumInput); got != wantChecksum {
		t.Fatalf("last Ogg page checksum = %08x, want %08x", wantChecksum, got)
	}
	temporary := filepath.Join(files.root, "call-ogg", ".segment-ogg.ogg.part")
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatalf("temporary recording remains: %v", err)
	}
}

func TestProductionWriterRejectsEmptyRecording(t *testing.T) {
	files, err := newFileStore(filepath.Join(t.TempDir(), "recordings"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.close() })
	codecs, err := callmedia.NewProductionOpusFactory()
	if err != nil {
		t.Fatal(err)
	}
	format := callmedia.PCMFormat{
		Encoding:      callmedia.PCMEncodingS16LE,
		SampleRate:    16000,
		Channels:      1,
		FrameDuration: 20 * time.Millisecond,
	}
	writer, relative, err := (productionWriterFactory{}).New(
		files,
		"call-empty",
		"segment-empty",
		format,
		codecs,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := writer.Finalize(); !errors.Is(err, ErrNoAudio) {
		t.Fatalf("Finalize() error = %v, want ErrNoAudio", err)
	}
	if _, err := os.Stat(filepath.Join(files.root, filepath.FromSlash(relative))); !os.IsNotExist(err) {
		t.Fatalf("empty final recording exists: %v", err)
	}
	temporary := filepath.Join(files.root, "call-empty", ".segment-empty.ogg.part")
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatalf("empty partial recording exists: %v", err)
	}
}

func lastOggPage(t *testing.T, payload []byte) []byte {
	t.Helper()
	offset := 0
	var page []byte
	for offset < len(payload) {
		if len(payload)-offset < oggPageHeaderSize ||
			!bytes.Equal(payload[offset:offset+4], []byte("OggS")) {
			t.Fatalf("invalid Ogg page at offset %d", offset)
		}
		segments := int(payload[offset+26])
		lacingEnd := offset + oggPageHeaderSize + segments
		if segments == 0 || lacingEnd > len(payload) {
			t.Fatalf("invalid Ogg lacing at offset %d", offset)
		}
		size := oggPageHeaderSize + segments
		for _, value := range payload[offset+oggPageHeaderSize : lacingEnd] {
			size += int(value)
		}
		if offset+size > len(payload) {
			t.Fatalf("truncated Ogg page at offset %d", offset)
		}
		page = payload[offset : offset+size]
		offset += size
	}
	if len(page) == 0 {
		t.Fatal("Ogg stream contains no pages")
	}
	return page
}
