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

    private func launch() async throws -> XCUIApplication {
        continueAfterFailure = false
        _ = try await fixture("/__uat/reset", body: [:])
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchEnvironment = [
            "MODEMDECK_UAT_MODE": "1", "MODEMDECK_UAT_SERVER_URL": server,
            "MODEMDECK_UAT_TOKEN": token, "MODEMDECK_UAT_INITIAL_SECTION": "home"
        ]
        app.launch()
        XCTAssertTrue(app.buttons["activity-message-\(threadID)"].waitForExistence(timeout: 20))
        return app
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
        XCTAssertTrue(app.textFields.firstMatch.exists, "Search remains available in Messages")
        XCTAssertFalse((messageRow.value as? String)?.contains("已收藏") == true)
        app.buttons["选择消息"].tap()
        messageRow.tap()
        app.buttons["message-uat-line-a:UAT-SERVICE"].tap()
        XCTAssertTrue(app.staticTexts["已选择 2 项"].exists)
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
        app.buttons["选择通话"].tap()
        call.tap()
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
        app.buttons["section-recordings"].tap()
        XCTAssertFalse(app.buttons["recording-uat-recording-example"].exists)
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
