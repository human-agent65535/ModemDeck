package callmedia

import (
	"errors"
	"testing"
)

func TestValidateOfferRequiresOneFullDuplexOpusAudioSection(t *testing.T) {
	if err := validateOfferSDP(minimalSDP(opusMedia("sendrecv"))); err != nil {
		t.Fatalf("valid Opus offer rejected: %v", err)
	}

	pcmu := "m=audio 9 UDP/TLS/RTP/SAVPF 0\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"a=sendrecv\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n"
	if err := validateOfferSDP(minimalSDP(pcmu)); !errors.Is(err, ErrUnsupportedCodec) {
		t.Fatalf("PCMU-only error = %v", err)
	}
	if err := validateOfferSDP(minimalSDP(opusMedia("sendonly"))); !errors.Is(err, ErrUnsupportedDirection) {
		t.Fatalf("one-way audio error = %v", err)
	}
	conflictingDirection := opusMedia("sendrecv") + "a=recvonly\r\n"
	if err := validateOfferSDP(minimalSDP(conflictingDirection)); !errors.Is(err, ErrInvalidOffer) {
		t.Fatalf("conflicting direction error = %v", err)
	}
	disabled := "m=audio 0 UDP/TLS/RTP/SAVPF 111\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"a=sendrecv\r\n" +
		"a=rtpmap:111 opus/48000/2\r\n"
	if err := validateOfferSDP(minimalSDP(disabled)); !errors.Is(err, ErrUnsupportedDirection) {
		t.Fatalf("disabled audio error = %v", err)
	}

	video := "m=video 9 UDP/TLS/RTP/SAVPF 96\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"a=rtpmap:96 VP8/90000\r\n"
	if err := validateOfferSDP(minimalSDP(opusMedia("sendrecv"), video)); !errors.Is(err, ErrUnsupportedMedia) {
		t.Fatalf("audio plus video error = %v", err)
	}
	if err := validateOfferSDP(minimalSDP(video)); !errors.Is(err, ErrUnsupportedMedia) {
		t.Fatalf("video-only error = %v", err)
	}
}

func TestValidateOfferRejectsInvalidPacketTime(t *testing.T) {
	media := opusMedia("sendrecv") + "a=ptime:15\r\n"
	if err := validateOfferSDP(minimalSDP(media)); !errors.Is(err, ErrInvalidOffer) {
		t.Fatalf("15 ms ptime error = %v", err)
	}
	media = opusMedia("sendrecv") + "a=ptime:60\r\na=maxptime:20\r\n"
	if err := validateOfferSDP(minimalSDP(media)); !errors.Is(err, ErrInvalidOffer) {
		t.Fatalf("ptime over maxptime error = %v", err)
	}
}
