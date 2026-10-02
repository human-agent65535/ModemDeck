import Foundation

@main struct MediaRequestTests {
    static func main() {
        let queue = DispatchQueue(label: "media-request-tests")
        var request = URLRequest(url: URL(string: "https://example.invalid/api/v1/calls/call/media")!)
        request.httpMethod = "POST"
        request.httpBody = Data("same-offer-and-owner".utf8)
        let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!

        func scenario(_ errors: [URLError?], status: Int = 200, expired: Bool = false) -> (Int, NSError?, Int) {
            let done = DispatchSemaphore(value: 0)
            var attempts = 0
            var resultError: NSError?
            var resultStatus = 0
            var operation: ModemDeckMediaRequest!
            queue.async {
                operation = ModemDeckMediaRequest(queue: queue, retryDelay: 0.01) { sent, reply in
                    precondition(sent.httpBody == request.httpBody && sent.httpMethod == "POST")
                    let error = errors[min(attempts, errors.count-1)]
                    attempts += 1
                    let http = HTTPURLResponse(url: sent.url!, statusCode: status, httpVersion: nil, headerFields: nil)
                    reply(error == nil ? Data("answer".utf8) : nil, error == nil ? http : nil, error)
                    return {}
                }
                operation.start(request, deadline: Date().addingTimeInterval(expired ? -1 : 3)) { _, response, error in
                    resultError = error as NSError?
                    resultStatus = (response as? HTTPURLResponse)?.statusCode ?? 0
                    done.signal()
                }
            }
            precondition(done.wait(timeout: .now()+4) == .success)
            queue.sync { operation = nil }
            return (attempts, resultError, resultStatus)
        }
        let recovered = scenario([URLError(.networkConnectionLost), nil])
        precondition(recovered.0 == 2 && recovered.1 == nil && recovered.2 == 200)
        let bounded = scenario([URLError(.networkConnectionLost)])
        precondition(bounded.0 == 2 && bounded.1?.code == NSURLErrorNetworkConnectionLost)
        let canceled = scenario([URLError(.cancelled)])
        precondition(canceled.0 == 1 && canceled.1?.code == NSURLErrorCancelled)
        let denied = scenario([nil], status: 401)
        precondition(denied.0 == 1 && denied.2 == 401)
        let expired = scenario([nil], expired: true)
        precondition(expired.0 == 0 && expired.1?.code == NSURLErrorTimedOut)

        let checked = DispatchSemaphore(value: 0)
        queue.async {
            var attempts = 0
            let operation = ModemDeckMediaRequest(queue: queue, retryDelay: 0.05) { _, reply in
                attempts += 1
                reply(nil, nil, URLError(.networkConnectionLost))
                return {}
            }
            operation.onRetry = { [weak operation] _, _ in operation?.cancel() }
            operation.start(request, deadline: Date().addingTimeInterval(3)) { _, _, _ in
                preconditionFailure("hangup completed a canceled exchange")
            }
            queue.asyncAfter(deadline: .now()+0.12) {
                _ = operation
                precondition(attempts == 1)
                checked.signal()
            }
        }
        precondition(checked.wait(timeout: .now()+2) == .success)

        let late = DispatchSemaphore(value: 0)
        queue.async {
            var reply: ModemDeckMediaRequest.Reply?
            let operation = ModemDeckMediaRequest(queue: queue) { _, callback in reply = callback; return {} }
            operation.start(request, deadline: Date().addingTimeInterval(3)) { _, _, _ in
                preconditionFailure("late reply revived a canceled call")
            }
            operation.cancel()
            reply?(Data(), response, nil)
            queue.async { _ = operation; late.signal() }
        }
        precondition(late.wait(timeout: .now()+2) == .success)
        print("7 media request behavior tests passed")
    }
}
