package callmedia

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"math"
	"net"
	"sync/atomic"
)

func AudioStatsLogger(logger *slog.Logger) func(string, AudioStatistics) {
	return func(id string, stats AudioStatistics) {
		logger.Info("call audio levels", "component", "callmedia", "call_id", id,
			"transport", stats.Transport, "state", stats.State, "failure_code", stats.FailureCode,
			"received_packets", stats.ReceivedPackets, "received_bytes", stats.ReceivedBytes,
			"sent_packets", stats.SentPackets, "dropped_packets", stats.DroppedPackets, "input_dbfs", stats.InputDBFS,
			"input_peak_dbfs", stats.InputPeakDBFS, "output_dbfs", stats.OutputDBFS,
			"neteq_concealed_samples", stats.NetEqConcealedSamples, "neteq_concealment_events", stats.NetEqConcealmentEvents,
			"neteq_inserted_samples", stats.NetEqInsertedSamples, "neteq_removed_samples", stats.NetEqRemovedSamples,
			"neteq_packets_discarded", stats.NetEqPacketsDiscarded, "neteq_target_delay_ms", stats.NetEqTargetDelayMS,
			"neteq_current_delay_ms", stats.NetEqCurrentDelayMS, "neteq_internal_sample_rate", stats.NetEqInternalSampleRate)
	}
}

// AudioStatistics contains counters and levels only, never audio or addresses.
type AudioStatistics struct {
	NetEqConcealedSamples   uint64 `json:"neteq_concealed_samples,omitempty"`
	NetEqConcealmentEvents  uint64 `json:"neteq_concealment_events,omitempty"`
	NetEqInsertedSamples    uint64 `json:"neteq_inserted_samples,omitempty"`
	NetEqRemovedSamples     uint64 `json:"neteq_removed_samples,omitempty"`
	NetEqPacketsDiscarded   uint64 `json:"neteq_packets_discarded,omitempty"`
	NetEqTargetDelayMS      uint64 `json:"neteq_target_delay_ms,omitempty"`
	NetEqCurrentDelayMS     uint64 `json:"neteq_current_delay_ms,omitempty"`
	NetEqInternalSampleRate uint64 `json:"neteq_internal_sample_rate,omitempty"`
	Transport               string `json:"transport,omitempty"`
	State                   string `json:"state,omitempty"`
	FailureCode             string `json:"failure_code,omitempty"`
	ReceivedPackets         uint64 `json:"received_packets"`
	ReceivedBytes           uint64 `json:"received_bytes"`
	DroppedPackets          uint64 `json:"dropped_packets,omitempty"`
	SentPackets             uint64 `json:"sent_packets"`
	InputDBFS               int    `json:"input_dbfs"`
	InputPeakDBFS           int    `json:"input_peak_dbfs"`
	OutputDBFS              int    `json:"output_dbfs"`
}

type audioCounters struct {
	receivedPackets atomic.Uint64
	receivedBytes   atomic.Uint64
	sentPackets     atomic.Uint64
	inputDBFS       atomic.Int64
	inputPeakDBFS   atomic.Int64
	outputDBFS      atomic.Int64
}

func PCMLevels(pcm []byte) (rmsDBFS, peakDBFS int) {
	var sum, peak float64
	for i := 0; i+1 < len(pcm); i += 2 {
		v := math.Abs(float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) / 32768)
		sum += v * v
		peak = math.Max(peak, v)
	}
	rms := float64(0)
	if len(pcm) >= 2 {
		rms = math.Sqrt(sum / float64(len(pcm)/2))
	}
	return levelDBFS(rms), levelDBFS(peak)
}

func levelDBFS(value float64) int {
	if value <= 0 {
		return -96
	}
	return int(math.Max(-96, math.Min(0, math.Round(20*math.Log10(value)))))
}

func (s *Session) Statistics() AudioStatistics {
	stats := AudioStatistics{ReceivedPackets: s.baseStats.ReceivedPackets + s.stats.receivedPackets.Load(), ReceivedBytes: s.baseStats.ReceivedBytes + s.stats.receivedBytes.Load(),
		SentPackets: s.baseStats.SentPackets + s.stats.sentPackets.Load(), DroppedPackets: s.baseStats.DroppedPackets, InputDBFS: int(s.stats.inputDBFS.Load()),
		InputPeakDBFS: int(s.stats.inputPeakDBFS.Load()), OutputDBFS: int(s.stats.outputDBFS.Load())}
	stats.Transport = "websocket"
	if s.socket != nil && s.socket.receiver != nil {
		n := s.socket.receiver.Statistics()
		stats.NetEqConcealedSamples = s.baseStats.NetEqConcealedSamples + n.ConcealedSamples
		stats.NetEqConcealmentEvents = s.baseStats.NetEqConcealmentEvents + n.ConcealmentEvents
		stats.NetEqInsertedSamples = s.baseStats.NetEqInsertedSamples + n.InsertedSamples
		stats.NetEqRemovedSamples = s.baseStats.NetEqRemovedSamples + n.RemovedSamples
		stats.NetEqPacketsDiscarded = s.baseStats.NetEqPacketsDiscarded + n.PacketsDiscarded
		stats.NetEqTargetDelayMS = uint64(n.TargetDelayMS)
		stats.NetEqCurrentDelayMS = uint64(n.CurrentDelayMS)
		stats.NetEqInternalSampleRate = uint64(n.InternalSampleRate)
		stats.DroppedPackets = stats.NetEqPacketsDiscarded
	}
	stats.State = "connecting"
	if s.events != nil {
		select {
		case <-s.events.connected:
			stats.State = "connected"
		default:
		}
	}
	if s.ctx != nil && s.ctx.Err() != nil {
		stats.State = "disconnected"
	}
	if s.Err() != nil {
		stats.FailureCode = AudioFailureCode(s.Err())
	}
	return stats
}

func (c *Core) Statistics(callID string) AudioStatistics {
	c.mu.Lock()
	defer c.mu.Unlock()
	if owner := c.owners[callID]; owner != nil && owner.session != nil {
		return owner.session.Statistics()
	}
	if stats, ok := c.finalStats[callID]; ok {
		return stats
	}
	return AudioStatistics{InputDBFS: -96, InputPeakDBFS: -96, OutputDBFS: -96}
}

// AudioFailureCode keeps diagnostics useful without exposing transport errors,
// addresses, authentication material, or any audio content.
func AudioFailureCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrCanceled) {
		return "cancelled"
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrTransportTimeout) || (errors.As(err, &network) && network.Timeout()) {
		return "transport_timeout"
	}
	switch {
	case errors.Is(err, ErrInvalidAudio):
		return "invalid_audio"
	case errors.Is(err, ErrEndpointIO), errors.Is(err, ErrEndpointUnavailable), errors.Is(err, ErrEndpointNotStarted):
		return "endpoint_failed"
	case errors.Is(err, ErrCodec):
		return "codec_failed"
	case errors.Is(err, ErrBackpressure):
		return "backpressure"
	case errors.Is(err, ErrTransportClosed):
		return "transport_closed"
	default:
		return "media_failed"
	}
}
