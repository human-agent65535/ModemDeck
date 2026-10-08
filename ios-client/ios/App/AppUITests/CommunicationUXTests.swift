import XCTest
import UIKit

/// Run only against scripts/uat-mock-server.mjs --mutable on loopback. All
/// identities and operations below are disposable synthetic fixture data.
@MainActor
final class CommunicationUXTests: XCTestCase {
    private let server = "https://127.0.0.1:18943"
    private let token = "md_ios_" + String(repeating: "A", count: 43)
    private let threadID = "uat-line-a:+12025550101"

    private func fixture(_ path: String, body: [String: Any]? = nil, method: String = "POST") async throws -> [String: Any] {
        var request = URLRequest(url: URL(string: server + path)!)
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        if let body {
            request.httpMethod = method
            request.httpBody = try JSONSerialization.data(withJSONObject: body)
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let (data, response) = try await URLSession.shared.data(for: request)
        XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
        return (try JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
    }

    private func launch(configuration: [String: Any] = [:], callSurface: Bool = false, testAudio: Bool = false, waitForActivity: Bool = true, environment: [String: String] = [:]) async throws -> XCUIApplication {
        continueAfterFailure = false
        _ = try await fixture("/__uat/reset", body: [:])
        if !configuration.isEmpty {
            _ = try await fixture("/__uat/configure", body: configuration)
        }
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchEnvironment = [
            "MODEMDECK_UAT_MODE": "1", "MODEMDECK_UAT_SERVER_URL": server,
            "MODEMDECK_UAT_TOKEN": token, "MODEMDECK_UAT_INITIAL_SECTION": "home"
        ]
        app.launchEnvironment.merge(environment) { _, next in next }
        if callSurface {
            app.launchEnvironment["MODEMDECK_UAT_CALL_STATE"] = "active"
            app.launchEnvironment["MODEMDECK_UAT_TEST_AUDIO_DYNAMIC"] = "1"
        }
        if testAudio { app.launchEnvironment["MODEMDECK_UAT_TEST_AUDIO"] = "1" }
        app.launch()
        if !callSurface && waitForActivity { XCTAssertTrue(app.buttons["activity-message-\(threadID)"].waitForExistence(timeout: 20)) }
        return app
    }

    func testSlowHistorySyncDoesNotDisableControls() async throws {
        let app = try await launch(configuration: ["historyDelayMS": 30000], waitForActivity: false)
        XCTAssertTrue(app.buttons["section-messages"].waitForExistence(timeout: 5))
        app.buttons["section-messages"].tap()
        let compose = app.buttons["新建短信"]
        XCTAssertTrue(compose.waitForExistence(timeout: 5))
        let ready = NSPredicate { _, _ in compose.isEnabled }
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: ready, object: compose)], timeout: 5)
        let state = try await fixture("/__uat/state")
        XCTAssertGreaterThan(state["historyReadsStarted"] as? Int ?? 0, 0)
        XCTAssertEqual(state["historyReadsCompleted"] as? Int, 0, "Controls must be usable before slow history completes")
        compose.tap()
        XCTAssertTrue(app.textFields["电话号码"].waitForExistence(timeout: 3))
        capture("startup-during-history-sync", app: app)
    }

    func testLargeCachedHistoryKeepsLaunchAndTabsResponsive() async throws {
        let app = try await launch(configuration: ["largeHistory": true])
        let loaded = NSPredicate { _, _ in app.buttons["recent-calls-all"].label.contains("3,002") || app.buttons["recent-calls-all"].label.contains("3002") }
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: loaded, object: app)], timeout: 20)
        // Relaunch with thousands of cached rows across 900 days, then exercise
        // both fresh-cache rendering and a tab switch rather than timing a splash.
        app.terminate()
        let started = Date()
        app.launch()
        XCTAssertTrue(app.buttons["section-settings"].waitForExistence(timeout: 5))
        app.buttons["section-settings"].tap()
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "偏好设置")).firstMatch.waitForExistence(timeout: 3))
        XCTAssertLessThan(Date().timeIntervalSince(started), 10, "Cached history must not monopolize the UI thread at launch")
        app.buttons["section-home"].tap()
        XCTAssertTrue(app.buttons["activity-message-\(threadID)"].waitForExistence(timeout: 5))
        capture("startup-large-cached-history", app: app)
    }

    func testSwitchingTabsClosesLeadingAndTrailingSwipeActions() async throws {
        let app = try await launch()
        for section in ["home", "messages", "calls", "contacts"] {
            app.buttons["section-" + section].tap()
            let id = ["home": "activity-message-\(threadID)", "messages": "message-\(threadID)",
                      "calls": "call-uat-call-missed", "contacts": "contact-uat-contact-example"][section]!
            let row = app.buttons[id]
            XCTAssertTrue(row.waitForExistence(timeout: 5))
            let restingY = row.frame.minY
            row.swipeLeft()
            XCTAssertTrue(app.buttons["删除"].waitForExistence(timeout: 3))
            app.buttons["section-settings"].tap()
            app.buttons["section-" + section].tap()
            XCTAssertFalse(app.buttons["删除"].exists, "\(section) retained its trailing swipe")
            XCTAssertEqual(row.frame.minY, restingY, accuracy: 2, "Switching tabs must retain the list position")
            if section != "contacts" {
                let start = row.coordinate(withNormalizedOffset: CGVector(dx: 0.15, dy: 0.5))
                let end = row.coordinate(withNormalizedOffset: CGVector(dx: 0.45, dy: 0.5))
                start.press(forDuration: 0.05, thenDragTo: end)
                XCTAssertTrue(app.buttons["标为已读"].waitForExistence(timeout: 3))
                app.buttons["section-settings"].tap()
                app.buttons["section-" + section].tap()
                XCTAssertFalse(app.buttons["标为已读"].exists, "\(section) retained its leading swipe")
            }
        }
    }

    private func capture(_ name: String, app: XCUIApplication) {
        // Full-screen capture preserves the iPad compositor's orientation and
        // window bounds; app-region snapshots can crop after device rotation.
        let screenshot = UIDevice.current.userInterfaceIdiom == .pad
            ? XCUIScreen.main.screenshot() : app.screenshot()
        let attachment = XCTAttachment(screenshot: screenshot)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    func testDeviceDiagnosticsConsentPersistsAndUploads() async throws {
        XCUIDevice.shared.orientation = UIDevice.current.userInterfaceIdiom == .pad ? .landscapeLeft : .portrait
        defer { XCUIDevice.shared.orientation = .portrait }
        _ = try await fixture("/__uat/reset", body: [:])
        let app = XCUIApplication()
        app.launchEnvironment = ["MODEMDECK_UAT_MODE": "1", "MODEMDECK_UAT_SERVER_URL": server,
            "MODEMDECK_UAT_TOKEN": token, "MODEMDECK_UAT_INITIAL_SECTION": "home",
            "MODEMDECK_UAT_DIAGNOSTICS_RESET": "1"]
        app.launch()
        func openDiagnostics() {
            XCTAssertTrue(app.buttons["section-settings"].waitForExistence(timeout: 15))
            app.buttons["section-settings"].tap()
            let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "手机诊断")).firstMatch
            XCTAssertTrue(row.waitForExistence(timeout: 5))
            row.tap()
            XCTAssertTrue(app.switches["diagnostics-upload-toggle"].waitForExistence(timeout: 5))
        }
        openDiagnostics()
        let toggle = app.switches["diagnostics-upload-toggle"]
        XCTAssertEqual(toggle.value as? String, "0")
        let before = try await fixture("/__uat/state")
        XCTAssertTrue((before["diagnosticBatches"] as? [Any] ?? []).isEmpty)
        capture("device-diagnostics-off", app: app)
        toggle.tap()
        XCTAssertEqual(toggle.value as? String, "1")
        app.launchEnvironment.removeValue(forKey: "MODEMDECK_UAT_DIAGNOSTICS_RESET")
        app.terminate()
        app.launch()
        openDiagnostics()
        XCTAssertEqual(toggle.value as? String, "1", "Consent must survive relaunch")
        var batches: [[String: Any]] = []
        for _ in 0..<30 {
            batches = (try await fixture("/__uat/state"))["diagnosticBatches"] as? [[String: Any]] ?? []
            if batches.flatMap({ $0["events"] as? [[String: Any]] ?? [] }).contains(where: { $0["category"] as? String == "api" }) { break }
            try await Task.sleep(nanoseconds: 300_000_000)
        }
        let events = batches.flatMap { $0["events"] as? [[String: Any]] ?? [] }
        XCTAssertTrue(events.contains { $0["category"] as? String == "app" })
        XCTAssertTrue(events.contains { $0["category"] as? String == "api" }, "Actual URLSession results must reach the collector")
        let uploaded = String(data: try JSONSerialization.data(withJSONObject: batches), encoding: .utf8)!
        for secret in [token, "+12025550101", "示例联系人", "第三条未读 UAT 消息"] {
            XCTAssertFalse(uploaded.contains(secret), "Diagnostic upload leaked communication data")
        }
        capture("device-diagnostics-on", app: app)
        toggle.tap()
        XCTAssertEqual(toggle.value as? String, "0")
        try await Task.sleep(nanoseconds: 500_000_000)
        let countAfterOff = ((try await fixture("/__uat/state"))["diagnosticBatches"] as? [Any] ?? []).count
        app.terminate()
        app.launch()
        openDiagnostics()
        XCTAssertEqual(toggle.value as? String, "0")
        try await Task.sleep(nanoseconds: 2_500_000_000)
        let after = try await fixture("/__uat/state")
        XCTAssertEqual((after["diagnosticBatches"] as? [Any] ?? []).count, countAfterOff,
                       "Disabled diagnostics must not upload new foreground/API events")
    }

    func testCompactCollectionHeadersKeepSearchAndFiltersUsable() async throws {
        let app = try await launch()
        func appearance(_ value: String) {
            app.buttons["section-settings"].tap()
            app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "偏好设置")).firstMatch.tap()
            app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "显示模式")).firstMatch.tap()
            app.buttons[value].tap()
        }
        appearance("浅色")
        app.buttons["section-messages"].tap()
        let row = app.buttons["message-\(threadID)"]
        let service = app.buttons["message-uat-line-a:UAT-SERVICE"]
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        let searchButton = app.buttons["collection-search-open"]
        let filters = app.buttons["collection-filters"]
        XCTAssertLessThanOrEqual(row.frame.minY - searchButton.frame.minY, 110)
        XCTAssertFalse(app.textFields["搜索"].exists, "The default header must not reserve a search row")
        capture("compact-messages-light", app: app)
        app.buttons["未读"].tap()
        filters.tap()
        app.buttons["仅收藏"].tap()
        XCTAssertTrue((filters.value as? String)?.contains("1") == true)
        filters.tap()
        if !app.buttons["Line A"].exists { app.buttons["线路"].tap() }
        app.buttons["Line A"].tap()
        XCTAssertTrue((filters.value as? String)?.contains("2") == true)
        searchButton.tap()
        let query = app.textFields["搜索"]
        XCTAssertTrue(query.waitForExistence(timeout: 3))
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 3), "Search should focus in one tap")
        query.typeText("示例")
        XCTAssertTrue(row.exists)
        XCTAssertFalse(service.exists)
        capture("compact-messages-search", app: app)
        app.buttons["section-contacts"].tap()
        let contact = app.buttons["contact-uat-contact-example"]
        XCTAssertTrue(contact.waitForExistence(timeout: 5))
        XCTAssertLessThanOrEqual(contact.frame.minY - searchButton.frame.minY, 62)
        XCTAssertTrue(app.buttons["新建联系人"].isHittable)
        capture("compact-contacts-light", app: app)
        app.buttons["section-messages"].tap()
        XCTAssertEqual(query.value as? String, "示例")
        XCTAssertFalse(app.keyboards.firstMatch.exists, "Returning to the tab must not reopen the keyboard")
        app.buttons["collection-search-cancel"].tap()
        XCTAssertTrue(app.buttons["未读"].isSelected)
        XCTAssertTrue((filters.value as? String)?.contains("2") == true)
        filters.tap()
        app.buttons["清除筛选"].tap()
        XCTAssertFalse(filters.isSelected)
        app.buttons["全部"].tap()
        XCTAssertTrue(service.waitForExistence(timeout: 3))
        app.buttons["section-calls"].tap()
        let call = app.buttons["call-uat-call-example"]
        XCTAssertTrue(call.waitForExistence(timeout: 5))
        XCTAssertLessThanOrEqual(call.frame.minY - searchButton.frame.minY, 110)
        app.buttons["有录音"].tap()
        XCTAssertTrue(call.exists)
        XCTAssertFalse(app.buttons["call-uat-call-missed"].exists)
        capture("compact-calls-light", app: app)
        app.buttons["更多操作"].tap()
        app.buttons["录音管理"].tap()
        XCTAssertTrue(app.buttons["recording-uat-recording-example"].waitForExistence(timeout: 4))
        XCTAssertTrue(searchButton.isHittable)
        capture("compact-recordings-light", app: app)
        app.buttons["返回通话"].tap()
        // The settings tab retains its detail navigation between visits.
        app.buttons["section-settings"].tap()
        app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "显示模式")).firstMatch.tap()
        app.buttons["深色"].tap()
        app.buttons["section-messages"].tap()
        capture("compact-messages-dark", app: app)
        if UIDevice.current.userInterfaceIdiom == .pad {
            XCUIDevice.shared.orientation = .landscapeLeft
            XCTAssertTrue(row.waitForExistence(timeout: 4))
            capture("compact-messages-ipad-landscape", app: app)
        }
    }

    private func backgroundBrightness(app: XCUIApplication) -> Double {
        let screenshot = app.screenshot().image.cgImage!
        let area = CGRect(x: CGFloat(screenshot.width) * 0.015, y: CGFloat(screenshot.height) * 0.7, width: 4, height: 4)
        let sample = screenshot.cropping(to: area)!
        var pixel = [UInt8](repeating: 0, count: 4)
        let context = CGContext(data: &pixel, width: 1, height: 1, bitsPerComponent: 8, bytesPerRow: 4,
            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
        context.draw(sample, in: CGRect(x: 0, y: 0, width: 1, height: 1))
        return Double(Int(pixel[0]) + Int(pixel[1]) + Int(pixel[2])) / (3 * 255)
    }

    func testRefreshDesignFiltersAndDialerDraft() async throws {
        let app = try await launch()
        XCTAssertFalse(app.buttons["section-recordings"].exists)
        capture("refresh-recents-light", app: app)
        app.buttons["recent-calls-recorded"].tap()
        let recorded = app.buttons["call-uat-call-example"]
        let missed = app.buttons["call-uat-call-missed"]
        XCTAssertTrue(recorded.waitForExistence(timeout: 5))
        XCTAssertFalse(missed.exists)
        capture("refresh-calls-recorded", app: app)
        app.buttons["未接"].tap()
        XCTAssertTrue(missed.waitForExistence(timeout: 3))
        XCTAssertFalse(recorded.exists)
        app.buttons["全部"].tap()
        XCTAssertTrue(recorded.exists && missed.exists)
        // A quick horizontal swipe must expose row actions, not start selection.
        recorded.swipeLeft()
        XCTAssertTrue(app.buttons["删除"].waitForExistence(timeout: 3))
        recorded.swipeRight()
        recorded.press(forDuration: 0.65)
        XCTAssertTrue(app.staticTexts["已选择 1 项"].waitForExistence(timeout: 3))
        app.buttons["复制"].tap()
        XCTAssertTrue(app.buttons["复制号码"].waitForExistence(timeout: 3))
        app.buttons["复制号码"].tap()
        capture("refresh-calls-selection", app: app)
        app.buttons["完成"].tap()
        app.buttons["更多操作"].tap()
        app.buttons["录音管理"].tap()
        XCTAssertTrue(app.buttons["recording-uat-recording-example"].waitForExistence(timeout: 4))
        app.buttons["返回通话"].tap()
        app.buttons["打开拨号盘"].firstMatch.tap()
        XCTAssertTrue(app.buttons["dial-key-1"].waitForExistence(timeout: 3))
        for key in ["1", "2", "0", "2", "5", "5", "5", "0", "1", "0", "1"] { app.buttons["dial-key-" + key].tap() }
        let number = app.textFields["dial-number"]
        XCTAssertEqual(number.value as? String, "12025550101")
        capture("refresh-dialer-draft", app: app)
        app.buttons["dialer-collapse"].tap()
        app.buttons["section-messages"].tap()
        app.buttons["打开拨号盘"].firstMatch.tap()
        XCTAssertTrue(number.waitForExistence(timeout: 3))
        XCTAssertEqual(number.value as? String, "12025550101")
        app.buttons["dial-delete"].tap()
        XCTAssertEqual(number.value as? String, "1202555010")
        app.buttons["dialer-collapse"].tap()
        app.buttons["section-settings"].tap()
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "偏好设置")).firstMatch.tap()
        app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "显示模式")).firstMatch.tap()
        app.buttons["深色"].tap()
        capture("refresh-settings-dark", app: app)
        app.buttons["section-messages"].tap()
        try await Task.sleep(nanoseconds: 300_000_000)
        XCTAssertLessThan(backgroundBrightness(app: app), 0.3, "The shell must actually adopt the selected dark appearance")
        capture("refresh-messages-dark", app: app)
        app.buttons["section-contacts"].tap()
        capture("refresh-contacts-dark", app: app)
        app.buttons["section-calls"].tap()
        capture("refresh-calls-dark", app: app)
        app.buttons["section-home"].tap()
        capture("refresh-recents-dark", app: app)
        app.buttons["打开拨号盘"].firstMatch.tap()
        capture("refresh-dialer-dark", app: app)
    }

    func testActiveCallCanCollapseAndRestoreWithoutLosingKeypad() async throws {
        let app = try await launch(callSurface: true)
        XCTAssertTrue(app.buttons["call-collapse"].waitForExistence(timeout: 12))
        app.buttons["键盘"].tap()
        try await Task.sleep(nanoseconds: 400_000_000)
        capture("refresh-active-call-keypad", app: app)
        app.buttons["call-collapse"].tap()
        XCTAssertTrue(app.buttons["call-restore"].waitForExistence(timeout: 4))
        app.buttons["section-messages"].tap()
        XCTAssertTrue(app.buttons["message-\(threadID)"].waitForExistence(timeout: 6))
        capture("refresh-mini-call", app: app)
        app.buttons["call-restore"].tap()
        XCTAssertTrue(app.buttons["隐藏键盘"].waitForExistence(timeout: 4))
        XCTAssertTrue(app.buttons["call-collapse"].exists)
    }

    func testCallAudioControlsAndGuidanceRemainVisible() async throws {
        var app = try await launch(callSurface: true)
        XCTAssertTrue(app.buttons["call-speaker"].waitForExistence(timeout: 12))
        capture("call-audio-controls", app: app)
        app.buttons["call-collapse"].tap()
        XCTAssertTrue(app.buttons["call-restore"].waitForExistence(timeout: 4))
        app.buttons["call-restore"].tap()
        XCTAssertTrue(app.buttons["call-speaker"].waitForExistence(timeout: 4))
        app.terminate()
        app = try await launch(callSurface: true, testAudio: true)
        XCTAssertTrue(app.staticTexts["请说几句话 · 3s"].waitForExistence(timeout: 12))
        XCTAssertTrue(app.progressIndicators["收音电平"].exists)
        XCTAssertTrue(app.buttons["call-speaker"].exists)
        XCTAssertTrue(app.buttons["挂断"].isHittable)
        capture("call-test-guidance-portrait", app: app)
        XCUIDevice.shared.orientation = .landscapeLeft
        try await Task.sleep(nanoseconds: 500_000_000)
        XCTAssertTrue(app.buttons["挂断"].isHittable)
        XCTAssertTrue(app.buttons["call-collapse"].isHittable)
        capture("call-test-guidance-landscape", app: app)
    }

    func testCallEndFeedbackAndExplicitTestResult() async throws {
        for test in [false, true] {
            let app = try await launch(callSurface: true, testAudio: test)
            let end = app.buttons["call-end"]
            XCTAssertTrue(end.waitForExistence(timeout: 8))
            end.tap()
            let result = app.staticTexts["call-ended-status"]
            // tap() already waits for app quiescence. A predicate wait polls at
            // one-second intervals and can miss the intentional one-second hold.
            XCTAssertTrue(result.exists, "Ending must provide feedback before the active surface disappears")
            capture(test ? "call-test-ended-result" : "call-ended-confirmed-feedback", app: app)
            XCTAssertFalse(app.buttons["call-mute"].exists)
            if test {
                let duration = app.staticTexts["call-ended-duration"].label
                try await Task.sleep(nanoseconds: 1_400_000_000)
                XCTAssertTrue(result.exists, "Test result requires explicit completion")
                XCTAssertEqual(app.staticTexts["call-ended-duration"].label, duration, "Ended duration must remain frozen")
                app.buttons["call-ended-done"].tap()
            }
            let gone = NSPredicate { _, _ in !result.exists }
            await fulfillment(of: [XCTNSPredicateExpectation(predicate: gone, object: result)], timeout: 5)
            app.terminate()
        }
    }

    func testPendingAndCollapsedCallEndKeepTheirPresentation() async throws {
        var app = try await launch(callSurface: true, environment: ["MODEMDECK_UAT_END_PENDING": "1"])
        XCTAssertTrue(app.buttons["call-end"].waitForExistence(timeout: 8))
        app.buttons["call-end"].tap()
        let status = app.staticTexts["call-ended-status"]
        XCTAssertTrue(status.waitForExistence(timeout: 2))
        XCTAssertTrue(status.label.contains("确认"))
        let duration = app.staticTexts["call-ended-duration"].label
        capture("call-ended-pending-server-confirmation", app: app)
        try await Task.sleep(nanoseconds: 1_400_000_000)
        XCTAssertTrue(status.exists)
        XCTAssertEqual(app.staticTexts["call-ended-duration"].label, duration)
        app.buttons["call-ended-done"].tap()
        app.terminate()

        app = try await launch(callSurface: true, testAudio: true)
        XCTAssertTrue(app.buttons["call-collapse"].waitForExistence(timeout: 8))
        app.buttons["call-collapse"].tap()
        XCTAssertTrue(app.buttons["call-restore"].waitForExistence(timeout: 2))
        app.buttons["call-mini-end"].tap()
        XCTAssertTrue(app.buttons["call-mini-end"].waitForExistence(timeout: 2))
        XCTAssertEqual(app.buttons["call-mini-end"].label, "关闭结束结果")
        XCTAssertFalse(app.staticTexts["call-ended-status"].isHittable, "Collapsed call must not expand just because it ended")
        capture("call-ended-collapsed-result", app: app)
        app.buttons["call-mini-end"].tap()
    }

    func testReducedMotionEndedResultClosesWithoutAnimation() async throws {
        XCTAssertTrue(UIAccessibility.isReduceMotionEnabled, "Run this case with the disposable simulator's Reduce Motion setting enabled")
        let app = try await launch(callSurface: true, testAudio: true)
        XCTAssertTrue(app.buttons["call-end"].waitForExistence(timeout: 8))
        app.buttons["call-end"].tap()
        XCTAssertTrue(app.staticTexts["call-ended-status"].waitForExistence(timeout: 2))
        capture("call-test-ended-reduced-motion", app: app)
        app.buttons["call-ended-done"].tap()
        // XCTest snapshots are asynchronous; immediate model dismissal is
        // separately covered by the production-method Reduce Motion regression.
        XCTAssertTrue(app.staticTexts["call-ended-status"].waitForNonExistence(timeout: 3))
    }

    func testCallPrimaryActionsStaySymmetricAndAvoidAnswerToEndOverlap() throws {
        continueAfterFailure = false
        let app = XCUIApplication()
        XCUIDevice.shared.orientation = .portrait
        for largeText in [false, true] {
            var realCallEndFrame: CGRect?
            for testCall in [false, true] {
                func launchVisualCall(_ state: String) {
                    app.terminate()
                    app.launchEnvironment = [
                        "MODEMDECK_UAT_MODE": "1",
                        "MODEMDECK_UAT_SERVER_URL": "https://127.0.0.1:1",
                        "MODEMDECK_UAT_TOKEN": token,
                        "MODEMDECK_UAT_CALL_STATE": state,
                        "MODEMDECK_UAT_TEST_AUDIO_DYNAMIC": "1",
                        "MODEMDECK_UAT_TEST_AUDIO": testCall ? "1" : "0"
                    ]
                    app.launchArguments = largeText
                        ? ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"] : []
                    app.launch()
                }
                launchVisualCall("ringing")
                let answer = app.buttons["call-answer"], decline = app.buttons["call-decline"]
                XCTAssertTrue(answer.waitForExistence(timeout: 12))
                XCTAssertTrue(answer.isHittable && decline.isHittable)
                XCTAssertFalse(app.buttons["call-recording"].exists)
                XCTAssertEqual((answer.frame.midX + decline.frame.midX) / 2, app.frame.midX, accuracy: 2)
                XCTAssertEqual(answer.frame.minY, decline.frame.minY, accuracy: 2)
                XCTAssertGreaterThanOrEqual(answer.frame.width, 44)
                XCTAssertGreaterThanOrEqual(answer.frame.height, 44)
                XCTAssertLessThan(decline.frame.maxX, answer.frame.minX)
                let answerFrame = answer.frame
                capture("call-incoming-\(testCall ? "test" : "real")-\(largeText ? "accessibility" : "default")", app: app)

                launchVisualCall("active")
                let end = app.buttons["call-end"]
                XCTAssertTrue(end.waitForExistence(timeout: 12))
                XCTAssertTrue(end.isHittable)
                XCTAssertEqual(end.frame.midX, app.frame.midX, accuracy: 2)
                XCTAssertFalse(end.frame.intersects(answerFrame), "A second tap at Answer must not hit End")
                XCTAssertTrue(app.frame.contains(end.frame), "Primary actions must remain inside the screen")
                if let realCallEndFrame {
                    XCTAssertEqual(end.frame.minY, realCallEndFrame.minY, accuracy: 2, "Test calls share the real-call primary action region")
                    XCTAssertEqual(end.frame.width, realCallEndFrame.width, accuracy: 2)
                } else { realCallEndFrame = end.frame }
                if !testCall {
                    let keypad = app.buttons["call-keypad"]
                    for _ in 0..<3 where !keypad.isHittable { app.scrollViews.firstMatch.swipeUp() }
                    XCTAssertTrue(keypad.isHittable, "Auxiliary controls must remain reachable at larger text sizes")
                    XCTAssertTrue(app.buttons["call-recording"].exists)
                    XCTAssertLessThan(keypad.frame.maxY, end.frame.minY)
                    XCTAssertLessThan(app.buttons["call-recording"].frame.maxY, end.frame.minY)
                }
                capture("call-active-\(testCall ? "test" : "real")-\(largeText ? "accessibility" : "default")", app: app)
            }
        }
        app.terminate()
    }

    func testRotationPreservesConversationAndDraft() async throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad)
        let app = try await launch()
        app.buttons["section-messages"].tap()
        app.buttons["message-\(threadID)"].tap()
        let composer = app.textFields["短信内容"]
        XCTAssertTrue(composer.waitForExistence(timeout: 6))
        composer.tap()
        composer.typeText("KEEP_THIS_UNSENT_DRAFT")
        let draft = app.descendants(matching: .any).matching(
            NSPredicate(format: "value CONTAINS %@", "KEEP_THIS_UNSENT_DRAFT")
        ).firstMatch
        XCTAssertTrue(draft.exists)
        for orientation in [UIDeviceOrientation.landscapeLeft, .portrait] {
            XCUIDevice.shared.orientation = orientation
            let expectedLandscape = orientation == .landscapeLeft
            let rotated = NSPredicate { _, _ in (app.frame.width > app.frame.height) == expectedLandscape }
            await fulfillment(of: [XCTNSPredicateExpectation(predicate: rotated, object: app)], timeout: 8)
            XCTAssertTrue(draft.waitForExistence(timeout: 5), "Rotation must retain the visible conversation and draft")
            XCTAssertFalse(app.staticTexts["选择会话"].exists)
            capture(expectedLandscape ? "fixed-draft-landscape" : "fixed-draft-portrait", app: app)
        }
    }

    func testSplitConversationSurvivesCompactRotation() async throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad)
        let app = try await launch()
        XCUIDevice.shared.orientation = .landscapeLeft
        let landscape = NSPredicate { _, _ in app.frame.width > app.frame.height }
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: landscape, object: app)], timeout: 8)
        app.buttons["section-messages"].tap()
        app.buttons["message-\(threadID)"].tap()
        let composer = app.textFields["短信内容"]
        XCTAssertTrue(composer.waitForExistence(timeout: 6))
        composer.tap()
        composer.typeText("SPLIT_DRAFT")
        XCUIDevice.shared.orientation = .portrait
        let portrait = NSPredicate { _, _ in app.frame.width < app.frame.height }
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: portrait, object: app)], timeout: 8)
        XCTAssertTrue(app.descendants(matching: .any).matching(
            NSPredicate(format: "value CONTAINS %@", "SPLIT_DRAFT")
        ).firstMatch.waitForExistence(timeout: 6))
        capture("fixed-split-to-compact-draft", app: app)
    }

    func testContactBatchDeleteKeepsItsOriginalSelection() async throws {
        let app = try await launch(configuration: ["secondContact": true, "contactDeleteDelayMS": 8000])
        app.buttons["section-contacts"].tap()
        let original = app.buttons["contact-uat-contact-example"]
        let other = app.buttons["contact-uat-contact-second"]
        XCTAssertTrue(original.waitForExistence(timeout: 6))
        original.press(forDuration: 0.65)
        XCTAssertTrue(app.staticTexts["已选择 1 项"].waitForExistence(timeout: 3))
        app.buttons["删除"].tap()
        app.alerts.buttons["删除"].tap()
        var started = false
        for _ in 0..<12 where !started {
            let state = try await fixture("/__uat/state")
            started = (state["operations"] as? [[String: Any]] ?? []).contains { $0["phase"] as? String == "started" }
            if !started { try await Task.sleep(nanoseconds: 100_000_000) }
        }
        XCTAssertTrue(started)
        XCTAssertFalse(original.isEnabled)
        XCTAssertFalse(other.isEnabled, "Selection must be frozen during the submitted deletion")
        app.buttons["section-settings"].tap()
        app.buttons["section-contacts"].tap()
        XCTAssertFalse(other.isEnabled, "Tab switches must not reset an in-flight batch operation")
        let gone = NSPredicate(format: "exists == false")
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: gone, object: original)], timeout: 12)
        XCTAssertTrue(other.exists)
        let state = try await fixture("/__uat/state")
        XCTAssertEqual((state["contacts"] as? [[String: Any]] ?? []).compactMap { $0["id"] as? String }, ["uat-contact-second"])
        capture("fixed-contact-delete-selection", app: app)
    }

    func testHomeSwipeCancelAndSharedReadState() async throws {
        let app = try await launch()
        let row = app.buttons["activity-message-\(threadID)"]
        capture("home-portrait", app: app)
        row.swipeLeft()
        app.buttons["删除"].firstMatch.tap()
        XCTAssertTrue(app.alerts["确认删除？"].waitForExistence(timeout: 3))
        app.alerts.buttons["取消"].tap()
        XCTAssertTrue(row.exists, "Cancelling deletion must leave the row in place")
        row.swipeRight()
        XCTAssertTrue(app.buttons["标为已读"].waitForExistence(timeout: 3))
        app.buttons["标为已读"].tap()
        let read = NSPredicate(format: "value CONTAINS %@", "已读")
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: read, object: row)], timeout: 8)
        app.buttons["section-messages"].tap()
        let messageRow = app.buttons["message-\(threadID)"]
        XCTAssertTrue(messageRow.waitForExistence(timeout: 5))
        XCTAssertTrue((messageRow.value as? String)?.contains("已读") == true)
        capture("messages-shared-read-state", app: app)
        let state = try await fixture("/__uat/state")
        let threads = state["threads"] as? [[String: Any]] ?? []
        XCTAssertEqual(threads.first?["unread_count"] as? Int, 0)
    }

    func testHomeQuickViewKeepsContextActionsAndModuleSelection() async throws {
        let app = try await launch()
        XCTAssertEqual(app.textFields.count, 0, "Home is a quick view, not a search workspace")
        for label in ["选择活动", "筛选线路", "仅收藏", "全部", "未读"] {
            XCTAssertFalse(app.buttons[label].exists, "Home must not show the management toolbar")
        }
        capture("home-quick-view", app: app)
        let message = app.buttons["activity-message-\(threadID)"]
        message.press(forDuration: 1)
        XCTAssertTrue(app.buttons["复制号码"].waitForExistence(timeout: 3))
        app.buttons["取消收藏"].tap()
        let notFavorite = NSPredicate(format: "NOT (value CONTAINS %@)", "已收藏")
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: notFavorite, object: message)], timeout: 5)
        app.buttons["section-messages"].tap()
        let messageRow = app.buttons["message-\(threadID)"]
        XCTAssertTrue(messageRow.waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["collection-search-open"].exists, "Search remains available in Messages")
        XCTAssertFalse((messageRow.value as? String)?.contains("已收藏") == true)
        messageRow.press(forDuration: 0.65)
        XCTAssertTrue(app.staticTexts["已选择 1 项"].waitForExistence(timeout: 3))
        app.buttons["message-uat-line-a:UAT-SERVICE"].tap()
        XCTAssertTrue(app.staticTexts["已选择 2 项"].exists)
        XCTAssertTrue(app.buttons["标为已读"].exists, "Mixed read state defaults to Mark Read")
        capture("messages-batch-selection", app: app)
        app.buttons["收藏"].tap()
        let favorite = NSPredicate(format: "value CONTAINS %@", "已收藏")
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: favorite, object: messageRow)], timeout: 8)
        app.buttons["完成"].tap()
        app.buttons["section-home"].tap()
        XCTAssertTrue(message.waitForExistence(timeout: 5))
        XCTAssertTrue((message.value as? String)?.contains("已收藏") == true)
        XCTAssertEqual(app.textFields.count, 0)
    }

    func testMessageConversationStartsAtUnreadAndAcknowledgesSnapshot() async throws {
        let app = try await launch(configuration: ["firstUnreadMessageID": 8])
        app.buttons["section-messages"].tap()
        let row = app.buttons["message-\(threadID)"]
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        row.tap()
        XCTAssertTrue(app.staticTexts["未读消息"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["未读消息"].isHittable, "The first unread marker must be visible without scrolling")
        XCTAssertTrue(app.staticTexts["历史 UAT 消息 8"].isHittable)
        XCTAssertLessThan(app.staticTexts["未读消息"].frame.minY - app.scrollViews.firstMatch.frame.minY, 40,
                          "The unread target must be aligned near the top of the conversation")
        capture("message-unread-anchor-and-day-groups", app: app)

        let conversation = app.scrollViews.firstMatch
        let dragStart = conversation.coordinate(withNormalizedOffset: CGVector(dx: 0.85, dy: 0.5))
        let dragEnd = conversation.coordinate(withNormalizedOffset: CGVector(dx: 0.58, dy: 0.5))
        dragStart.press(
            forDuration: 0.05,
            thenDragTo: dragEnd,
            withVelocity: .slow,
            thenHoldForDuration: 0.5
        )

        var readOperation: [String: Any]?
        for _ in 0..<20 where readOperation == nil {
            let state = try await fixture("/__uat/state")
            let operations = state["operations"] as? [[String: Any]] ?? []
            readOperation = operations.last {
                $0["pathname"] as? String == "/api/v1/messages/read"
            }
            if readOperation == nil { try await Task.sleep(nanoseconds: 250_000_000) }
        }
        XCTAssertEqual(readOperation?["through_message_id"] as? Int, 30)
    }

    func testMessageSwipeBackAfterSwitchingTabs() async throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone)
        guard #available(iOS 26.0, *) else {
            throw XCTSkip("Full-content interactive back requires iOS 26; edge back is covered separately")
        }
        let app = try await launch()
        app.buttons["section-messages"].tap()
        let row = app.buttons["message-\(threadID)"]
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        row.tap()
        let back = app.buttons["返回消息"]
        XCTAssertTrue(back.waitForExistence(timeout: 5))
        let conversation = app.scrollViews.firstMatch
        let leftStart = conversation.coordinate(withNormalizedOffset: CGVector(dx: 0.82, dy: 0.45))
        let leftEnd = conversation.coordinate(withNormalizedOffset: CGVector(dx: 0.42, dy: 0.45))
        leftStart.press(forDuration: 0.05, thenDragTo: leftEnd)
        XCTAssertTrue(back.exists, "Timestamp reveal must keep the conversation open")

        let contentStart = app.coordinate(withNormalizedOffset: CGVector(dx: 0.25, dy: 0.45))
        let contentEnd = app.coordinate(withNormalizedOffset: CGVector(dx: 0.92, dy: 0.45))
        let cancelledEnd = app.coordinate(withNormalizedOffset: CGVector(dx: 0.4, dy: 0.45))
        let restingBackX = back.frame.minX
        contentStart.press(forDuration: 0.05, thenDragTo: cancelledEnd, withVelocity: .slow, thenHoldForDuration: 0.4)
        XCTAssertTrue(back.exists, "Cancelling an interactive swipe must retain the conversation")
        let settled = NSPredicate { _, _ in abs(back.frame.minX - restingBackX) < 1 }
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: settled, object: back)], timeout: 3)
        contentStart.press(forDuration: 0.05, thenDragTo: contentEnd)
        let gone = NSPredicate(format: "exists == false")
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: gone, object: back)], timeout: 5)
        guard !back.exists else { return }
        XCTAssertTrue(row.isHittable, "Right swipe must return to the message list")

        row.tap()
        XCTAssertTrue(back.waitForExistence(timeout: 5))
        app.buttons["section-home"].tap()
        app.buttons["section-messages"].tap()
        XCTAssertTrue(back.waitForExistence(timeout: 5))
        let edgeStart = app.coordinate(withNormalizedOffset: CGVector(dx: 0.005, dy: 0.45))
        edgeStart.press(forDuration: 0.05, thenDragTo: contentEnd)
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: gone, object: back)], timeout: 5)
        XCTAssertTrue(row.isHittable, "Edge back must survive a retained tab disappearing and reappearing")
        capture("messages-swipe-back-and-tab-reentry", app: app)
    }

    func testCachedConversationKeepsItsInitialPositionDuringRefresh() async throws {
        let app = try await launch(configuration: ["messagesRead": true])
        app.buttons["section-messages"].tap()
        let row = app.buttons["message-\(threadID)"]
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        let search = app.buttons["collection-search-open"]
        XCTAssertTrue(search.exists)
        let listFrame = search.frame
        row.tap()
        let latest = app.staticTexts["第 3 条未读 UAT 消息。"]
        XCTAssertTrue(latest.waitForExistence(timeout: 5))
        XCTAssertTrue(latest.isHittable, "A read conversation must initially show its latest message")
        if UIDevice.current.userInterfaceIdiom == .pad {
            app.buttons["section-home"].tap()
            app.buttons["section-messages"].tap()
        } else {
            app.buttons["返回消息"].tap()
            XCTAssertTrue(row.waitForExistence(timeout: 5))
            XCTAssertEqual(search.frame.minY, listFrame.minY, accuracy: 1)
        }
        _ = try await fixture("/__uat/configure", body: ["messageReadDelayMS": 4000])
        let state = try await fixture("/__uat/state")
        let before = state["messageReadsCompleted"] as? Int ?? 0
        if UIDevice.current.userInterfaceIdiom == .pad {
            app.buttons["section-home"].tap()
            app.buttons["section-messages"].tap()
        } else {
            row.tap()
        }
        XCTAssertTrue(latest.waitForExistence(timeout: 2))
        XCTAssertTrue(latest.isHittable, "Cached history must open at the same target before the request finishes")
        let initialFrame = latest.frame
        let pending = try await fixture("/__uat/state")
        XCTAssertEqual(pending["messageReadsCompleted"] as? Int, before, "Capture must precede the delayed response")
        var refreshed = false
        for _ in 0..<50 where !refreshed {
            let state = try await fixture("/__uat/state")
            refreshed = (state["messageReadsCompleted"] as? Int ?? 0) > before
            if !refreshed { try await Task.sleep(nanoseconds: 100_000_000) }
        }
        XCTAssertTrue(refreshed)
        XCTAssertTrue(latest.isHittable)
        XCTAssertEqual(latest.frame.minY, initialFrame.minY, accuracy: 1, "Refreshing the same history must not move the viewport")
        capture("messages-stable-cached-position", app: app)
        let earliest = app.staticTexts["历史 UAT 回复 1"]
        for _ in 0..<5 where !earliest.isHittable {
            app.scrollViews.firstMatch.swipeDown()
        }
        XCTAssertTrue(earliest.isHittable, "The timestamp gesture must leave vertical history scrolling available")
    }

    func testHomeCallRecordingAndNavigation() async throws {
        let app = try await launch()
        app.buttons["activity-call-uat-call-example"].tap()
        XCTAssertTrue(app.staticTexts["录音片段 1"].waitForExistence(timeout: 5))
        capture("home-call-recording-detail", app: app)
        if UIDevice.current.userInterfaceIdiom == .pad {
            XCTAssertTrue(app.buttons["activity-message-\(threadID)"].exists)
            XCUIDevice.shared.orientation = .landscapeLeft
            let landscape = NSPredicate { _, _ in app.frame.width > app.frame.height }
            await fulfillment(of: [XCTNSPredicateExpectation(predicate: landscape, object: app)], timeout: 5)
            XCTAssertTrue(app.staticTexts["录音片段 1"].waitForExistence(timeout: 3))
            // Let the system rotation snapshot finish before retaining visual evidence.
            try await Task.sleep(nanoseconds: 800_000_000)
            capture("ipad-landscape-master-detail", app: app)
            XCUIDevice.shared.orientation = .portrait
        } else {
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.005, dy: 0.45))
            let finish = app.coordinate(withNormalizedOffset: CGVector(dx: 0.85, dy: 0.45))
            start.press(forDuration: 0.05, thenDragTo: finish)
            XCTAssertTrue(app.buttons["activity-message-\(threadID)"].waitForExistence(timeout: 4))
        }
    }

    func testCallBatchDeleteRequiresConfirmationAndUpdatesHome() async throws {
        let app = try await launch()
        app.buttons["section-calls"].tap()
        let call = app.buttons["call-uat-call-example"]
        let missed = app.buttons["call-uat-call-missed"]
        XCTAssertTrue(call.waitForExistence(timeout: 5))
        call.press(forDuration: 0.65)
        XCTAssertTrue(app.staticTexts["已选择 1 项"].waitForExistence(timeout: 3))
        missed.tap()
        app.buttons["删除"].tap()
        let confirmation = app.alerts["删除所选通话？"]
        XCTAssertTrue(confirmation.waitForExistence(timeout: 3))
        confirmation.buttons["取消"].tap()
        XCTAssertTrue(call.exists && missed.exists)
        let before = try await fixture("/__uat/state")
        XCTAssertEqual((before["operations"] as? [Any])?.count, 0)
        app.buttons["删除"].tap()
        confirmation.buttons["删除"].tap()
        let removed = NSPredicate(format: "exists == false")
        await fulfillment(of: [
            XCTNSPredicateExpectation(predicate: removed, object: missed),
            XCTNSPredicateExpectation(predicate: removed, object: call)
        ], timeout: 8)
        app.buttons["section-home"].tap()
        XCTAssertTrue(app.buttons["activity-message-\(threadID)"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["activity-call-uat-call-example"].exists)
        XCTAssertFalse(app.buttons["activity-call-uat-call-missed"].exists)
        app.buttons["section-calls"].tap()
        app.buttons["有录音"].tap()
        XCTAssertFalse(app.buttons["call-uat-call-example"].exists)
        let after = try await fixture("/__uat/state")
        XCTAssertEqual((after["threads"] as? [Any])?.count, 2)
        XCTAssertEqual((after["calls"] as? [Any])?.count, 0)
        XCTAssertEqual((after["recordings"] as? [Any])?.count, 0)
        XCTAssertEqual((after["operations"] as? [Any])?.count, 1)
    }

    func testRemovedDetailAfterResumeRemainsNavigable() async throws {
        let app = try await launch()
        app.buttons["activity-call-uat-call-example"].tap()
        XCTAssertTrue(app.staticTexts["录音片段 1"].waitForExistence(timeout: 5))
        XCUIDevice.shared.press(.home)
        _ = try await fixture("/api/v1/calls/batch", body: ["action": "delete", "ids": ["uat-call-example"]], method: "PATCH")
        app.activate()
        if UIDevice.current.userInterfaceIdiom == .pad {
            XCTAssertTrue(app.staticTexts["选择短信或通话"].waitForExistence(timeout: 8))
        } else {
            XCTAssertTrue(app.staticTexts["条目已不可用"].waitForExistence(timeout: 8))
            app.buttons["返回"].tap()
        }
        XCTAssertTrue(app.buttons["activity-message-\(threadID)"].exists)
        XCTAssertFalse(app.buttons["activity-call-uat-call-example"].exists)
    }

    func testOfflineHistoryAndAutomaticReconnect() async throws {
        let app = try await launch()
        app.terminate()
        _ = try await fixture("/__uat/connectivity", body: ["online": false])
        app.launch()
        XCTAssertTrue(app.otherElements["connection-offline"].waitForExistence(timeout: 12))
        XCTAssertTrue(app.buttons["activity-message-\(threadID)"].exists, "Cached history must remain available")
        XCTAssertTrue(app.staticTexts["待同步"].exists, "Cached line status must not claim to be live")
        capture("offline-history", app: app)
        _ = try await fixture("/__uat/connectivity", body: ["online": true])
        let recovered = NSPredicate(format: "exists == false")
        await fulfillment(of: [XCTNSPredicateExpectation(predicate: recovered, object: app.otherElements["connection-offline"])], timeout: 40)
        XCTAssertTrue(app.buttons["activity-message-\(threadID)"].exists)
        let state = try await fixture("/__uat/state")
        XCTAssertEqual((state["operations"] as? [Any])?.count, 0, "Reconnect must never replay writes")
        capture("automatically-reconnected", app: app)
    }
}
