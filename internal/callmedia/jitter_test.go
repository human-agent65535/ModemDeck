package callmedia

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/pion/rtp"
)

func TestJitterReordersPacketsBeforePlayout(t *testing.T) {
	format := testFormat(8000)
	config, err := (JitterConfig{}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	codec := &fakeCodec{format: format}
	jitter := newJitterBuffer(config.PacketCapacity)
	if err := jitter.push(testRTP(101, 1960, 20, 0x22)); err != nil {
		t.Fatal(err)
	}
	if err := jitter.push(testRTP(100, 1000, 20, 0x11)); err != nil {
		t.Fatal(err)
	}
	if !jitter.start() {
		t.Fatal("jitter did not start")
	}
	playout := newRTPPlayout(format, codec, jitter, config)
	first := make([]byte, format.FrameBytes())
	second := make([]byte, format.FrameBytes())
	if err := playout.nextFrame(first); err != nil {
		t.Fatal(err)
	}
	if err := playout.nextFrame(second); err != nil {
		t.Fatal(err)
	}
	if !allBytes(first, 0x11) || !allBytes(second, 0x22) {
		t.Fatal("packets were not played in sequence order")
	}
}

func TestPlayoutSplitsVariableSixtyMillisecondPacket(t *testing.T) {
	format := testFormat(16000)
	config, err := (JitterConfig{}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	codec := &fakeCodec{format: format}
	jitter := newJitterBuffer(config.PacketCapacity)
	if err := jitter.push(testRTP(1, 500, 60, 0x33)); err != nil {
		t.Fatal(err)
	}
	if !jitter.start() {
		t.Fatal("jitter did not start")
	}
	playout := newRTPPlayout(format, codec, jitter, config)
	for index := 0; index < 3; index++ {
		frame := make([]byte, format.FrameBytes())
		if err := playout.nextFrame(frame); err != nil {
			t.Fatal(err)
		}
		if !allBytes(frame, 0x33) {
			t.Fatalf("frame %d did not come from the 60 ms packet", index)
		}
	}
}

func TestPlayoutUsesPLCForTimestampGapAndStopsAtBound(t *testing.T) {
	format := testFormat(8000)
	config, err := (JitterConfig{MaxConcealment: 40 * time.Millisecond}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	codec := &fakeCodec{format: format}
	jitter := newJitterBuffer(config.PacketCapacity)
	if err := jitter.push(testRTP(1, 1000, 20, 0x11)); err != nil {
		t.Fatal(err)
	}
	if err := jitter.push(testRTP(3, 2920, 20, 0x33)); err != nil {
		t.Fatal(err)
	}
	if !jitter.start() {
		t.Fatal("jitter did not start")
	}
	playout := newRTPPlayout(format, codec, jitter, config)
	first := make([]byte, format.FrameBytes())
	concealed := make([]byte, format.FrameBytes())
	third := make([]byte, format.FrameBytes())
	if err := playout.nextFrame(first); err != nil {
		t.Fatal(err)
	}
	if err := playout.nextFrame(concealed); err != nil {
		t.Fatal(err)
	}
	if err := playout.nextFrame(third); err != nil {
		t.Fatal(err)
	}
	if !allBytes(concealed, 0xee) {
		t.Fatal("missing RTP interval was not produced by codec PLC")
	}
	if !allBytes(third, 0x33) {
		t.Fatal("playout did not resume at the next packet")
	}
	if got := codec.concealCalls.Load(); got != 1 {
		t.Fatalf("PLC calls = %d, want 1", got)
	}

	empty := newJitterBuffer(config.PacketCapacity)
	if err := empty.push(testRTP(10, 100, 20, 0x44)); err != nil {
		t.Fatal(err)
	}
	if !empty.start() {
		t.Fatal("empty-tail jitter did not start")
	}
	bounded := newRTPPlayout(format, codec, empty, config)
	frame := make([]byte, format.FrameBytes())
	if err := bounded.nextFrame(frame); err != nil {
		t.Fatal(err)
	}
	if err := bounded.nextFrame(frame); err != nil {
		t.Fatal(err)
	}
	if err := bounded.nextFrame(frame); err != nil {
		t.Fatal(err)
	}
	if err := bounded.nextFrame(frame); !errors.Is(err, ErrMediaGap) {
		t.Fatalf("concealment bound error = %v", err)
	}
}

func TestJitterCapacityAndSequenceWrapAreBounded(t *testing.T) {
	jitter := newJitterBuffer(2)
	if err := jitter.push(testRTP(65535, 0, 20, 1)); err != nil {
		t.Fatal(err)
	}
	if err := jitter.push(testRTP(0, 960, 20, 2)); err != nil {
		t.Fatal(err)
	}
	if err := jitter.push(testRTP(1, 1920, 20, 3)); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("capacity error = %v", err)
	}
	if !jitter.start() {
		t.Fatal("jitter did not start")
	}
	packet, exact, ok := jitter.candidate()
	if !ok || !exact || packet.sequence != 65535 {
		t.Fatalf("first unwrapped packet = %+v exact=%v ok=%v", packet, exact, ok)
	}
	jitter.consume(packet.sequence)
	packet, exact, ok = jitter.candidate()
	if !ok || !exact || packet.sequence != 65536 {
		t.Fatalf("wrapped packet = %+v exact=%v ok=%v", packet, exact, ok)
	}
}

func testRTP(sequence uint16, timestamp uint32, milliseconds byte, value byte) *rtp.Packet {
	return &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			SequenceNumber: sequence,
			Timestamp:      timestamp,
		},
		Payload: []byte{milliseconds, value},
	}
}

func allBytes(value []byte, expected byte) bool {
	return bytes.Count(value, []byte{expected}) == len(value)
}
