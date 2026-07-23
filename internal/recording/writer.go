package recording

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

const oggPageHeaderSize = 27

type segmentWriter interface {
	Write(callmedia.DuplexFrame) error
	Finalize() (duration time.Duration, size int64, err error)
	Abort() error
}

type segmentWriterFactory interface {
	New(
		*fileStore,
		string,
		string,
		callmedia.PCMFormat,
		callmedia.OpusCodecFactory,
	) (segmentWriter, string, error)
}

type productionWriterFactory struct{}

type writerOnly struct {
	file *os.File
}

func (w writerOnly) Write(payload []byte) (int, error) {
	if w.file == nil {
		return 0, ErrStorage
	}
	written, err := w.file.Write(payload)
	if err == nil && written != len(payload) {
		err = io.ErrShortWrite
	}
	return written, err
}

func (productionWriterFactory) New(
	files *fileStore,
	callID, segmentID string,
	format callmedia.PCMFormat,
	codecs callmedia.OpusCodecFactory,
) (segmentWriter, string, error) {
	if files == nil || codecs == nil {
		return nil, "", ErrInvalidArgument
	}
	segment, err := files.createSegment(callID, segmentID)
	if err != nil {
		return nil, "", err
	}
	ogg, err := oggwriter.NewWith(
		writerOnly{file: segment.file},
		callmedia.RTPClockRate,
		1,
	)
	if err != nil {
		_ = segment.abort()
		return nil, "", fmt.Errorf("create Ogg Opus stream: %w", ErrStorage)
	}
	codec, err := codecs.New(format)
	if err != nil || codec == nil {
		_ = ogg.Close()
		_ = segment.abort()
		return nil, "", fmt.Errorf("create recording Opus codec: %w", errors.Join(ErrCodec, err))
	}
	if codec.Format() != format {
		_ = codec.Close()
		_ = ogg.Close()
		_ = segment.abort()
		return nil, "", fmt.Errorf("recording Opus format mismatch: %w", ErrCodec)
	}
	return &oggSegmentWriter{
		format:    format,
		codec:     codec,
		ogg:       ogg,
		segment:   segment,
		timestamp: 1,
		sequence:  1,
	}, segment.relative, nil
}

type oggSegmentWriter struct {
	format  callmedia.PCMFormat
	codec   callmedia.OpusCodec
	ogg     *oggwriter.OggWriter
	segment *segmentFile

	timestamp      uint32
	sequence       uint16
	lastSequence   uint64
	lastPageOffset int64
	frames         int64

	mu       sync.Mutex
	finished bool
}

func (w *oggSegmentWriter) Write(frame callmedia.DuplexFrame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return ErrClosed
	}
	if len(frame.DownlinkPCM) != w.format.FrameBytes() ||
		len(frame.UplinkPCM) != w.format.FrameBytes() ||
		frame.Sequence == 0 {
		return ErrInvalidArgument
	}
	if w.lastSequence != 0 && frame.Sequence != w.lastSequence+1 {
		return ErrBackpressure
	}
	mixed := mixMonoS16LE(frame.DownlinkPCM, frame.UplinkPCM)
	payload, err := w.codec.Encode(mixed)
	if err != nil {
		return fmt.Errorf("encode recording frame: %w", errors.Join(ErrCodec, err))
	}
	if len(payload) == 0 {
		return fmt.Errorf("encode empty recording frame: %w", ErrCodec)
	}
	offset, err := w.segment.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("locate Ogg recording page: %w", ErrStorage)
	}
	packet := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    callmedia.OpusPayloadType,
			SequenceNumber: w.sequence,
			Timestamp:      w.timestamp,
			SSRC:           1,
		},
		Payload: payload,
	}
	if err := w.ogg.WriteRTP(packet); err != nil {
		return fmt.Errorf("write Ogg Opus frame: %w", ErrStorage)
	}
	w.lastPageOffset = offset
	w.lastSequence = frame.Sequence
	w.frames++
	w.sequence++
	w.timestamp += w.format.RTPFrameSamples()
	return nil
}

