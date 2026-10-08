package callmedia

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"math"
	"net"
	"sync/atomic"
	"time"
)

func AudioStatsLogger(logger *slog.Logger) func(string, AudioStatistics) {
	return func(id string, stats AudioStatistics) {
		logger.Info("call audio levels", "component", "callmedia", "call_id", id,
			"transport", stats.Transport, "state", stats.State, "failure_code", stats.FailureCode,
			"received_packets", stats.ReceivedPackets, "received_bytes", stats.ReceivedBytes,
			"sent_packets", stats.SentPackets, "dropped_packets", stats.DroppedPackets, "input_dbfs", stats.InputDBFS,
			"input_peak_dbfs", stats.InputPeakDBFS, "output_dbfs", stats.OutputDBFS,
			"dropped_source_early_packets", stats.DroppedSourceEarlyPackets,
			"dropped_source_late_packets", stats.DroppedSourceLatePackets,
			"dropped_queue_overflow_packets", stats.DroppedQueueOverflowPackets,
			"dropped_reanchor_packets", stats.DroppedReanchorPackets,
			"dropped_playout_packets", stats.DroppedPlayoutPackets,
			"dropped_rebuffer_packets", stats.DroppedRebufferPackets,
			"clock_reanchors", stats.ClockReanchors,
			"playout_underruns", stats.PlayoutUnderruns,
			"playout_silence_frames", stats.PlayoutSilenceFrames,
			"playout_missed_ticks", stats.PlayoutMissedTicks)
	}
}

// AudioStatistics contains counters and levels only, never audio or addresses.
type AudioStatistics struct {
	DroppedSourceEarlyPackets   uint64 `json:"dropped_source_early_packets,omitempty"`
	DroppedSourceLatePackets    uint64 `json:"dropped_source_late_packets,omitempty"`
	DroppedQueueOverflowPackets uint64 `json:"dropped_queue_overflow_packets,omitempty"`
	DroppedReanchorPackets      uint64 `json:"dropped_reanchor_packets,omitempty"`
	DroppedPlayoutPackets       uint64 `json:"dropped_playout_packets,omitempty"`
	DroppedRebufferPackets      uint64 `json:"dropped_rebuffer_packets,omitempty"`
	ClockReanchors              uint64 `json:"clock_reanchors,omitempty"`
	PlayoutUnderruns            uint64 `json:"playout_underruns,omitempty"`
	PlayoutSilenceFrames        uint64 `json:"playout_silence_frames,omitempty"`
	PlayoutMissedTicks          uint64 `json:"playout_missed_ticks,omitempty"`
	Transport                   string `json:"transport,omitempty"`
	State                       string `json:"state,omitempty"`
	FailureCode                 string `json:"failure_code,omitempty"`
	ReceivedPackets             uint64 `json:"received_packets"`
	ReceivedBytes               uint64 `json:"received_bytes"`
	DroppedPackets              uint64 `json:"dropped_packets,omitempty"`
	SentPackets                 uint64 `json:"sent_packets"`
	InputDBFS                   int    `json:"input_dbfs"`
	InputPeakDBFS               int    `json:"input_peak_dbfs"`
	OutputDBFS                  int    `json:"output_dbfs"`
}

type audioCounters struct {
	droppedSourceEarlyPackets   atomic.Uint64
	droppedSourceLatePackets    atomic.Uint64
	droppedQueueOverflowPackets atomic.Uint64
	droppedReanchorPackets      atomic.Uint64
	droppedPlayoutPackets       atomic.Uint64
	droppedRebufferPackets      atomic.Uint64
	clockReanchors              atomic.Uint64
	playoutUnderruns            atomic.Uint64
	playoutSilenceFrames        atomic.Uint64
	playoutMissedTicks          atomic.Uint64
	receivedPackets             atomic.Uint64
	receivedBytes               atomic.Uint64
	sentPackets                 atomic.Uint64
	droppedPackets              atomic.Uint64
	inputDBFS                   atomic.Int64
	inputPeakDBFS               atomic.Int64
	outputDBFS                  atomic.Int64
}

func (c *audioCounters) recordSourceDrop(age time.Duration) {
	if age < -socketLateBudget {
		c.droppedSourceEarlyPackets.Add(1)
	} else {
		c.droppedSourceLatePackets.Add(1)
	}
	c.droppedPackets.Add(1)
}
func (c *audioCounters) recordQueueDrops(d socketDrops) {
	c.droppedQueueOverflowPackets.Add(d.overflow)
	c.droppedReanchorPackets.Add(d.reanchor)
	c.droppedPlayoutPackets.Add(d.playout)
	c.droppedRebufferPackets.Add(d.rebuffer)
	c.droppedPackets.Add(d.total())
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
		SentPackets: s.baseStats.SentPackets + s.stats.sentPackets.Load(), DroppedPackets: s.baseStats.DroppedPackets + s.stats.droppedPackets.Load(), InputDBFS: int(s.stats.inputDBFS.Load()),
		InputPeakDBFS: int(s.stats.inputPeakDBFS.Load()), OutputDBFS: int(s.stats.outputDBFS.Load())}
	stats.DroppedSourceEarlyPackets = s.baseStats.DroppedSourceEarlyPackets + s.stats.droppedSourceEarlyPackets.Load()
	stats.DroppedSourceLatePackets = s.baseStats.DroppedSourceLatePackets + s.stats.droppedSourceLatePackets.Load()
	stats.DroppedQueueOverflowPackets = s.baseStats.DroppedQueueOverflowPackets + s.stats.droppedQueueOverflowPackets.Load()
	stats.DroppedReanchorPackets = s.baseStats.DroppedReanchorPackets + s.stats.droppedReanchorPackets.Load()
	stats.DroppedPlayoutPackets = s.baseStats.DroppedPlayoutPackets + s.stats.droppedPlayoutPackets.Load()
	stats.DroppedRebufferPackets = s.baseStats.DroppedRebufferPackets + s.stats.droppedRebufferPackets.Load()
	stats.ClockReanchors = s.baseStats.ClockReanchors + s.stats.clockReanchors.Load()
	stats.PlayoutUnderruns = s.baseStats.PlayoutUnderruns + s.stats.playoutUnderruns.Load()
	stats.PlayoutSilenceFrames = s.baseStats.PlayoutSilenceFrames + s.stats.playoutSilenceFrames.Load()
	stats.PlayoutMissedTicks = s.baseStats.PlayoutMissedTicks + s.stats.playoutMissedTicks.Load()
	stats.Transport = "websocket"
	stats.Transport = "websocket"
	// Derive the public total from the same category snapshot, so concurrent
	// updates cannot make telemetry disagree with its own six-way breakdown.
	stats.DroppedPackets = stats.DroppedSourceEarlyPackets + stats.DroppedSourceLatePackets + stats.DroppedQueueOverflowPackets + stats.DroppedReanchorPackets + stats.DroppedPlayoutPackets + stats.DroppedRebufferPackets
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
