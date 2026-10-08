// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT
// Adapted from github.com/pion/webrtc/v4 v4.2.18 pkg/media/oggwriter.
// Only the existing single-track mono stream with default headers is retained.
// RTP transport metadata is discarded by upstream and is not an input here.
// See LICENSE.pion-ogg and THIRD_PARTY_NOTICES.md.
package recording

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
)

const (
	pageHeaderTypeContinuationOfStream = 0x00
	pageHeaderTypeContinuationOfPacket = 0x01
	pageHeaderTypeBeginningOfStream    = 0x02
	pageHeaderTypeEndOfStream          = 0x04
	opusGranuleSampleRate              = 48000
	maxOpusPacketSamples               = opusGranuleSampleRate * 120 / 1000
	pageHeaderSignature                = "OggS"
	pageHeaderSize                     = 27
	maxOggPageSegments                 = 255
	noGranulePosition                  = ^uint64(0)
)

var errInvalidOpusPacket = errors.New("invalid Opus packet")

type oggPage struct {
	data, payload []byte
	headerType    uint8
	granulePos    uint64
	pageIndex     uint32
}

// Single owner; oggSegmentWriter serializes access and marks EOS on the final
// page after Close, exactly as with the previous non-seekable upstream writer.
type opusOggWriter struct {
	stream            io.Writer
	serial, pageIndex uint32
	granule           uint64
	checksum          *[256]uint32
}

func newOpusOggWriter(stream io.Writer) (*opusOggWriter, error) {
	if stream == nil {
		return nil, ErrStorage
	}
	var serial [4]byte
	if _, err := rand.Read(serial[:]); err != nil {
		return nil, err
	}
	w := &opusOggWriter{stream: stream, serial: binary.LittleEndian.Uint32(serial[:]), checksum: generateChecksumTable()}
	// Upstream buildIDHeader defaults: mono family0, pre-skip3840,48k,zero gain.
	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8] = 1
	head[9] = 1
	binary.LittleEndian.PutUint16(head[10:], 3840)
	binary.LittleEndian.PutUint32(head[12:], 48000)
	if err := w.writePage(head, pageHeaderTypeBeginningOfStream, 0); err != nil {
		return nil, err
	}
	// Upstream buildCommentHeader defaults: vendor pion, no user comments.
	tags := make([]byte, 20)
	copy(tags, "OpusTags")
	binary.LittleEndian.PutUint32(tags[8:], 4)
	copy(tags[12:], "pion")
	if err := w.writePage(tags, pageHeaderTypeContinuationOfStream, 0); err != nil {
		return nil, err
	}
	return w, nil
}
func (w *opusOggWriter) WriteOpus(payload []byte) error {
	if w.stream == nil {
		return ErrClosed
	}
	samples, err := opusPacketSampleCount(payload)
	if err != nil {
		return err
	}
	w.granule += samples
	return w.writePage(payload, pageHeaderTypeContinuationOfStream, w.granule)
}
func (w *opusOggWriter) writePage(payload []byte, headerType uint8, granule uint64) error {
	pages := createPagesForSerial(w.checksum, payload, headerType, granule, w.serial, w.pageIndex)
	for _, page := range pages {
		n, err := w.stream.Write(page.data)
		if err != nil {
			return err
		}
		if n != len(page.data) {
			return io.ErrShortWrite
		}
	}
	w.pageIndex += uint32(len(pages))
	return nil
}
func (w *opusOggWriter) Close() error { w.stream = nil; return nil }

func createPagesForSerial(
	checksumTable *[256]uint32,
	payload []byte,
	headerType uint8,
	granulePos uint64,
	serial uint32,
	pageIndex uint32,
) []oggPage {
	pages := []oggPage{}
	payloadOffset := 0
	remainingPayload := len(payload)
	firstPage := true

	for {
		segmentTable := make([]byte, 0, maxOggPageSegments)
		pagePayloadSize := 0
		packetComplete := false
		for len(segmentTable) < maxOggPageSegments {
			if remainingPayload >= 255 {
				segmentTable = append(segmentTable, 255)
				pagePayloadSize += 255
				remainingPayload -= 255

				continue
			}

			segmentTable = append(segmentTable, byte(remainingPayload)) //nolint:gosec // remainingPayload is < 255 here.
			pagePayloadSize += remainingPayload
			remainingPayload = 0
			packetComplete = true

			break
		}

		pagePayload := payload[payloadOffset : payloadOffset+pagePayloadSize]
		pageHeaderType := packetPageHeaderType(headerType, firstPage, packetComplete)
		pageGranulePos := noGranulePosition
		if packetComplete {
			pageGranulePos = granulePos
		}

		pages = append(pages, oggPage{
			data: createPageForSerialWithSegments(
				checksumTable,
				pagePayload,
				segmentTable,
				pageHeaderType,
				pageGranulePos,
				serial,
				pageIndex,
			),
			payload:    pagePayload,
			headerType: pageHeaderType,
			granulePos: pageGranulePos,
			pageIndex:  pageIndex,
		})

		payloadOffset += pagePayloadSize
		pageIndex++
		firstPage = false
		if packetComplete {
			break
		}
	}

	return pages
}

