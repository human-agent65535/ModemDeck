import Foundation
// INSERT_PRODUCT_METHODS
@main struct Tests {
    static func main() {
        let payload = Data([0x12, 0x34])
        let bytes = ModemDeckAudioPacket.encode(sequence: 0x01020304, timestamp: 0xfffffffe, payload: payload)
        precondition(Array(bytes) == [0x4d, 0x44, 1, 0, 1, 2, 3, 4, 255, 255, 255, 254, 0x12, 0x34])
        let decoded = ModemDeckAudioPacket.decode(bytes)!
        precondition(decoded.sequence == 0x01020304 && decoded.timestamp == 0xfffffffe && decoded.payload == payload)
        precondition(ModemDeckAudioPacket.decode(Data()) == nil)
        for position in 0..<4 {
            var invalid = bytes; invalid[position] ^= 0xff
            precondition(ModemDeckAudioPacket.decode(invalid) == nil)
        }
        precondition(ModemDeckAudioPacket.decode(bytes.prefix(12)) == nil)
        precondition(ModemDeckAudioPacket.decode(Data(repeating: 0, count: 1288)) == nil)
        let largest = ModemDeckAudioPacket.encode(sequence: UInt32.max, timestamp: UInt32.max, payload: Data(repeating: 5, count: 1275))
        precondition(ModemDeckAudioPacket.decode(largest)?.payload.count == 1275)
        let wrapped = ModemDeckAudioPacket.encode(sequence: UInt32.max &+ 1, timestamp: UInt32.max &+ 320, payload: payload)
        precondition(ModemDeckAudioPacket.decode(wrapped)?.sequence == 0)
        precondition(ModemDeckAudioPacket.decode(wrapped)?.timestamp == 319)
    }
}
