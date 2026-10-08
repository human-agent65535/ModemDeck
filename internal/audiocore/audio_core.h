#ifndef MODEMDECK_AUDIO_CORE_H
#define MODEMDECK_AUDIO_CORE_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* One source clock for the native, browser and server adapters. Times enter
 * and leave in monotonic seconds; integer microseconds own all decisions. */
typedef struct {
    int64_t media_us, anchor_media_us, anchor_arrival_us;
    int64_t window_media_us, window_arrival_us, play_at_us, age_us;
    uint32_t sequence, timestamp, generation;
    int initialized, recovering, stable_windows, window_had_outside;
} md_audio_clock;

void md_audio_clock_reset(md_audio_clock *clock);
/* -1: malformed clock, 0: valid but stale/early, 1: admitted. Invalid input
 * does not mutate state. Valid discarded packets still advance source time. */
int md_audio_clock_accept(md_audio_clock *clock, uint32_t sequence,
                          uint32_t timestamp, double now_seconds);
double md_audio_clock_play_at(const md_audio_clock *clock);
double md_audio_clock_age(const md_audio_clock *clock);
/* Unwrapped source progress, including omitted frames and uint32 wraps. */
double md_audio_clock_source_samples(const md_audio_clock *clock);
uint32_t md_audio_clock_generation(const md_audio_clock *clock);

uint32_t md_audio_frame_samples(void);
uint32_t md_audio_queue_capacity(void);
uint32_t md_audio_send_queue_capacity(void);
double md_audio_frame_seconds(void);
double md_audio_prebuffer_seconds(void);
double md_audio_max_age_seconds(void);
/* Capture uses the frame's sample-end time; playback uses its original source
 * slot. Rebuffering may change the device epoch, never that original deadline. */
int md_audio_frame_expired(double source_seconds, double now_seconds);
double md_audio_playback_start(double source_play_at, double now_seconds);
double md_audio_source_slot(double epoch_seconds, double relative_source_samples);

#ifdef __cplusplus
}
#endif
#endif
