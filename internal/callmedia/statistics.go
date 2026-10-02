package callmedia

import (
	"encoding/binary"
	"log/slog"
	"math"
	"sync/atomic"
)

func AudioStatsLogger(logger *slog.Logger) func(string, AudioStatistics) {
	return func(id string, stats AudioStatistics) {
		logger.Info("call audio levels", "component", "callmedia", "call_id", id,
			"received_packets", stats.ReceivedPackets, "received_bytes", stats.ReceivedBytes,
			"sent_packets", stats.SentPackets, "input_dbfs", stats.InputDBFS,
			"input_peak_dbfs", stats.InputPeakDBFS, "output_dbfs", stats.OutputDBFS)
	}
}

// AudioStatistics contains counters and levels only, never audio or addresses.
type AudioStatistics struct {
	ReceivedPackets uint64 `json:"received_packets"`
	ReceivedBytes   uint64 `json:"received_bytes"`
	SentPackets     uint64 `json:"sent_packets"`
	InputDBFS       int    `json:"input_dbfs"`
	InputPeakDBFS   int    `json:"input_peak_dbfs"`
	OutputDBFS      int    `json:"output_dbfs"`
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
	return AudioStatistics{ReceivedPackets: s.stats.receivedPackets.Load(), ReceivedBytes: s.stats.receivedBytes.Load(),
		SentPackets: s.stats.sentPackets.Load(), InputDBFS: int(s.stats.inputDBFS.Load()),
		InputPeakDBFS: int(s.stats.inputPeakDBFS.Load()), OutputDBFS: int(s.stats.outputDBFS.Load())}
}

func (c *Core) Statistics(callID string) AudioStatistics {
	c.mu.Lock()
	defer c.mu.Unlock()
	if owner := c.owners[callID]; owner != nil && owner.session != nil {
		return owner.session.Statistics()
	}
	return AudioStatistics{InputDBFS: -96, InputPeakDBFS: -96, OutputDBFS: -96}
}
