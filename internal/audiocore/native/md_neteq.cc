#include "md_neteq.h"
#include "api/audio/audio_frame.h"
#include "api/audio_codecs/audio_decoder_factory_template.h"
#include "api/audio_codecs/opus/audio_decoder_opus.h"
#include "api/environment/environment_factory.h"
#include "api/make_ref_counted.h"
#include "api/neteq/default_neteq_factory.h"
#include "api/rtp_headers.h"
#include "common_audio/resampler/include/resampler.h"
#include "opus.h"
#include "rtc_base/logging.h"
#include "system_wrappers/include/clock.h"
#include <algorithm>
#include <array>
#include <atomic>
#include <climits>
#include <cstring>
#include <memory>
#include <new>
using namespace webrtc;
class MonoOpusFactory : public AudioDecoderFactory {
public:
  std::vector<AudioCodecSpec> GetSupportedDecoders() override {
    std::vector<AudioCodecSpec> s;
    AudioDecoderOpus::AppendSupportedDecoders(&s);
    return s;
  }
  bool IsSupportedDecoder(const SdpAudioFormat &f) override {
    return AudioDecoderOpus::SdpToConfig(f).has_value();
  }
  std::unique_ptr<AudioDecoder>
  Create(const Environment &e, const SdpAudioFormat &f,
         std::optional<AudioCodecPairId>) override {
    auto c = AudioDecoderOpus::SdpToConfig(f);
    if (!c)
      return nullptr;
    c->sample_rate_hz = 48000;
    c->num_channels = 1;
    return AudioDecoderOpus::MakeAudioDecoder(e, *c);
  }
};

