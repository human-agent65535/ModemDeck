import Foundation

/// One foreground owner for connection probes. It never retries a business write.
@MainActor
final class ModemDeckConnectionRecovery {
    enum Result: Sendable { case online, retry, stop }
    typealias Probe = @MainActor () async -> Result
    typealias Sleep = @Sendable (UInt64) async throws -> Void

    private let probe: Probe
    private let sleep: Sleep
    private let delays: [UInt64]
    private var check: Task<Result, Never>?
    private var retry: Task<Void, Never>?
    private var generation = 0
    private var attempt = 0
    private(set) var enabled = false
    private(set) var foreground = true

    init(
        delays: [UInt64] = [1, 2, 5, 10, 30].map { UInt64($0) * 1_000_000_000 },
        sleep: @escaping Sleep = { try await Task.sleep(nanoseconds: $0) },
        probe: @escaping Probe
    ) {
        precondition(!delays.isEmpty)
        self.delays = delays
        self.sleep = sleep
        self.probe = probe
    }

    func enable() { enabled = true }

    func setForeground(_ value: Bool) {
        foreground = value
        if !value { cancelWork() }
    }

    func stop() {
        enabled = false
        attempt = 0
        cancelWork()
    }

    func refresh() async {
        guard enabled, foreground else { return }
        if let check {
            _ = await check.value
            return
        }
        retry?.cancel()
        retry = nil
        let current = generation
        let operation = Task { await probe() }
        check = operation
        let result = await operation.value
        guard current == generation, enabled, foreground else { return }
        check = nil
        switch result {
        case .online:
            attempt = 0
        case .retry:
            scheduleRetry()
        case .stop:
            stop()
        }
    }

    func requestFailed() {
        guard check == nil else { return }
        scheduleRetry()
    }

    /// A path change or a successful page request prompts a real server probe;
    /// neither is proof that the authenticated session has recovered.
    func connectionMayBeAvailable() {
        guard enabled, foreground, check == nil else { return }
        Task { [weak self] in await self?.refresh() }
    }

    private func scheduleRetry() {
        guard enabled, foreground, retry == nil else { return }
        let delay = delays[min(attempt, delays.count - 1)]
        attempt += 1
        let current = generation
        let sleep = sleep
        retry = Task { [weak self] in
            do { try await sleep(delay) } catch { return }
            guard !Task.isCancelled, let self,
                  current == self.generation, self.enabled, self.foreground else { return }
            self.retry = nil
            await self.refresh()
        }
    }

    private func cancelWork() {
        generation += 1
        retry?.cancel()
        retry = nil
        check?.cancel()
        check = nil
    }
}
