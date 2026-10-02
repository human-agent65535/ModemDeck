import Foundation
private final class Owner {
    let queue = DispatchQueue(label: "heartbeat-test")
    let renewed = DispatchSemaphore(value: 0)
    var stopped = false, leaseRequestInFlight = false
    var leaseTimer: DispatchSourceTimer?
    var requests = 0
    let callID = "audit", testCall: Bool
    init(testCall: Bool) { self.testCall = testCall }
    func request(path: String, method: String, json: [String: String], completion: @escaping (Result<Void, Error>) -> Void) {
        let prefix = testCall ? "/api/v1/mobile/call-tests" : "/api/v1/calls"
        precondition(path == "\(prefix)/audit/lease" && method == "PUT" && json.isEmpty)
        requests += 1
        completion(.success(()))
        renewed.signal()
    }
    // INSERT_PRODUCT_METHODS
}
@main struct Tests {
    static func main() {
        let owners = [Owner(testCall: false), Owner(testCall: true)]
        // No connect() or media/ICE exchange has happened.
        for c in owners { c.startControlHeartbeat(); c.startControlHeartbeat() }
        for c in owners {
            precondition(c.renewed.wait(timeout: .now() + 7) == .success)
            c.queue.sync {
                precondition(c.requests == 1 && !c.leaseRequestInFlight)
                c.stopped = true
                c.leaseTimer?.cancel(); c.leaseTimer = nil
            }
            c.startControlHeartbeat()
            c.queue.sync { precondition(c.leaseTimer == nil) }
        }
    }
}
