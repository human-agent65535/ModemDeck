import Foundation

/// Retries only the idempotent media exchange, with the exact same offer and
/// owner. Each attempt gets a fresh connection pool; CallKit's deadline wins.
final class ModemDeckMediaRequest {
    typealias Reply = (Data?, URLResponse?, Error?) -> Void
    typealias Sender = (URLRequest, @escaping Reply) -> (() -> Void)
    private let queue: DispatchQueue
    private let send: Sender
    private let retryDelay: TimeInterval
    private var cancelAttempt: (() -> Void)?
    private var retry: DispatchWorkItem?
    private var completed = false
    var onRetry: ((Int, Error) -> Void)?

    init(queue: DispatchQueue, retryDelay: TimeInterval = 0.3, send: @escaping Sender = ModemDeckMediaRequest.sendRequest) {
        self.queue = queue
        self.retryDelay = retryDelay
        self.send = send
    }

    func start(_ request: URLRequest, deadline: Date, completion: @escaping Reply) {
        attempt(request, deadline: deadline, number: 0, completion: completion)
    }

    func cancel() {
        completed = true
        retry?.cancel()
        retry = nil
        cancelAttempt?()
        cancelAttempt = nil
    }

    private func attempt(_ original: URLRequest, deadline: Date, number: Int, completion: @escaping Reply) {
        guard !completed else { return }
        let remaining = deadline.timeIntervalSinceNow
        guard remaining > 0 else {
            completed = true
            completion(nil, nil, URLError(.timedOut))
            return
        }
        var request = original
        request.timeoutInterval = min(number == 0 ? 10 : 8, remaining)
        cancelAttempt = send(request) { [weak self] data, response, error in
            guard let self else { return }
            self.queue.async { [self] in
                guard !self.completed else { return }
                self.cancelAttempt = nil
                let failure = error as NSError?
                let transient = failure?.domain == NSURLErrorDomain &&
                    [NSURLErrorNetworkConnectionLost, NSURLErrorTimedOut, NSURLErrorCannotConnectToHost].contains(failure?.code ?? 0)
                if let error, transient, number == 0, deadline.timeIntervalSinceNow > self.retryDelay + 1 {
                    self.onRetry?(1, error)
                    let work = DispatchWorkItem { [weak self] in
                        self?.attempt(original, deadline: deadline, number: 1, completion: completion)
                    }
                    self.retry = work
                    self.queue.asyncAfter(deadline: .now() + self.retryDelay, execute: work)
                    return
                }
                self.completed = true
                completion(data, response, error)
            }
        }
    }

    private static func sendRequest(_ request: URLRequest, completion: @escaping Reply) -> (() -> Void) {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = request.timeoutInterval
        configuration.timeoutIntervalForResource = request.timeoutInterval
        let session = ModemDeckDiagnostics.urlSession(configuration: configuration)
        let task = session.diagnosticDataTask(with: request) { data, response, error in
            completion(data, response, error)
            session.finishTasksAndInvalidate()
        }
        task.resume()
        return { session.invalidateAndCancel() }
    }
}
