import Foundation
// INSERT_PRODUCT_METHODS
struct Vectors: Decodable {
    struct Case: Decodable {
        struct Frame: Decodable {
            let sequence: UInt32, timestamp: UInt32
            let arrival_us: Int64
            let play_us: Int64?
            let accepted: Bool
            let generation: Int
        }
        let name: String
        let frames: [Frame]
    }
    let version: Int
    let cases: [Case]
}
@main struct Tests {
    static func main() throws {
        let vectors = try JSONDecoder().decode(Vectors.self, from: Data(clockVectorsJSON.utf8))
        precondition(vectors.version == 1 && vectors.cases.count >= 7)
        for vector in vectors.cases {
            var clock = ModemDeckAudioClock()
            for (index, frame) in vector.frames.enumerated() {
                let accepted = clock.accept(sequence: frame.sequence, timestamp: frame.timestamp,
                                            now: Double(frame.arrival_us) / 1_000_000)
                precondition(accepted == frame.accepted,
                             "\(vector.name) frame \(index): acceptance \(String(describing: accepted)) != \(frame.accepted)")
                precondition(clock.generation == frame.generation,
                             "\(vector.name) frame \(index): generation \(clock.generation) != \(frame.generation)")
                if frame.accepted, let expectedPlay = frame.play_us {
                    precondition(abs(clock.playAt * 1_000_000 - Double(expectedPlay)) <= 2,
                                 "\(vector.name) frame \(index): playAt \(clock.playAt * 1_000_000) != \(expectedPlay)")
                }
            }
        }
        var malformed = ModemDeckAudioClock()
        precondition(malformed.accept(sequence: 0, timestamp: 0, now: 10) == true)
        precondition(malformed.accept(sequence: 0, timestamp: 0, now: 10.02) == nil)
        precondition(malformed.accept(sequence: 1, timestamp: 641, now: 10.02) == nil)
        precondition(malformed.accept(sequence: 1, timestamp: 320, now: 10.02) == true)
        precondition(malformed.generation == 1)
    }
}
