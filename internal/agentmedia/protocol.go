package agentmedia

import (
	"bytes"
	"encoding/binary"
	"io"
)

const (
	upgradeProtocol  = "modemdeck-media-v2"
	protocolVersion  = 2
	frameHeaderBytes = 16
	maxControlBytes  = 64
)

type frameType uint8

const (
	framePlayback frameType = 1
	frameCapture  frameType = 2
	frameClose    frameType = 5
	frameStart    frameType = 6
	frameStarted  frameType = 7
	frameError    frameType = 8
)

var frameMagic = [4]byte{'M', 'D', 'M', '2'}

type frame struct {
	Type     frameType
	Sequence uint32
	Payload  []byte
}

func readFrame(reader io.Reader, maxPayload int) (frame, error) {
	var header [frameHeaderBytes]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return frame{}, err
	}
	if !bytes.Equal(header[:4], frameMagic[:]) ||
		header[5] != 0 ||
		header[6] != 0 ||
		header[7] != 0 {
		return frame{}, newError(
			ErrorProtocol,
			"read_media_frame",
			"invalid host media frame header",
			nil,
		)
	}
	payloadLength := binary.BigEndian.Uint32(header[12:16])
	if uint64(payloadLength) > uint64(maxPayload) {
		return frame{}, newError(
			ErrorProtocol,
			"read_media_frame",
			"host media frame exceeds negotiated size",
			nil,
		)
	}
	payload := make([]byte, int(payloadLength))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return frame{}, err
	}
	return frame{
		Type:     frameType(header[4]),
		Sequence: binary.BigEndian.Uint32(header[8:12]),
		Payload:  payload,
	}, nil
}

func writeFrame(writer io.Writer, value frame) error {
	var header [frameHeaderBytes]byte
	copy(header[:4], frameMagic[:])
	header[4] = byte(value.Type)
	binary.BigEndian.PutUint32(header[8:12], value.Sequence)
	binary.BigEndian.PutUint32(header[12:16], uint32(len(value.Payload)))
	if err := writeFull(writer, header[:]); err != nil {
		return err
	}
	return writeFull(writer, value.Payload)
}

func writeFull(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		written, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		payload = payload[written:]
	}
	return nil
}
