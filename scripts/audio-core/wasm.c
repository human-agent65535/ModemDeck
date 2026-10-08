#include "audio_core.h"

/* Each WebAssembly instance is one receive clock. No allocator, imports or
 * process-global browser state; reconnects instantiate an empty clock. */
static md_audio_clock clock_state;
void md_clock_reset(void) { md_audio_clock_reset(&clock_state); }
int md_clock_accept(uint32_t sequence, uint32_t timestamp, double now_seconds) {
    return md_audio_clock_accept(&clock_state, sequence, timestamp, now_seconds);
}
double md_clock_play_at(void) { return md_audio_clock_play_at(&clock_state); }
double md_clock_source_samples(void) { return md_audio_clock_source_samples(&clock_state); }
double md_clock_age(void) { return md_audio_clock_age(&clock_state); }
uint32_t md_clock_generation(void) { return md_audio_clock_generation(&clock_state); }
