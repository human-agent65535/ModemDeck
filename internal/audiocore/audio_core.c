//go:build cgo

#include "audio_core.h"

#define FRAME_US INT64_C(20000)
#define PREBUFFER_US INT64_C(40000)
#define MAX_AGE_US INT64_C(100000)
#define WINDOW_US INT64_C(100000)
#define WINDOW_TOLERANCE_US INT64_C(20000)
#define DRIFT_LIMIT_US INT64_C(100)

static int64_t microseconds(double seconds) {
    double scaled = seconds * 1000000.0;
    return (int64_t)(scaled + (scaled < 0 ? -0.5 : 0.5));
}

static int valid_time(double seconds) {
    /* Also rejects NaN/infinity before a float-to-integer conversion. */
    return seconds >= -1000000000000.0 && seconds <= 1000000000000.0;
}

void md_audio_clock_reset(md_audio_clock *clock) {
    *clock = (md_audio_clock){0};
}

int md_audio_clock_accept(md_audio_clock *clock, uint32_t sequence,
                          uint32_t timestamp, double now_seconds) {
    if (!valid_time(now_seconds)) return -1;
    int64_t now = microseconds(now_seconds);
    if (clock->initialized) {
        uint32_t advance = sequence - clock->sequence;
        if (advance == 0 || advance >= UINT32_C(0x80000000) ||
            timestamp - clock->timestamp != advance * UINT32_C(320)) return -1;
        int64_t span = (int64_t)advance * FRAME_US;
        /* Bound malicious jumps as well as ordinary long-call accumulation. */
        if (clock->media_us > INT64_C(4000000000000000000) - span) return -1;
        clock->media_us += span;
    } else {
        clock->initialized = 1;
        clock->anchor_arrival_us = clock->window_arrival_us = now;
        clock->generation = 1;
    }
    clock->sequence = sequence;
    clock->timestamp = timestamp;
    int64_t predicted = clock->anchor_arrival_us + clock->media_us - clock->anchor_media_us;
    clock->age_us = now - predicted;
    int outside = clock->age_us > MAX_AGE_US || clock->age_us < -MAX_AGE_US;
    if (outside && !clock->recovering) {
        clock->recovering = 1;
        clock->stable_windows = 0;
        clock->window_media_us = clock->media_us;
        clock->window_arrival_us = now;
        clock->window_had_outside = 1;
    }
    /* Fresh tails in a partially stale batch do not erase the stale head.
     * Observe independent complete source windows, not individual arrivals. */
    clock->window_had_outside |= outside;
    int64_t span = clock->media_us - clock->window_media_us;
    if (span >= WINDOW_US) {
        int64_t difference = now - clock->window_arrival_us - span;
        int stable = difference >= -WINDOW_TOLERANCE_US && difference <= WINDOW_TOLERANCE_US;
        if (clock->recovering) {
            if (stable && !clock->window_had_outside) {
                /* Natural catch-up ends an episode without renewing deadlines
                 * or interpreting the catch-up interval as oscillator drift. */
                clock->recovering = 0;
                clock->stable_windows = 0;
            } else {
                clock->stable_windows = stable ? clock->stable_windows + 1 : 0;
                if (clock->stable_windows == 3) {
                    clock->anchor_media_us = clock->media_us;
                    clock->anchor_arrival_us = now;
                    clock->generation++;
                    predicted = now;
                    clock->age_us = 0;
                    outside = 0;
                    clock->recovering = 0;
                    clock->stable_windows = 0;
                }
            }
        } else if (stable) {
            int64_t correction = difference;
            if (correction > DRIFT_LIMIT_US) correction = DRIFT_LIMIT_US;
            if (correction < -DRIFT_LIMIT_US) correction = -DRIFT_LIMIT_US;
            clock->anchor_arrival_us += correction;
            predicted += correction;
        }
        clock->window_media_us = clock->media_us;
        clock->window_arrival_us = now;
        /* The boundary frame participates in both adjacent windows. */
        clock->window_had_outside = outside;
    }
    if (outside) return 0;
    clock->play_at_us = predicted + PREBUFFER_US;
    return 1;
}

double md_audio_clock_play_at(const md_audio_clock *clock) { return (double)clock->play_at_us / 1000000.0; }
double md_audio_clock_age(const md_audio_clock *clock) { return (double)clock->age_us / 1000000.0; }
double md_audio_clock_source_samples(const md_audio_clock *clock) { return (double)(clock->media_us / FRAME_US) * 320.0; }
uint32_t md_audio_clock_generation(const md_audio_clock *clock) { return clock->generation; }
uint32_t md_audio_frame_samples(void) { return 320; }
uint32_t md_audio_queue_capacity(void) { return 7; }
uint32_t md_audio_send_queue_capacity(void) { return (uint32_t)(MAX_AGE_US / FRAME_US); }
double md_audio_frame_seconds(void) { return (double)FRAME_US / 1000000.0; }
double md_audio_prebuffer_seconds(void) { return (double)PREBUFFER_US / 1000000.0; }
double md_audio_max_age_seconds(void) { return (double)MAX_AGE_US / 1000000.0; }
int md_audio_frame_expired(double source_seconds, double now_seconds) {
    if (!valid_time(source_seconds) || !valid_time(now_seconds)) return 1;
    return microseconds(now_seconds) - microseconds(source_seconds) > MAX_AGE_US;
}
double md_audio_playback_start(double source_play_at, double now_seconds) {
    double earliest = now_seconds + md_audio_prebuffer_seconds();
    return source_play_at > earliest ? source_play_at : earliest;
}

double md_audio_source_slot(double epoch_seconds, double relative_source_samples) {
    return epoch_seconds + relative_source_samples / 16000.0;
}
