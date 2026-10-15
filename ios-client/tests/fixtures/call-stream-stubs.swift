import Foundation
private enum ModemDeckNativeError: Error { case invalidResponse }
// INSERT_PRODUCT_METHODS
@main struct Tests {
    static func main() {
        var states: [ModemDeckRuntimeCallState] = []
        var failures = 0
        var durableEvents: [(String, Data)] = []
        let request = URLRequest(url: URL(string: "https://example.invalid/events")!)
        let stream = ModemDeckRuntimeCallStream(request: request, onState: { states.append($0) }, onEvent: { durableEvents.append(($0, $1)) }, onCompletion: { _, error in
            precondition(error != nil)
            failures += 1
        })
        let session = URLSession(configuration: .ephemeral)
        defer { session.invalidateAndCancel() }
        let task = session.dataTask(with: request)
        stream.urlSession(session, dataTask: task, didReceive: HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "text/event-stream"])!) { _ in }
        let payload = #"{"id":"call-1","revision":7,"line_id":"line-1","direction":"incoming","remote_number":"test","phase":"ringing","active":true,"ended":false,"was_answered":false,"control_state":"available","media_available":true}"#
        let event = "event: call_state\r\ndata: \(payload)\r\n\r\n"
        // Exercise framing across arbitrary network chunks.
        for byte in event.utf8 { stream.urlSession(session, dataTask: task, didReceive: Data([byte])) }
        precondition(states.count == 1, "server event must decode")
        precondition(states[0].lineID == "line-1" && states[0].controlState == "available")
        let changed = payload.replacingOccurrences(of: "\"control_state\":\"available\"", with: "\"control_state\":\"occupied\"")
        stream.urlSession(session, dataTask: task, didReceive: Data("event: call_state\ndata: \(changed)\n\n".utf8))
        precondition(states.count == 2 && states[1].revision == states[0].revision)
        let durable = #"{"epoch":"server-a","revision":12,"data_revision":8,"calls":{"calls":[]},"recordings":[]}"#
        for byte in "event: state\r\ndata: \(durable)\r\n\r\n".utf8 {
            stream.urlSession(session, dataTask: task, didReceive: Data([byte]))
        }
        precondition(durableEvents.count == 1 && durableEvents[0].0 == "state")
        precondition(String(data: durableEvents[0].1, encoding: .utf8) == durable)
        stream.urlSession(session, dataTask: task, didReceive: Data("event: heartbeat\ndata: {\"at\":\"2026-01-01T00:00:00Z\"}\n\n".utf8))
        precondition(durableEvents.count == 2 && durableEvents[1].0 == "heartbeat", "real server heartbeat framing must reach the durable owner")
        precondition(states.count == 2, "global durable events must not reconcile a CallKit call")
        stream.urlSession(session, dataTask: task, didReceive: Data("event: call_state\ndata: {}\n\n".utf8))
        precondition(failures == 1, "malformed state must force reconciliation, not disappear silently")
        stream.urlSession(session, dataTask: task, didReceive: Data(event.utf8))
        precondition(states.count == 2, "closed streams must not publish queued events")
    }
}
