import Foundation
// INSERT_PRODUCT_METHODS
@main struct Tests {
    static func main() {
        let tone = ModemDeckRingbackRenderer()
        let n = 96_000
        let samples = UnsafeMutablePointer<Float>.allocate(capacity: n)
        samples.initialize(repeating: 0, count: n)
        defer { samples.deinitialize(count: n); samples.deallocate() }
        tone.setEnabled(true); tone.mix(samples, count: n)
        precondition((0..<32_000).contains { abs(samples[$0]) > 0.05 })
        precondition((32_000..<n).allSatisfy { samples[$0] == 0 }, "ringback cadence must be 2 seconds on and 4 off")
        samples.update(repeating: 0, count: n)
        tone.mix(samples, count: 320)
        precondition((0..<320).contains { samples[$0] != 0 }, "remote silence must not suppress local progress")
        samples.update(repeating: 0, count: n); samples[0] = 0.02
        tone.mix(samples, count: 320)
        precondition(samples[0] == 0.02 && (1..<320).allSatisfy { samples[$0] == 0 }, "audible remote media must immediately replace progress tone")
        samples.update(repeating: 0, count: n)
        tone.setEnabled(false); tone.setEnabled(true); tone.mix(samples, count: 320)
        precondition((0..<320).allSatisfy { samples[$0] == 0 }, "later ringing updates must not restore tone after early media")
        print("ringback-ok")
    }
}