struct md_neteq {
  struct Packet {
    std::array<uint8_t, MD_AUDIO_MAX_PAYLOAD> data;
    size_t len;
    uint16_t seq;
    uint32_t ts;
    int64_t arrival;
  };
  int rate;
  int64_t now = 0, last_pull = -1, last_render = -1;
  SimulatedClock clock{0};
  Environment env;
  std::unique_ptr<NetEq> neteq;
  AudioFrame frame;
  Resampler resampler;
  std::array<Packet, MD_AUDIO_INGRESS_CAPACITY> ingress;
  std::atomic<uint64_t> head{0}, tail{0};
  std::array<int16_t, 480> remainder{};
  size_t remainder_at = 0, remainder_size = 0;
  std::array<std::atomic<uint64_t>, 18> snapshot{};
  uint64_t real_output_samples = 0, last_real_render_us = 0;
  std::atomic<uint64_t> render_errors{0};
  std::atomic<uint64_t> snapshot_version{0};
  explicit md_neteq(int r)
      : rate(r), env(CreateEnvironment(&clock)), resampler(48000, r, 1) {
    NetEq::Config c;
    c.sample_rate_hz = 48000;
    c.max_packets_in_buffer = 200;
    c.min_delay_ms = 40;
    c.max_delay_ms = 200;
    neteq = DefaultNetEqFactory().Create(env, c,
                                         make_ref_counted<MonoOpusFactory>());
    if (!neteq ||
        !neteq->RegisterPayloadType(
            111, SdpAudioFormat("opus", 48000, 2,
                                {{"stereo", "0"}, {"useinbandfec", "1"}})))
      throw std::bad_alloc();
    neteq->CreateDecoder(111);
  }
  static bool valid_time(int64_t us) {
    return us >= 0 && us <= INT64_C(1000000000000000);
  }
  void advance(int64_t us) {
    if (us > now) {
      clock.AdvanceTimeMicroseconds(us - now);
      now = us;
    }
  }
  int insert(const uint8_t *data, size_t len, uint16_t seq, uint32_t ts,
             int64_t us) {
    advance(us);
    RTPHeader h;
    h.payloadType = 111;
    h.sequenceNumber = seq;
    h.timestamp = ts;
    h.ssrc = 1;
    return neteq->InsertPacket(h, std::span<const uint8_t>(data, len),
                               Timestamp::Micros(us));
  }
  int drain() {
    auto t = tail.load(std::memory_order_relaxed);
    auto h = head.load(std::memory_order_acquire);
    while (t != h) {
      auto &p = ingress[t % MD_AUDIO_INGRESS_CAPACITY];
      int rc = insert(p.data.data(), p.len, p.seq, p.ts, p.arrival);
      tail.store(++t, std::memory_order_release);
      if (rc != NetEq::kOK)
        return MD_AUDIO_ERROR;
    }
    return 0;
  }
  void publish() {
    auto l = neteq->GetLifetimeStatistics();
    auto ts = neteq->GetPlayoutTimestamp();
    const uint64_t values[18] = {
        l.concealed_samples,
        l.concealment_events,
        l.inserted_samples_for_deceleration,
        l.removed_samples_for_acceleration,
        l.packets_discarded,
        l.jitter_buffer_packets_received,
        l.jitter_buffer_emitted_count,
        l.jitter_buffer_delay_ms,
        l.jitter_buffer_target_delay_ms,
        uint64_t(std::max(0, neteq->TargetDelayMs())),
        uint64_t(std::max(0, neteq->FilteredCurrentDelayMs())),
        uint64_t(neteq->last_output_sample_rate_hz()),
        ts.value_or(0),
        ts.has_value() ? 1u : 0u,
        frame.timestamp_,
        uint64_t(frame.speech_type_),
        real_output_samples,
        last_real_render_us};
    snapshot_version.fetch_add(1, std::memory_order_seq_cst);
    for (size_t i = 0; i < 18; i++)
      snapshot[i].store(values[i], std::memory_order_seq_cst);
    snapshot_version.fetch_add(1, std::memory_order_seq_cst);
  }
};
static bool valid_packet(const uint8_t *p, size_t n, int64_t t) {
  return p && n && n <= MD_AUDIO_MAX_PAYLOAD && md_neteq::valid_time(t) &&
         opus_packet_get_nb_channels(p) == 1 &&
         opus_packet_get_nb_samples(p, int(n), 48000) == 960;
}
extern "C" md_neteq *md_neteq_create(int rate) {
  if (rate != 8000 && rate != 16000 && rate != 48000)
    return nullptr;
  try {
    LogMessage::LogToDebug(LS_NONE);
    return new md_neteq(rate);
  } catch (...) {
    return nullptr;
  }
}
extern "C" int md_neteq_insert(md_neteq *p, const uint8_t *data, size_t len,
                               uint16_t seq, uint32_t ts, int64_t us) {
  if (!p || !valid_packet(data, len, us))
    return MD_AUDIO_INVALID;
  return p->insert(data, len, seq, ts, us) == NetEq::kOK ? 0 : MD_AUDIO_ERROR;
}
extern "C" int md_neteq_enqueue(md_neteq *p, const uint8_t *data, size_t len,
                                uint16_t seq, uint32_t ts, int64_t us) {
  if (!p || !valid_packet(data, len, us))
    return MD_AUDIO_INVALID;
  auto h = p->head.load(std::memory_order_relaxed);
  auto t = p->tail.load(std::memory_order_acquire);
  if (h - t == MD_AUDIO_INGRESS_CAPACITY)
    return MD_AUDIO_BACKPRESSURE;
  auto &packet = p->ingress[h % MD_AUDIO_INGRESS_CAPACITY];
  std::memcpy(packet.data.data(), data, len);
  packet.len = len;
  packet.seq = seq;
  packet.ts = ts;
  packet.arrival = us;
  p->head.store(h + 1, std::memory_order_release);
  return 0;
}
extern "C" int md_neteq_pull(md_neteq *p, int64_t us, int16_t *out,
                             size_t cap) {
  if (!p || !out || cap < size_t(p->rate / 100) || !md_neteq::valid_time(us) ||
      us < p->last_pull)
    return MD_AUDIO_INVALID;
  int rc = p->drain();
  if (rc < 0)
    return rc;
  p->advance(us);
  p->last_pull = us;
  bool muted = false;
  if (p->neteq->GetAudio(&p->frame, &muted) != NetEq::kOK)
    return MD_AUDIO_ERROR;
  if (p->frame.sample_rate_hz_ != 48000 || p->frame.num_channels_ != 1 ||
      p->frame.samples_per_channel_ != 480)
    return MD_AUDIO_FORMAT;
  int count = p->rate / 100;
  if (muted)
    std::memset(out, 0, count * sizeof(int16_t));
  else if (p->rate == 48000)
    std::memcpy(out, p->frame.data(), 480 * sizeof(int16_t));
  else {
    size_t n = 0;
    if (p->resampler.Push(p->frame.data(), 480, out, cap, n) != 0 ||
        n != size_t(count))
      return MD_AUDIO_RESAMPLE;
  }
  if (!muted && p->frame.speech_type_ == AudioFrame::kNormalSpeech) {
    p->real_output_samples += count;
    p->last_real_render_us = us;
  }
  p->publish();
  return count;
}
extern "C" int md_neteq_render_float(md_neteq *p, int64_t us, float *out,
                                     size_t count) {
  if (!p)
    return MD_AUDIO_INVALID;
  if (!out || count == 0 || count > 8192 || !md_neteq::valid_time(us) ||
      us < p->last_render) {
    p->render_errors.fetch_add(1, std::memory_order_relaxed);
    return MD_AUDIO_INVALID;
  }
  p->last_render = us;
  size_t written = 0;
  while (written < count) {
    if (p->remainder_at == p->remainder_size) {
      int n = md_neteq_pull(p, us, p->remainder.data(), p->remainder.size());
      if (n < 0) {
        p->render_errors.fetch_add(1, std::memory_order_relaxed);
        std::fill(out + written, out + count, 0.0f);
        return n;
      }
      p->remainder_at = 0;
      p->remainder_size = size_t(n);
    }
    size_t n = std::min(count - written, p->remainder_size - p->remainder_at);
    for (size_t i = 0; i < n; i++)
      out[written + i] = float(p->remainder[p->remainder_at + i]) / 32768.0f;
    p->remainder_at += n;
    written += n;
  }
  return int(count);
}
extern "C" int md_neteq_reset_render(md_neteq *p) {
  if (!p)
    return MD_AUDIO_INVALID;
  p->remainder_at = p->remainder_size = 0;
  return 0;
}
extern "C" int md_neteq_get_stats(md_neteq *p, md_neteq_stats *s) {
  if (!p || !s)
    return MD_AUDIO_INVALID;
  uint64_t v[18], before, after;
  do {
    before = p->snapshot_version.load(std::memory_order_seq_cst);
    if (before & 1)
      continue;
    for (size_t i = 0; i < 18; i++)
      v[i] = p->snapshot[i].load(std::memory_order_seq_cst);
    after = p->snapshot_version.load(std::memory_order_seq_cst);
  } while ((before & 1) || before != after);
  *s = {v[0],
        v[1],
        v[2],
        v[3],
        v[4],
        v[5],
        v[6],
        v[7],
        v[8],
        uint32_t(v[9]),
        uint32_t(v[10]),
        uint32_t(v[11]),
        uint32_t(v[12]),
        uint32_t(v[13]),
        uint32_t(v[14]),
        uint32_t(v[15]),
        v[16],
        v[17],
        p->render_errors.load(std::memory_order_relaxed),
        0};
  auto tail = p->tail.load(std::memory_order_acquire);
  auto head = p->head.load(std::memory_order_acquire);
  s->ingress_queued =
      uint32_t(std::min<uint64_t>(MD_AUDIO_INGRESS_CAPACITY, head - tail));
  return 0;
}
extern "C" void md_neteq_destroy(md_neteq *p) { delete p; }
extern "C" int md_audio_ingress_capacity() { return MD_AUDIO_INGRESS_CAPACITY; }
extern "C" int md_audio_send_capacity() { return MD_AUDIO_SEND_CAPACITY; }
struct md_opus_encoder {
  OpusEncoder *state;
};
struct md_opus_decoder {
  OpusDecoder *state;
};
static bool valid_codec(int rate, int channels) {
  return (rate == 8000 || rate == 12000 || rate == 16000 || rate == 24000 ||
          rate == 48000) &&
         (channels == 1 || channels == 2);
}
extern "C" md_opus_encoder *md_opus_encoder_create(int rate, int channels) {
  if (!valid_codec(rate, channels))
    return nullptr;
  int code;
  OpusEncoder *e =
      opus_encoder_create(rate, channels, OPUS_APPLICATION_VOIP, &code);
  if (!e)
    return nullptr;
  if (opus_encoder_ctl(e, OPUS_SET_BITRATE(24000)) != OPUS_OK ||
      opus_encoder_ctl(e, OPUS_SET_VBR(1)) != OPUS_OK ||
      opus_encoder_ctl(e, OPUS_SET_VBR_CONSTRAINT(1)) != OPUS_OK ||
      opus_encoder_ctl(e, OPUS_SET_COMPLEXITY(8)) != OPUS_OK ||
      opus_encoder_ctl(e, OPUS_SET_SIGNAL(OPUS_SIGNAL_VOICE)) != OPUS_OK ||
      opus_encoder_ctl(e, OPUS_SET_INBAND_FEC(1)) != OPUS_OK ||
      opus_encoder_ctl(e, OPUS_SET_PACKET_LOSS_PERC(10)) != OPUS_OK ||
      opus_encoder_ctl(e, OPUS_SET_DTX(0)) != OPUS_OK) {
    opus_encoder_destroy(e);
    return nullptr;
  }
  auto *p = new (std::nothrow) md_opus_encoder{e};
  if (!p)
    opus_encoder_destroy(e);
  return p;
}
extern "C" int md_opus_encode(md_opus_encoder *p, const int16_t *pcm, int n,
                              uint8_t *out, size_t cap) {
  if (!p || !pcm || !out || n <= 0 || cap == 0 || cap > INT_MAX)
    return MD_AUDIO_INVALID;
  return opus_encode(p->state, pcm, n, out, int(cap));
}
extern "C" int md_opus_encode_float(md_opus_encoder *p, const float *pcm, int n,
                                    uint8_t *out, size_t cap) {
  if (!p || !pcm || !out || n <= 0 || cap == 0 || cap > INT_MAX)
    return MD_AUDIO_INVALID;
  return opus_encode_float(p->state, pcm, n, out, int(cap));
}
extern "C" void md_opus_encoder_destroy(md_opus_encoder *p) {
  if (p) {
    opus_encoder_destroy(p->state);
    delete p;
  }
}
extern "C" md_opus_decoder *md_opus_decoder_create(int rate, int channels) {
  if (!valid_codec(rate, channels))
    return nullptr;
  int code;
  auto *d = opus_decoder_create(rate, channels, &code);
  if (!d)
    return nullptr;
  auto *p = new (std::nothrow) md_opus_decoder{d};
  if (!p)
    opus_decoder_destroy(d);
  return p;
}
extern "C" int md_opus_decode(md_opus_decoder *p, const uint8_t *data, size_t n,
                              int16_t *out, int cap) {
  if (!p || !data || !n || n > MD_AUDIO_MAX_PAYLOAD || !out || cap <= 0)
    return MD_AUDIO_INVALID;
  return opus_decode(p->state, data, int(n), out, cap, 0);
}
extern "C" int md_opus_conceal(md_opus_decoder *p, int n, int16_t *out,
                               int cap) {
  if (!p || !out || n <= 0 || n > cap)
    return MD_AUDIO_INVALID;
  return opus_decode(p->state, nullptr, 0, out, n, 0);
}
extern "C" void md_opus_decoder_destroy(md_opus_decoder *p) {
  if (p) {
    opus_decoder_destroy(p->state);
    delete p;
  }
}
extern "C" int md_opus_packet_samples(const uint8_t *data, size_t n, int rate) {
  if (!data || !n || n > MD_AUDIO_MAX_PAYLOAD || !valid_codec(rate, 1))
    return MD_AUDIO_INVALID;
  return opus_packet_get_nb_samples(data, int(n), rate);
}
extern "C" const char *md_opus_version() { return opus_get_version_string(); }
