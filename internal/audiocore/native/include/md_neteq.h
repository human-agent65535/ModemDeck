#ifndef MD_NETEQ_H
#define MD_NETEQ_H
#include <stddef.h>
#include <stdint.h>
#ifdef __cplusplus
extern "C" {
#endif
/* Resource limits, not audio age or end-to-end delay guarantees. */
enum {
  MD_AUDIO_MAX_PAYLOAD = 1275,
  MD_AUDIO_INGRESS_CAPACITY = 200,
  MD_AUDIO_SEND_CAPACITY = 100,
  MD_AUDIO_ERROR = -1,
  MD_AUDIO_INVALID = -2,
  MD_AUDIO_FORMAT = -3,
  MD_AUDIO_RESAMPLE = -4,
  MD_AUDIO_BACKPRESSURE = -5
};
typedef struct md_neteq md_neteq;
typedef struct md_neteq_stats {
  uint64_t concealed_samples, concealment_events, inserted_samples,
      removed_samples, packets_discarded, packets_received, emitted_count,
      delay_ms_sum, target_delay_ms_sum;
  uint32_t target_delay_ms, current_delay_ms, internal_sample_rate,
      playout_timestamp48k, playout_timestamp_valid, frame_timestamp,
      speech_type;
  uint64_t real_output_samples, last_real_render_us, render_errors;
  uint32_t ingress_queued;
} md_neteq_stats;
/* NetEq/Opus always decode internally at48k. Supported outputs8/16/48k mono.
   min40/max200ms configure adaptive target delay, not an absolute age limit. */
md_neteq *md_neteq_create(int output_sample_rate);
/* Direct insert: render owner only. True arrival may precede last pull;
   environment time advances by max(now,arrival), packetInfo retains arrival. */
int md_neteq_insert(md_neteq *, const uint8_t *, size_t, uint16_t, uint32_t,
                    int64_t arrival_us);
/* SPSC: exactly one network producer; pull/render is the sole consumer.
   Copies one mono20ms Opus payload into preallocated storage; other formats
   return INVALID. Full returns BACKPRESSURE.
   Do not mix direct insert and enqueue concurrently. Times are nonnegative us
   relative to one monotonic epoch, RTP timestamp48k/sequence16 wrap naturally.
 */
int md_neteq_enqueue(md_neteq *, const uint8_t *, size_t, uint16_t, uint32_t,
                     int64_t arrival_us);
/* Owner-only: drains ingress then GetAudio exactly10ms. Nondecreasing pull
 * time. */
int md_neteq_pull(md_neteq *, int64_t now_us, int16_t *, size_t capacity);
/* Owner-only arbitrary mono frame demand; holds at most one10ms PCM remainder.
   All GetAudio calls within one callback receive the same actual now_us;
   NetEq advances its own10ms tick. No synthetic future wall time.
   No host PCM jitter queue, deadline cutting, or independently shifted epoch.
   Returns requested frame_count or negative; output is normalized[-1,1]. */
int md_neteq_render_float(md_neteq *, int64_t now_us, float *,
                          size_t frame_count);
/* Owner-only, after stopping render. Drops only the sub10ms device remainder;
   retains NetEq packet buffer/decoder/adaptive state and the monotonic clock.
 */
int md_neteq_reset_render(md_neteq *);
/* Concurrent-safe snapshot, published after each successful pull. */
int md_neteq_get_stats(md_neteq *, md_neteq_stats *);
/* Stop producer and render before destroy. */
void md_neteq_destroy(md_neteq *);
int md_audio_ingress_capacity(void);
int md_audio_send_capacity(void);
/* Common bundled Opus, interleaved PCM. A codec instance has one owner.
   Encoder defaults: VOIP24kbps constrained VBR, complexity8, voice,FEC1,
   expected loss10%, DTX0. samples_per_channel is a legal Opus frame size.
   Encode returns payload bytes; decode/conceal return samples/channel. */
typedef struct md_opus_encoder md_opus_encoder;
typedef struct md_opus_decoder md_opus_decoder;
md_opus_encoder *md_opus_encoder_create(int sample_rate, int channels);
int md_opus_encode(md_opus_encoder *, const int16_t *, int samples_per_channel,
                   uint8_t *, size_t capacity);
int md_opus_encode_float(md_opus_encoder *, const float *,
                         int samples_per_channel, uint8_t *, size_t capacity);
void md_opus_encoder_destroy(md_opus_encoder *);
md_opus_decoder *md_opus_decoder_create(int sample_rate, int channels);
int md_opus_decode(md_opus_decoder *, const uint8_t *, size_t, int16_t *,
                   int capacity_per_channel);
int md_opus_conceal(md_opus_decoder *, int samples_per_channel, int16_t *,
                    int capacity_per_channel);
void md_opus_decoder_destroy(md_opus_decoder *);
int md_opus_packet_samples(const uint8_t *, size_t, int sample_rate);
const char *md_opus_version(void);
#ifdef __cplusplus
}
#endif
#endif
