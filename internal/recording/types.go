package recording

import (
	"context"
	"io"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type Repository interface {
	RecordingSettings(context.Context) (store.RecordingSettings, error)
	UpdateRecordingSettings(context.Context, bool, int64) (store.RecordingSettings, error)
	PrepareCallRecordingRequest(context.Context, string, bool) error
	DeleteExpiredCallRecordingRequests(context.Context, time.Time, int) (int64, error)
	EnsureCallRecordingState(context.Context, string) (store.CallRecordingState, error)
	CallRecordingState(context.Context, string) (store.CallRecordingState, error)
	SetCallRecordingEnabled(context.Context, string, bool) (store.CallRecordingState, error)
	CreateRecordingSegment(context.Context, store.RecordingSegment) (store.RecordingSegment, error)
	MarkRecordingSegmentStarted(context.Context, string, string, time.Time) error
	CompleteRecordingSegment(context.Context, string, string, time.Time, int64, int64) error
	FailRecordingSegment(context.Context, string, string, string, time.Time) error
	RecordingSegments(context.Context, string) ([]store.RecordingSegment, error)
	RecordingSegment(context.Context, string, string) (store.RecordingSegment, error)
	InterruptedRecordingSegments(context.Context) ([]store.RecordingSegment, error)
	FailInterruptedRecordingSegments(context.Context) error
	FinalizePendingCallRecording(context.Context, string, string) error
	CallByID(context.Context, string) (store.Call, error)
}

type Media interface {
	SubscribeDuplex(context.Context, callmedia.ActiveCall) (*callmedia.DuplexSubscription, error)
}

type Options struct {
	RootDirectory string
	CodecFactory  callmedia.OpusCodecFactory
	Now           func() time.Time
	Random        io.Reader
	Report        func(error)

	writerFactory segmentWriterFactory
}

type CallRecordings struct {
	State    store.CallRecordingState `json:"state"`
	Segments []store.RecordingSegment `json:"segments"`
}

type Download struct {
	Segment store.RecordingSegment
	File    ReadSeekCloser
}

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}