func (w *oggSegmentWriter) Finalize() (time.Duration, int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return 0, 0, ErrClosed
	}
	w.finished = true
	if err := w.closeStreams(); err != nil {
		_ = w.segment.abort()
		return 0, 0, err
	}
	if w.frames == 0 {
		_ = w.segment.abort()
		return 0, 0, ErrNoAudio
	}
	if err := markOggPageEndOfStream(w.segment.file, w.lastPageOffset); err != nil {
		_ = w.segment.abort()
		return 0, 0, err
	}
	size, err := w.segment.publish()
	if err != nil {
		return 0, 0, err
	}
	return time.Duration(w.frames) * w.format.FrameDuration, size, nil
}

func (w *oggSegmentWriter) Abort() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.finished {
		w.finished = true
		_ = w.closeStreams()
	}
	return w.segment.abort()
}

func (w *oggSegmentWriter) closeStreams() error {
	var result error
	if w.ogg != nil {
		if err := w.ogg.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close Ogg recording: %w", ErrStorage))
		}
		w.ogg = nil
	}
	if w.codec != nil {
		if err := w.codec.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close recording codec: %w", ErrCodec))
		}
		w.codec = nil
	}
	return result
}

func markOggPageEndOfStream(file *os.File, offset int64) error {
	if file == nil || offset < 0 {
		return ErrStorage
	}
	header := make([]byte, oggPageHeaderSize)
	if _, err := file.ReadAt(header, offset); err != nil {
		return fmt.Errorf("read final Ogg page header: %w", ErrStorage)
	}
	if !bytes.Equal(header[:4], []byte("OggS")) || header[4] != 0 {
		return fmt.Errorf("validate final Ogg page header: %w", ErrStorage)
	}
	segments := int(header[26])
	if segments == 0 {
		return fmt.Errorf("validate final Ogg page lacing: %w", ErrStorage)
	}
	page := make([]byte, oggPageHeaderSize+segments)
	copy(page, header)
	if _, err := file.ReadAt(page[oggPageHeaderSize:], offset+oggPageHeaderSize); err != nil {
		return fmt.Errorf("read final Ogg page lacing: %w", ErrStorage)
	}
	payloadSize := 0
	for _, size := range page[oggPageHeaderSize:] {
		payloadSize += int(size)
	}
	page = append(page, make([]byte, payloadSize)...)
	if _, err := file.ReadAt(
		page[oggPageHeaderSize+segments:],
		offset+int64(oggPageHeaderSize+segments),
	); err != nil {
		return fmt.Errorf("read final Ogg page payload: %w", ErrStorage)
	}
	page[5] |= 0x04
	clear(page[22:26])
	binary.LittleEndian.PutUint32(page[22:26], oggChecksum(page))
	written, err := file.WriteAt(page, offset)
	if err != nil || written != len(page) {
		return fmt.Errorf("write final Ogg page: %w", ErrStorage)
	}
	return nil
}

func oggChecksum(page []byte) uint32 {
	var checksum uint32
	for _, value := range page {
		checksum ^= uint32(value) << 24
		for range 8 {
			if checksum&0x80000000 != 0 {
				checksum = (checksum << 1) ^ 0x04c11db7
			} else {
				checksum <<= 1
			}
		}
	}
	return checksum
}

func mixMonoS16LE(downlink, uplink []byte) []byte {
	mixed := make([]byte, len(downlink))
	for offset := 0; offset+1 < len(downlink); offset += 2 {
		remote := int32(int16(binary.LittleEndian.Uint16(downlink[offset:])))
		local := int32(int16(binary.LittleEndian.Uint16(uplink[offset:])))
		sum := remote + local
		if sum > 32767 {
			sum = 32767
		} else if sum < -32768 {
			sum = -32768
		}
		binary.LittleEndian.PutUint16(mixed[offset:], uint16(int16(sum)))
	}
	return mixed
}