func packetPageHeaderType(headerType uint8, firstPage, packetComplete bool) uint8 {
	if firstPage {
		if packetComplete {
			return headerType
		}

		return headerType &^ pageHeaderTypeEndOfStream
	}

	pageHeaderType := uint8(pageHeaderTypeContinuationOfPacket)
	if packetComplete {
		pageHeaderType |= headerType & pageHeaderTypeEndOfStream
	}

	return pageHeaderType
}

func createPageForSerialWithSegments(
	checksumTable *[256]uint32,
	payload []byte,
	segmentTable []byte,
	headerType uint8,
	granulePos uint64,
	serial uint32,
	pageIndex uint32,
) []byte {
	page := make([]byte, pageHeaderSize+len(segmentTable)+len(payload))

	copy(page[0:], pageHeaderSignature)                 // page headers starts with 'OggS'
	page[4] = 0                                         // Version
	page[5] = headerType                                // 1 = continuation, 2 = beginning of stream, 4 = end of stream
	binary.LittleEndian.PutUint64(page[6:], granulePos) // granule position
	binary.LittleEndian.PutUint32(page[14:], serial)    // Bitstream serial number
	binary.LittleEndian.PutUint32(page[18:], pageIndex) // Page sequence number
	page[26] = uint8(len(segmentTable))                 //nolint:gosec // segmentTable is capped at maxOggPageSegments.

	copy(page[pageHeaderSize:], segmentTable)
	copy(page[pageHeaderSize+len(segmentTable):], payload)

	var checksum uint32
	for index := range page {
		checksum = (checksum << 8) ^ checksumTable[byte(checksum>>24)^page[index]]
	}

	binary.LittleEndian.PutUint32(page[22:], checksum)

	return page
}

func opusPacketSampleCount(payload []byte) (uint64, error) {
	if len(payload) == 0 {
		return 0, errInvalidOpusPacket
	}

	frameCount, err := opusPacketFrameCount(payload)
	if err != nil {
		return 0, err
	}

	sampleCount := uint64(opusSamplesPerFrame(payload[0])) * uint64(frameCount)
	if sampleCount > maxOpusPacketSamples {
		return 0, errInvalidOpusPacket
	}

	return sampleCount, nil
}

func opusPacketFrameCount(payload []byte) (uint8, error) {
	switch payload[0] & 0x03 {
	case 0:
		return 1, nil
	case 1, 2:
		return 2, nil
	case 3:
		if len(payload) < 2 {
			return 0, errInvalidOpusPacket
		}

		frameCount := payload[1] & 0x3f
		if frameCount == 0 {
			return 0, errInvalidOpusPacket
		}

		return frameCount, nil
	default:
		return 0, errInvalidOpusPacket
	}
}

func opusSamplesPerFrame(toc byte) uint32 {
	if toc&0x80 != 0 {
		return (opusGranuleSampleRate << ((toc >> 3) & 0x03)) / 400
	}
	if toc&0x60 == 0x60 {
		if toc&0x08 != 0 {
			return opusGranuleSampleRate / 50
		}

		return opusGranuleSampleRate / 100
	}

	frameSize := (toc >> 3) & 0x03
	if frameSize == 3 {
		return opusGranuleSampleRate * 60 / 1000
	}

	return (opusGranuleSampleRate << frameSize) / 100
}

func generateChecksumTable() *[256]uint32 {
	var table [256]uint32
	const poly = 0x04c11db7

	for i := range table {
		remainder := uint32(i) << 24 //nolint:gosec // G115
		for range 8 {
			if (remainder & 0x80000000) != 0 {
				remainder = (remainder << 1) ^ poly
			} else {
				remainder <<= 1
			}
		}
		table[i] = (remainder & 0xffffffff) //nolint:gosec // no out of bounds access here.
	}

	return &table
}
