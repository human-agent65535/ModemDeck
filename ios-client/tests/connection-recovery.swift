import Foundation

@MainActor
private func expect(_ condition: @autoclosure () -> Bool, _ message: String) {
    precondition(condition(), message)
}

@MainActor
private func eventually(_ condition: () -> Bool) async {
    for _ in 0..<200 {
        if condition() { return }
        try? await Task.sleep(nanoseconds: 2_000_000)
    }
    preconditionFailure("Timed out waiting for connection recovery")
}

private actor DelayRecorder {
    var values: [UInt64] = []
    func sleep(_ value: UInt64) async throws {
        values.append(value)
        try await Task.sleep(nanoseconds: 1_000_000)
    }
}

@main
private struct ConnectionRecoveryTests {
    @MainActor static func main() async {
        await retriesWithoutUserInteraction()
        await coalescesConcurrentChecks()
        await suspendsAndResumes()
        await ignoresLateRevokedResults()
        await pathRecoveryBypassesBackoff()
        await stopResultDoesNotRetry()
        print("6 connection recovery behavior tests passed")
    }

    @MainActor static func retriesWithoutUserInteraction() async {
        var probes = 0
        let delays = DelayRecorder()
        let recovery = ModemDeckConnectionRecovery(sleep: { try await delays.sleep($0) }) {
            probes += 1
            return probes < 7 ? .retry : .online
        }
        recovery.enable()
        await recovery.refresh()
        await eventually { probes == 7 }
        let recorded = await delays.values
        expect(recorded == [1, 2, 5, 10, 30, 30].map { UInt64($0) * 1_000_000_000 }, "Backoff must cap at 30 seconds")
        try? await Task.sleep(nanoseconds: 10_000_000)
        expect(probes == 7, "A successful recovery must stop polling")
        recovery.stop()
    }

    @MainActor static func coalescesConcurrentChecks() async {
        var probes = 0
        let recovery = ModemDeckConnectionRecovery {
            probes += 1
            try? await Task.sleep(nanoseconds: 20_000_000)
            return .online
        }
        recovery.enable()
        async let first: () = recovery.refresh()
        async let second: () = recovery.refresh()
        async let third: () = recovery.refresh()
        _ = await (first, second, third)
        expect(probes == 1, "Page success, resume and retry must share one check")
        recovery.stop()
    }

    @MainActor static func suspendsAndResumes() async {
        var probes = 0
        let recovery = ModemDeckConnectionRecovery(delays: [20_000_000]) {
            probes += 1
            return probes == 1 ? .retry : .online
        }
        recovery.enable()
        await recovery.refresh()
        recovery.setForeground(false)
        recovery.requestFailed()
        recovery.connectionMayBeAvailable()
        try? await Task.sleep(nanoseconds: 40_000_000)
        expect(probes == 1, "There must be no UI polling in the background")
        recovery.setForeground(true)
        await recovery.refresh()
        expect(probes == 2, "Returning to the foreground probes immediately")
        recovery.stop()
    }

    @MainActor static func ignoresLateRevokedResults() async {
        var probes = 0
        var completeOldProbe: CheckedContinuation<ModemDeckConnectionRecovery.Result, Never>?
        let recovery = ModemDeckConnectionRecovery(delays: [1_000_000]) {
            probes += 1
            if probes == 1 { return await withCheckedContinuation { completeOldProbe = $0 } }
            return .online
        }
        recovery.enable()
        let old = Task { await recovery.refresh() }
        await eventually { completeOldProbe != nil }
        recovery.stop()
        recovery.enable()
        await recovery.refresh()
        completeOldProbe?.resume(returning: .retry)
        await old.value
        try? await Task.sleep(nanoseconds: 10_000_000)
        expect(probes == 2, "A late request from the previous pairing must not restart recovery")
        recovery.stop()
    }

    @MainActor static func pathRecoveryBypassesBackoff() async {
        var probes = 0
        let recovery = ModemDeckConnectionRecovery(delays: [2_000_000_000]) {
            probes += 1
            return probes == 1 ? .retry : .online
        }
        recovery.enable()
        await recovery.refresh()
        recovery.connectionMayBeAvailable()
        await eventually { probes == 2 }
        recovery.stop()
    }

    @MainActor static func stopResultDoesNotRetry() async {
        var probes = 0
        let recovery = ModemDeckConnectionRecovery(delays: [1_000_000]) {
            probes += 1
            return .stop
        }
        recovery.enable()
        await recovery.refresh()
        recovery.requestFailed()
        recovery.connectionMayBeAvailable()
        try? await Task.sleep(nanoseconds: 10_000_000)
        expect(probes == 1 && !recovery.enabled, "Revoked credentials require pairing, not network retries")
    }
}
