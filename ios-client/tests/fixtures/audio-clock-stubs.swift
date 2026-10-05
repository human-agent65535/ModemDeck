import Foundation
// INSERT_PRODUCT_METHODS
@main struct Tests {
    static func main() {
        var clock = ModemDeckAudioClock()
        precondition(clock.accept(sequence: 0, timestamp: 0, now: 10) == true)
        precondition(clock.accept(sequence: 0, timestamp: 0, now: 10.02) == nil)
        precondition(clock.accept(sequence: 1, timestamp: 641, now: 10.02) == nil)
        precondition(clock.accept(sequence: 1, timestamp: 320, now: 10.4) == false)
        precondition(clock.accept(sequence: 10, timestamp: 3200, now: 10.4) == false)
        precondition(clock.accept(sequence: 20, timestamp: 6400, now: 10.4) == true)
        precondition(clock.accept(sequence: 21, timestamp: 6720, now: 20) == false)
        precondition(clock.accept(sequence: 22, timestamp: 7040, now: 20.02) == false)
        precondition(clock.accept(sequence: 23, timestamp: 7360, now: 20.04) == false)
        precondition(clock.accept(sequence: 24, timestamp: 7680, now: 20.06) == true)
        var shifted = ModemDeckAudioClock()
        precondition(shifted.accept(sequence: 0, timestamp: 0, now: 10) == true)
        for index in 1...100 {
            let received = shifted.accept(sequence: UInt32(index), timestamp: UInt32(index * 320), now: 10 + Double(index) * 0.02 + 0.2)
            precondition(received == (index >= 4), "A sustained 200 ms latency step must recover after cadence stabilizes")
        }
        var burst = ModemDeckAudioClock()
        precondition(burst.accept(sequence: 0, timestamp: 0, now: 10) == true)
        for index in 1...10 {
            precondition(burst.accept(sequence: UInt32(index), timestamp: UInt32(index * 320), now: 10.8 + Double(index) * 0.001) == false,
                         "A stale queued burst must not establish a new playout clock")
        }
        for index in 11...13 {
            precondition(burst.accept(sequence: UInt32(index), timestamp: UInt32(index * 320), now: 10.81 + Double(index - 10) * 0.02) == (index == 13))
        }
        var wrapped = ModemDeckAudioClock()
        precondition(wrapped.accept(sequence: UInt32.max, timestamp: UInt32.max &- 319, now: 1) == true)
        precondition(wrapped.accept(sequence: 0, timestamp: 0, now: 1.02) == true)
    }
}
