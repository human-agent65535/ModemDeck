import SwiftUI
import UIKit

extension Color {
    static func adaptive(_ light: UInt32, _ dark: UInt32) -> Color {
        Color(UIColor { traits in
            let hex = traits.userInterfaceStyle == .dark ? dark : light
            return UIColor(red: CGFloat((hex >> 16) & 255) / 255,
                           green: CGFloat((hex >> 8) & 255) / 255,
                           blue: CGFloat(hex & 255) / 255, alpha: 1)
        })
    }

    static let mdBackground = adaptive(0xF5F7F6, 0x101512)
    static let mdSurface = adaptive(0xFFFFFF, 0x1C2420)
    static let mdSurfaceSubtle = adaptive(0xF5F7F6, 0x171E1A)
    static let mdSurfaceHover = adaptive(0xE9EEEB, 0x29332D)
    static let mdSelected = adaptive(0xDFEEE7, 0x244238)
    static let mdText = adaptive(0x19241F, 0xEBF0ED)
    static let mdMuted = adaptive(0x58675F, 0xAFBDB4)
    static let mdFaint = adaptive(0x636F68, 0x9BAAA1)
    static let mdBorder = adaptive(0xDEE5E0, 0x354139)
    static let mdAccent = adaptive(0x117864, 0x85D4B8)
    static let mdAccentStrong = adaptive(0x0C624F, 0xA5E3CC)
    static let mdAccentSoft = adaptive(0xE1F0E9, 0x243E33)
    static let mdOnAccent = adaptive(0xFFFFFF, 0x103328)
    static let mdBlue = adaptive(0x2568A6, 0x92C5F3)
    static let mdBlueSoft = adaptive(0xEAF2F9, 0x20384D)
    static let mdDanger = adaptive(0xB93643, 0xFF9B9E)
    static let mdDangerSoft = adaptive(0xFCEBEE, 0x3D2226)
    static let mdSuccess = adaptive(0x18794E, 0x7BD6A2)
    static let mdWarning = adaptive(0x946200, 0xF0C35C)
    static let mdWarningSoft = adaptive(0xFFF5D8, 0x3A2F14)
    static let mdFavorite = adaptive(0xA86400, 0xF2B544)
    // Filled call controls carry a white glyph: >= 4.5:1 against it in both
    // appearances and >= 3:1 against mdBackground.
    static let mdCallEnd = adaptive(0xC72E3D, 0xD03A47)
    static let mdCallAnswer = adaptive(0x1B8449, 0x1C874B)
    static let modemDeckTint = mdAccent
}

/// Line identity colors. Matches the Web `--line-<id>-*` palette and stays
/// apart from mdAccent/mdDanger so a line never reads as a status color.
struct ModemDeckLineColor {
    let foreground: Color
    let soft: Color
    let border: Color

    private init(_ fg: (UInt32, UInt32), _ soft: (UInt32, UInt32), _ border: (UInt32, UInt32)) {
        foreground = .adaptive(fg.0, fg.1)
        self.soft = .adaptive(soft.0, soft.1)
        self.border = .adaptive(border.0, border.1)
    }

    static let teal = Self((0x075E54, 0x7FD8CB), (0xD7F1ED, 0x173B36), (0x78C4BA, 0x2F6E65))
    static let blue = Self((0x1E4F8A, 0x9CC3F2), (0xDFEAFA, 0x1C2F47), (0x87ADD6, 0x3A5F8E))
    static let indigo = Self((0x343D91, 0xB3B9F5), (0xE2E5FB, 0x262B52), (0x9199DC, 0x4D5499))
    static let violet = Self((0x6B3287, 0xD9B0EE), (0xF0E1F8, 0x372443), (0xBF8BD7, 0x704A86))
    static let green = Self((0x35651E, 0xAED89A), (0xE2F1D9, 0x223A1A), (0x91C277, 0x4C7239))
    static let amber = Self((0x765000, 0xF0CD6E), (0xFFF0BD, 0x3A3014), (0xE2BA55, 0x7A6224))
    static let orange = Self((0x8B3E08, 0xF5B286), (0xFFE3CF, 0x40271A), (0xE9A16D, 0x87542F))
    static let red = Self((0x982D22, 0xF2A196), (0xF8DAD5, 0x45221E), (0xDE8072, 0x8E463C))

    static func preset(_ id: String) -> ModemDeckLineColor? {
        switch id {
        case "teal": return .teal
        case "blue": return .blue
        case "indigo": return .indigo
        case "violet": return .violet
        case "green": return .green
        case "amber": return .amber
        case "orange": return .orange
        case "red": return .red
        default: return nil
        }
    }

    /// Same order and FNV-1a hash as Web lineTone.ts, so an unlabeled line
    /// gets the same color on both clients; teal/green/red are never chosen.
    static let automatic: [ModemDeckLineColor] = [.blue, .amber, .violet, .orange, .indigo]

    static func automatic(for lineID: String) -> ModemDeckLineColor {
        let key = lineID.trimmingCharacters(in: .whitespacesAndNewlines)
        var hash: UInt32 = 2_166_136_261
        for scalar in (key.isEmpty ? "line" : key).unicodeScalars {
            hash ^= scalar.value
            hash = hash &* 16_777_619
        }
        return automatic[Int(hash % UInt32(automatic.count))]
    }
}

enum ModemDeckRadius {
    static let small: CGFloat = 8
    static let medium: CGFloat = 12
    static let large: CGFloat = 20
    static let xLarge: CGFloat = 28
}

enum ModemDeckSpacing {
    static let xxs: CGFloat = 4
    static let xs: CGFloat = 8
    static let sm: CGFloat = 12
    static let md: CGFloat = 16
    static let lg: CGFloat = 20
    static let xl: CGFloat = 24
    static let xxl: CGFloat = 32
}

extension RoundedRectangle {
    static func modemDeck(_ radius: CGFloat) -> RoundedRectangle {
        RoundedRectangle(cornerRadius: radius, style: .continuous)
    }
}

enum ModemDeckAppearance: String, CaseIterable, Identifiable {
    case system, light, dark
    var id: String { rawValue }
    var colorScheme: ColorScheme? {
        switch self {
        case .system: return nil
        case .light: return .light
        case .dark: return .dark
        }
    }
}

enum ModemDeckPanDirection {
    /// The pan's translation so far, or its velocity before it has moved.
    static func movement(of pan: UIPanGestureRecognizer, in view: UIView?) -> CGPoint {
        let translation = pan.translation(in: view)
        return translation == .zero ? pan.velocity(in: view) : translation
    }
}

private struct ModemDeckInteractivePopSupport: UIViewControllerRepresentable {
    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        weak var navigationController: UINavigationController?
        private weak var edgeGesture: UIGestureRecognizer?
        private weak var contentGesture: UIGestureRecognizer?
        private weak var previousEdgeDelegate: UIGestureRecognizerDelegate?
        private weak var previousContentDelegate: UIGestureRecognizerDelegate?

        func install(on navigationController: UINavigationController?) {
            guard let navigationController,
                  let gesture = navigationController.interactivePopGestureRecognizer else { return }
            if self.navigationController !== navigationController { uninstall() }
            self.navigationController = navigationController
            if gesture.delegate !== self { previousEdgeDelegate = gesture.delegate }
            edgeGesture = gesture
            gesture.delegate = self
            gesture.isEnabled = true
            if #available(iOS 26.0, *),
               let gesture = navigationController.interactiveContentPopGestureRecognizer {
                if gesture.delegate !== self { previousContentDelegate = gesture.delegate }
                contentGesture = gesture
                gesture.delegate = self
                gesture.isEnabled = true
            }
        }

        func uninstall() {
            if edgeGesture?.delegate === self { edgeGesture?.delegate = previousEdgeDelegate }
            if contentGesture?.delegate === self { contentGesture?.delegate = previousContentDelegate }
            edgeGesture = nil
            contentGesture = nil
            navigationController = nil
        }

        func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
            guard let navigationController,
                  navigationController.viewControllers.count > 1,
                  navigationController.transitionCoordinator == nil else { return false }
            // The screen-edge recognizer is already directional; checking its velocity would
            // reject a slow, deliberate swipe from the edge.
            if gestureRecognizer === edgeGesture { return true }
            if let pan = gestureRecognizer as? UIPanGestureRecognizer {
                // Judge direction from the accumulated movement: at recognition a slow finger's
                // velocity is near zero and noisy, which made back swipes need a quick flick.
                let movement = ModemDeckPanDirection.movement(of: pan, in: navigationController.view)
                let direction: CGFloat = navigationController.view.effectiveUserInterfaceLayoutDirection == .rightToLeft ? -1 : 1
                return movement.x * direction > abs(movement.y)
            }
            return true
        }
    }

    final class ResolverViewController: UIViewController {
        var resolve: ((UINavigationController?) -> Void)?

        override func viewDidAppear(_ animated: Bool) {
            super.viewDidAppear(animated)
            resolve?(navigationController)
        }

        override func didMove(toParent parent: UIViewController?) {
            super.didMove(toParent: parent)
            resolve?(navigationController)
        }

        override func viewDidLayoutSubviews() {
            super.viewDidLayoutSubviews()
            // SwiftUI can reconfigure navigation gestures after an appearance,
            // including when a retained tab is attached to the shell again.
            if view.window != nil { resolve?(navigationController) }
        }
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIViewController(context: Context) -> ResolverViewController {
        let controller = ResolverViewController()
        controller.view.isUserInteractionEnabled = false
        controller.view.backgroundColor = .clear
        controller.resolve = { [weak coordinator = context.coordinator] navigationController in
            coordinator?.install(on: navigationController)
        }
        return controller
    }

    func updateUIViewController(_ controller: ResolverViewController, context: Context) {
        context.coordinator.install(on: controller.navigationController)
    }

    static func dismantleUIViewController(_ controller: ResolverViewController, coordinator: Coordinator) {
        controller.resolve = nil
        coordinator.uninstall()
    }
}

private struct ModemDeckInteractiveBackModifier: ViewModifier {
    let enabled: Bool

    @ViewBuilder
    func body(content: Content) -> some View {
        if enabled {
            content.background(
                ModemDeckInteractivePopSupport()
                    .frame(width: 0, height: 0)
            )
        } else {
            content
        }
    }
}

extension View {
    func modemDeckInteractiveBack(_ enabled: Bool = true) -> some View {
        modifier(ModemDeckInteractiveBackModifier(enabled: enabled))
    }
}

struct ModemDeckCopyItem: Identifiable, Hashable {
    let label: String
    let value: String

    var id: String { "\(label)\u{0}\(value)" }
}

private struct ModemDeckCopyMenuModifier: ViewModifier {
    let items: [ModemDeckCopyItem]
    let actions: [ModemDeckContextAction]
    @Environment(\.modemDeckAllowsContextMenu) private var allowsContextMenu

    @ViewBuilder
    func body(content: Content) -> some View {
        if !allowsContextMenu {
            content.accessibilityActions {
                ForEach(items) { item in
                    Button(item.label) { UIPasteboard.general.string = item.value }
                }
            }
        } else if items.isEmpty && actions.isEmpty {
            content
        } else {
            content.contextMenu {
                ForEach(items) { item in
                    Button {
                        UIPasteboard.general.string = item.value
                    } label: {
                        Label(item.label, systemImage: "doc.on.doc")
                    }
                }
                if !items.isEmpty && !actions.isEmpty { Divider() }
                ForEach(actions) { action in
                    Button(role: action.destructive ? .destructive : nil, action: action.perform) {
                        Label(action.title, systemImage: action.icon)
                    }
                    .disabled(action.disabled)
                }
            }
        }
    }
}

extension View {
    func modemDeckCopyMenu(_ items: [ModemDeckCopyItem], actions: [ModemDeckContextAction] = []) -> some View {
        modifier(ModemDeckCopyMenuModifier(items: items.filter { !$0.value.isEmpty }, actions: actions))
    }
}

extension ModemDeckSessionController {
    var usesChinese: Bool {
        // The server reports "auto" (or nothing) when the system language follows each device.
        let configured = session?.language.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let language = configured.isEmpty || configured.lowercased() == "auto"
            ? Locale.preferredLanguages.first ?? "en"
            : configured
        return language.lowercased().hasPrefix("zh")
    }

    func text(_ chinese: String, _ english: String) -> String {
        usesChinese ? chinese : english
    }

    /// Standard phone labels arrive as English keys ("mobile", "home"); custom labels show as typed.
    func phoneLabelText(_ label: String) -> String {
        switch label.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() {
        case "mobile", "cell": return text("手机", "Mobile")
        case "home": return text("住宅", "Home")
        case "work": return text("工作", "Work")
        case "main": return text("主要", "Main")
        case "iphone": return "iPhone"
        case "fax", "home fax", "work fax": return text("传真", "Fax")
        case "pager": return text("传呼", "Pager")
        case "other": return text("其他", "Other")
        case "": return text("电话", "Phone")
        default: return label
        }
    }

    func dateText(_ value: String) -> String {
        ModemDeckDateText.short(
            value,
            locale: Locale(identifier: usesChinese ? "zh_CN" : "en_US")
        )
    }

    /// Time of day only, for rows that already sit under a day heading.
    func timeOfDayText(_ value: String) -> String {
        guard let date = ModemDeckDateText.date(value) else { return value }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: usesChinese ? "zh_CN" : "en_US")
        formatter.setLocalizedDateFormatFromTemplate("j:mm")
        return formatter.string(from: date)
    }

    func compactDateText(_ value: String) -> String {
        ModemDeckDateText.compact(
            value,
            locale: Locale(identifier: usesChinese ? "zh_CN" : "en_US")
        )
    }

    func messageDayText(_ value: String) -> String {
        ModemDeckDateText.day(
            value,
            locale: Locale(identifier: usesChinese ? "zh_CN" : "en_US"),
            today: text("今天", "Today"),
            yesterday: text("昨天", "Yesterday")
        )
    }

    func messageTimeText(_ value: String) -> String {
        ModemDeckDateText.time(
            value,
            locale: Locale(identifier: usesChinese ? "zh_CN" : "en_US")
        )
    }
}

enum ModemDeckDateText {
    private static let fractionalFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()

    private static let formatter = ISO8601DateFormatter()

    static func date(_ value: String) -> Date? {
        fractionalFormatter.date(from: value) ?? formatter.date(from: value)
    }

    static func short(_ value: String, locale: Locale = .current) -> String {
        guard !value.isEmpty, let date = date(value) else { return value }
        return date.formatted(
            Date.FormatStyle(date: .abbreviated, time: .shortened, locale: locale)
        )
    }

    static func compact(_ value: String, locale: Locale = .current) -> String {
        guard !value.isEmpty, let date = date(value) else { return value }
        let calendar = Calendar.current
        let formatter = DateFormatter()
        formatter.locale = locale
        if calendar.isDateInToday(date) {
            formatter.setLocalizedDateFormatFromTemplate("j:mm")
        } else if calendar.component(.year, from: date) == calendar.component(.year, from: Date()) {
            formatter.setLocalizedDateFormatFromTemplate("Md")
        } else {
            formatter.setLocalizedDateFormatFromTemplate("yMd")
        }
        return formatter.string(from: date)
    }

    static func day(
        _ value: String,
        locale: Locale = .current,
        today: String,
        yesterday: String
    ) -> String {
        guard !value.isEmpty, let date = date(value) else { return value }
        let calendar = Calendar.current
        if calendar.isDateInToday(date) { return today }
        if calendar.isDateInYesterday(date) { return yesterday }
        let formatter = DateFormatter()
        formatter.locale = locale
        if calendar.component(.year, from: date) == calendar.component(.year, from: Date()) {
            formatter.setLocalizedDateFormatFromTemplate("EEEEMMMMd")
        } else {
            formatter.setLocalizedDateFormatFromTemplate("yMMMEd")
        }
        return formatter.string(from: date)
    }

    static func time(_ value: String, locale: Locale = .current) -> String {
        guard !value.isEmpty, let date = date(value) else { return value }
        let formatter = DateFormatter()
        formatter.locale = locale
        formatter.setLocalizedDateFormatFromTemplate("j:mm")
        return formatter.string(from: date)
    }

    static func sameDay(_ left: String, _ right: String) -> Bool {
        guard let leftDate = date(left), let rightDate = date(right) else { return left == right }
        return Calendar.current.isDate(leftDate, inSameDayAs: rightDate)
    }

    static func duration(_ seconds: Int) -> String {
        let safe = max(0, seconds)
        return String(format: "%d:%02d", safe / 60, safe % 60)
    }
}

enum ModemDeckLayout {
    /// The list column's width before the shell has measured the screen.
    static let padListWidth: CGFloat = 340
    /// Measured from the scene, not the device idiom: iPhone Duo's unfolded display reports the
    /// phone idiom, and its 951 pt wide scene fits the rail and two columns.
    static let splitWorkspaceMinimumWidth: CGFloat = 900
    /// Comfortable reading width for conversations and forms on wide screens.
    static let readableContentWidth: CGFloat = 720

    /// About 36% of the content width, kept between a usable minimum and a readable maximum.
    static func listColumnWidth(forContentWidth width: CGFloat) -> CGFloat {
        min(400, max(320, (width * 0.36).rounded()))
    }
    static let pageHorizontalPadding: CGFloat = 20
    static let pageHeaderHeight: CGFloat = 76
    static let collectionHeaderHeight: CGFloat = 56
    static let toolbarHorizontalPadding: CGFloat = 20
    static let toolbarVerticalPadding: CGFloat = 4
    static let toolbarGap: CGFloat = 6
    static let controlHitSize: CGFloat = 44
    static let controlVisualSize: CGFloat = 36
    static let listHorizontalPadding: CGFloat = 20
    static let listRowMinHeight: CGFloat = 80
    /// Settings directory rows are single-purpose and denser than communication rows.
    static let settingsRowMinHeight: CGFloat = 64
    static let listAvatarSize: CGFloat = 44
    static let listAvatarTextSpacing: CGFloat = 12
    static let listTextLeading = listHorizontalPadding + listAvatarSize + listAvatarTextSpacing
    static let listUnreadIndicatorSize: CGFloat = 10
    static let listSelectionMarkWidth: CGFloat = 24
    /// Header glyphs keep growing with text up to here, then stay inside their 44 pt targets.
    static let headerGlyphMaximumSize = DynamicTypeSize.xxxLarge
}

/// Rows grouped under a day heading show only the time, so the date is not said twice.
private struct ModemDeckRowsShowTimeOnlyKey: EnvironmentKey {
    static let defaultValue = false
}

private struct ModemDeckListColumnWidthKey: EnvironmentKey {
    static let defaultValue = ModemDeckLayout.padListWidth
}

private struct ModemDeckUsesSplitWorkspaceKey: EnvironmentKey {
    static let defaultValue = false
}

struct ModemDeckPadDialerAction {
    let accessibilityLabel: String
    let open: () -> Void
}

private struct ModemDeckPadDialerActionKey: EnvironmentKey {
    static let defaultValue: ModemDeckPadDialerAction? = nil
}

extension EnvironmentValues {
    var modemDeckRowsShowTimeOnly: Bool {
        get { self[ModemDeckRowsShowTimeOnlyKey.self] }
        set { self[ModemDeckRowsShowTimeOnlyKey.self] = newValue }
    }

    /// The list column's width beside a detail, proportional to the space available.
    var modemDeckListColumnWidth: CGFloat {
        get { self[ModemDeckListColumnWidthKey.self] }
        set { self[ModemDeckListColumnWidthKey.self] = newValue }
    }

    var modemDeckUsesSplitWorkspace: Bool {
        get { self[ModemDeckUsesSplitWorkspaceKey.self] }
        set { self[ModemDeckUsesSplitWorkspaceKey.self] = newValue }
    }

    var modemDeckPadDialerAction: ModemDeckPadDialerAction? {
        get { self[ModemDeckPadDialerActionKey.self] }
        set { self[ModemDeckPadDialerActionKey.self] = newValue }
    }
}

struct ModemDeckRootView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @ObservedObject private var callController: ModemDeckCallController
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var started = false
    @State private var callExpanded = true
    @AppStorage("modemdeck.appearance") private var appearance = ModemDeckAppearance.system.rawValue

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        _callController = ObservedObject(wrappedValue: controller.callController)
    }

    var body: some View {
        ZStack {
            content
                .allowsHitTesting(!callController.endedCallClosing && (callController.visibleCall == nil || !callExpanded))
                .accessibilityHidden(callController.visibleCall != nil && callExpanded)

            if let call = callController.visibleCall {
                ModemDeckActiveCallView(
                    call: call,
                    controller: controller,
                    callController: callController,
                    collapse: { callExpanded = false }
                )
                .accessibilityElement(children: .contain)
                .accessibilityAddTraits(callExpanded ? .isModal : [])
                .opacity(callExpanded && !callController.endedCallClosing ? 1 : 0)
                .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: callController.endedCallClosing)
                .allowsHitTesting(callExpanded && !callController.endedCallClosing)
                .accessibilityHidden(!callExpanded)
                .transition(reduceMotion ? .identity : .opacity)
                .zIndex(20)
            }
        }
        .preferredColorScheme(ModemDeckAppearance(rawValue: appearance)?.colorScheme)
        .accentColor(.mdAccent)
        .textSelection(.enabled)
        .alert(
            controller.text("通话连接失败", "Call Connection Failed"),
            isPresented: Binding(
                get: { !callController.connectionFailureMessage.isEmpty },
                set: { if !$0 { callController.connectionFailureMessage = "" } }
            )
        ) {
            Button(controller.text("好", "OK"), role: .cancel) {
                callController.connectionFailureMessage = ""
            }
        } message: {
            Text(callController.connectionFailureMessage)
        }
        .alert(
            controller.text("无法发起通话", "Call Could Not Start"),
            isPresented: Binding(
                get: { callController.startFailure != nil && callController.call == nil },
                set: { if !$0 { callController.startFailure = nil } }
            ),
            presenting: callController.startFailure
        ) { failure in
            if failure.microphoneDenied, let url = URL(string: UIApplication.openSettingsURLString) {
                Button(controller.text("打开设置", "Open Settings")) {
                    callController.startFailure = nil
                    UIApplication.shared.open(url)
                }
            }
            Button(controller.text("好", "OK"), role: .cancel) {
                callController.startFailure = nil
            }
        } message: { failure in
            Text(failure.microphoneDenied
                ? controller.text("拨打电话前需要允许麦克风访问。", "Microphone access is required before placing a call.")
                : failure.message)
        }
        .task {
            guard !started else { return }
            started = true
            await controller.start()
            consumePendingNotificationRoute()
        }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didBecomeActiveNotification)) { _ in
            callController.refreshFromCallKit()
            Task {
                await controller.refreshNotificationStatus()
                controller.refreshMicrophoneStatus()
                await controller.resume()
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didEnterBackgroundNotification)) { _ in
            controller.suspend()
        }
        .onReceive(NotificationCenter.default.publisher(for: .modemDeckRemoteNotification)) { notification in
            guard notification.object == nil else { return }
            guard controller.phase == .paired else { return }
            Task { await controller.refresh() }
        }
        .onReceive(NotificationCenter.default.publisher(for: .modemDeckNotificationResponse)) { _ in
            consumePendingNotificationRoute()
        }
        .onReceive(NotificationCenter.default.publisher(for: .modemDeckAuthenticationFailed)) { _ in
            Task { await controller.handleAuthenticationFailure() }
        }
        .onChange(of: callController.call?.id) { callID in
            guard callID != nil else { return }
            callExpanded = true
            UIApplication.shared.sendAction(
                #selector(UIResponder.resignFirstResponder),
                to: nil,
                from: nil,
                for: nil
            )
        }
    }

    @ViewBuilder
    private var content: some View {
        switch controller.phase {
        case .launching, .loading:
            ModemDeckLaunchView(
                title: controller.text("正在连接 ModemDeck…", "Connecting to ModemDeck…")
            )
        case .unpaired:
            ModemDeckPairingView(session: controller)
        case .paired:
            ModemDeckAdaptiveShell(controller: controller, callExpanded: $callExpanded)
                .safeAreaInset(edge: .top, spacing: 0) {
                    if controller.connectionState == .offline {
                        ModemDeckOfflineBanner(controller: controller)
                    }
                }
        case .unavailable:
            ModemDeckUnavailableView(controller: controller)
        }
    }

    private func consumePendingNotificationRoute() {
        guard let route = ModemDeckPushCoordinator.shared.consumePendingNotificationRoute() else {
            return
        }
        controller.openMessage(threadKey: route.threadKey)
    }
}

private struct ModemDeckOfflineBanner: View {
    @ObservedObject var controller: ModemDeckSessionController

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "wifi.slash")
                .font(.caption.weight(.semibold))
            Text(controller.text("暂时离线 · 自动重连中", "Offline · Reconnecting"))
                .font(.caption.weight(.medium))
                .lineLimit(1)
                .accessibilityHint(controller.text("历史内容仍可查看和复制。", "Saved history remains available to read and copy."))
            Spacer(minLength: 8)
            Button(controller.text("重试", "Retry")) {
                Task { await controller.refresh() }
            }
            .font(.caption.weight(.semibold))
            .disabled(controller.reconnecting)
        }
        .foregroundColor(.mdText)
        .padding(.horizontal, 12)
        .frame(minHeight: 34)
        .background(Color.mdWarningSoft)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("connection-offline")
        .overlay(alignment: .bottom) {
            Rectangle().fill(Color.mdWarning.opacity(0.25)).frame(height: 1)
        }
    }
}

private struct ModemDeckLaunchView: View {
    let title: String

    var body: some View {
        VStack(spacing: 18) {
            Image(systemName: "antenna.radiowaves.left.and.right")
                .font(.system(size: 46, weight: .medium))
                .foregroundColor(.mdAccent)
            ProgressView(title)
                .foregroundColor(.mdMuted)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.mdBackground.ignoresSafeArea())
    }
}

private struct ModemDeckUnavailableView: View {
    @ObservedObject var controller: ModemDeckSessionController

    var body: some View {
        VStack(spacing: 18) {
            Image(systemName: "wifi.exclamationmark")
                .font(.system(size: 50))
                .foregroundColor(.mdWarning)
            Text(controller.text("暂时无法连接", "Unable to Connect"))
                .font(.title2.bold())
                .foregroundColor(.mdText)
            Text(controller.errorMessage)
                .foregroundColor(.mdMuted)
                .multilineTextAlignment(.center)
            Button(controller.text("重试", "Try Again")) {
                Task { await controller.refresh() }
            }
            .buttonStyle(.borderedProminent)
            .tint(.mdAccent)
            Button(controller.text("重新配对", "Pair Again"), role: .destructive) {
                Task { await controller.disconnect() }
            }
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.mdBackground.ignoresSafeArea())
    }
}

enum ModemDeckSection: String, CaseIterable, Identifiable {
    case home
    case contacts
    case messages
    case dial
    case calls
    case recordings
    case settings

    var id: String { rawValue }

    static var contentSections: [ModemDeckSection] {
        [.home, .messages, .contacts, .calls, .settings]
    }

    static var initialSection: ModemDeckSection {
        #if DEBUG
        let value = ProcessInfo.processInfo.environment["MODEMDECK_UAT_INITIAL_SECTION"] ?? ""
        return ModemDeckSection(rawValue: value) ?? .home
        #else
        return .home
        #endif
    }

    var icon: String {
        switch self {
        case .home: return ModemDeckLucideAsset.house
        case .contacts: return ModemDeckLucideAsset.usersRound
        case .messages: return ModemDeckLucideAsset.messageSquareText
        case .dial: return ModemDeckLucideAsset.phoneCall
        case .calls: return ModemDeckLucideAsset.phone
        case .recordings: return ModemDeckLucideAsset.audioLines
        case .settings: return ModemDeckLucideAsset.settings
        }
    }
}

@MainActor
private final class ModemDeckSectionNavigation: ObservableObject {
    @Published var path: [ModemDeckRoute] = []
}

/// One navigation path per section for the life of the shell, so a tab returns where it was.
@MainActor
private final class ModemDeckShellNavigation: ObservableObject {
    private var paths: [ModemDeckSection: ModemDeckSectionNavigation] = [:]

    func navigation(for section: ModemDeckSection) -> ModemDeckSectionNavigation {
        if let existing = paths[section] { return existing }
        let created = ModemDeckSectionNavigation()
        paths[section] = created
        return created
    }
}

private struct ModemDeckSectionRoot: View {
    let section: ModemDeckSection
    @ObservedObject var controller: ModemDeckSessionController
    let padDialerAction: ModemDeckPadDialerAction?
    @ObservedObject var navigation: ModemDeckSectionNavigation

    var body: some View {
        NavigationStack(path: $navigation.path) {
            destination
                .navigationDestination(for: ModemDeckRoute.self) { route in
                    ModemDeckRouteContent(route: route, controller: controller)
                }
        }
        .environment(\.modemDeckNavigate, { navigation.path.append($0) })
        // Dialing belongs to Recents and Calls; other sections keep their toolbars uncluttered.
        .environment(\.modemDeckPadDialerAction, section == .home || section == .calls ? padDialerAction : nil)
        .onChange(of: controller.callsNavigationRevision) { _ in
            if section == .calls { navigation.path.removeAll() }
        }
    }

    @ViewBuilder
    private var destination: some View {
        switch section {
        case .home:
            ModemDeckHomeView(controller: controller)
        case .contacts:
            ModemDeckContactsView(controller: controller)
        case .messages:
            ModemDeckMessagesView(controller: controller)
        case .calls:
            ModemDeckCallsView(controller: controller)
        case .recordings:
            ModemDeckRecordingsView(controller: controller)
        case .settings:
            ModemDeckSettingsView(controller: controller)
        case .dial:
            EmptyView()
        }
    }
}

private struct ModemDeckAdaptiveShell: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Binding var callExpanded: Bool

    var body: some View {
        GeometryReader { geometry in
            // iPhone Duo keeps its status bar in a trailing safe-area column, so measure the
            // whole scene rather than the inset content width.
            let sceneWidth = geometry.size.width + geometry.safeAreaInsets.leading + geometry.safeAreaInsets.trailing
            let usesSplitWorkspace = sceneWidth >= ModemDeckLayout.splitWorkspaceMinimumWidth
            ModemDeckWorkspaceShell(controller: controller, callExpanded: $callExpanded)
                .environment(\.modemDeckUsesSplitWorkspace, usesSplitWorkspace)
                .environment(\.modemDeckListColumnWidth, ModemDeckLayout.listColumnWidth(
                    forContentWidth: geometry.size.width
                ))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

/// The system tab view: a bottom tab bar on iPhone, a sidebar or floating tab bar on iPad, and on
/// iPhone Duo the system's vertical tab bar in the status-bar column, with the page's toolbar
/// buttons beside it.
private struct ModemDeckWorkspaceShell: View {
    @ObservedObject var controller: ModemDeckSessionController
    @ObservedObject private var callController: ModemDeckCallController
    @Binding var callExpanded: Bool
    @StateObject private var navigation = ModemDeckShellNavigation()
    @State private var showingDialer: Bool
    private let badges: ModemDeckSectionBadgeSource

    init(controller: ModemDeckSessionController, callExpanded: Binding<Bool>) {
        self.controller = controller
        _callController = ObservedObject(wrappedValue: controller.callController)
        _callExpanded = callExpanded
        _showingDialer = State(initialValue: ModemDeckSection.initialSection == .dial)
        badges = ModemDeckSectionBadgeSource(controller: controller)
    }

    var body: some View {
        tabs
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.mdBackground.ignoresSafeArea())
        .sheet(isPresented: $showingDialer) {
            ModemDeckDialerPanel(controller: controller, close: { showingDialer = false })
                .presentationDetents([.height(640), .large])
                .presentationDragIndicator(.visible)
        }
        .onReceive(controller.callController.$call) { call in
            if call != nil { showingDialer = false }
        }
        .onChange(of: controller.selectedSection) { _ in
            UIApplication.shared.sendAction(
                #selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil
            )
        }
    }

    /// The dialer is a sheet, not a tab; a request for it shows Recents underneath.
    private var selection: Binding<ModemDeckSection> {
        Binding(
            get: { controller.selectedSection == .dial ? .home : controller.selectedSection },
            set: { controller.selectedSection = $0 }
        )
    }

    @ViewBuilder
    private var tabs: some View {
        if #available(iOS 18.0, *) {
            TabView(selection: selection) {
                ForEach(ModemDeckSection.contentSections) { section in
                    Tab(section.title(controller), systemImage: section.systemImage, value: section) {
                        tabContent(section)
                    }
                    .badge(badges.count(for: section))
                }
            }
            .tabViewStyle(.sidebarAdaptable)
        } else {
            TabView(selection: selection) {
                ForEach(ModemDeckSection.contentSections) { section in
                    tabContent(section)
                        .tabItem { Label(section.title(controller), systemImage: section.systemImage) }
                        .badge(badges.count(for: section))
                        .tag(section)
                }
            }
        }
    }

    private func tabContent(_ section: ModemDeckSection) -> some View {
        ModemDeckSectionRoot(
            section: section,
            controller: controller,
            padDialerAction: ModemDeckPadDialerAction(
                accessibilityLabel: controller.text("打开拨号盘", "Open dialer"),
                open: openDialer
            ),
            navigation: navigation.navigation(for: section)
        )
        // A collapsed call stays one tap away above the tab bar, and above a conversation's
        // composer when the tab bar steps aside.
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if let call = miniCall {
                ModemDeckMiniCallBar(call: call, controller: controller, callController: callController,
                                     restore: { callExpanded = true })
                    .padding(.horizontal, 12).padding(.vertical, 8)
            }
        }
    }

    private var miniCall: ModemDeckPresentedCall? {
        callExpanded ? nil : callController.visibleCall
    }

    private func openDialer() {
        if callController.call != nil { callExpanded = true } else { showingDialer = true }
    }
}

/// Unread messages and unread missed calls shown on the section navigation.
private struct ModemDeckSectionBadgeSource: DynamicProperty {
    @ObservedObject var messages: ModemDeckMessagesStore
    @ObservedObject var calls: ModemDeckCallsStore

    init(controller: ModemDeckSessionController) {
        _messages = ObservedObject(wrappedValue: controller.messagesStore)
        _calls = ObservedObject(wrappedValue: controller.callsStore)
    }

    func count(for section: ModemDeckSection) -> Int {
        switch section {
        case .messages: return messages.unreadSummary.unreadThreadCount
        case .calls: return calls.calls.lazy.filter { $0.missed && !$0.read }.count
        default: return 0
        }
    }
}

private struct ModemDeckListColumnModifier: ViewModifier {
    @Environment(\.modemDeckListColumnWidth) private var width

    func body(content: Content) -> some View {
        content.frame(width: width)
    }
}

extension View {
    /// Sizes a list beside its detail in proportion to the screen, as Mail and Messages do.
    func modemDeckListColumn() -> some View {
        modifier(ModemDeckListColumnModifier())
    }

    /// Hides the system tab bar for this page while it is shown, as Messages does for a
    /// conversation; the system animates the bar and the page's safe area together.
    @ViewBuilder func modemDeckTabBarVisible(_ visible: Bool) -> some View {
        if visible { self } else { toolbar(.hidden, for: .tabBar) }
    }

    /// A pushed detail takes the system bar's inline title and back button; shown beside its
    /// list it leaves the list page's bar alone.
    @ViewBuilder func modemDeckDetailTitle(_ title: String, pushed: Bool) -> some View {
        if pushed {
            navigationTitle(title).navigationBarTitleDisplayMode(.inline)
        } else {
            self
        }
    }

    /// Marks a pushed screen (a conversation or composer) that owns the bottom edge on iPhone.
    /// A detail embedded beside its list leaves the tab bar alone.
    func modemDeckImmersiveScreen(_ pushed: Bool = true) -> some View {
        modemDeckTabBarVisible(!pushed)
    }
}

/// A one-pixel separator that stays crisp at every display scale.
struct ModemDeckHairline: View {
    @Environment(\.displayScale) private var displayScale

    var body: some View {
        Rectangle().fill(Color.mdBorder).frame(height: 1 / displayScale)
    }
}

extension ModemDeckSection {
    var systemImage: String {
        switch self {
        case .home: return "clock.arrow.circlepath"
        case .messages: return "bubble.left.and.bubble.right"
        case .contacts: return "person.2"
        case .calls: return "phone"
        case .settings: return "gearshape"
        case .recordings: return "waveform"
        case .dial: return "phone.arrow.up.right"
        }
    }

    @MainActor func title(_ controller: ModemDeckSessionController) -> String {
        switch self {
        case .home: return controller.text("最近", "Recents")
        case .messages: return controller.text("消息", "Messages")
        case .contacts: return controller.text("联系人", "Contacts")
        case .calls: return controller.text("通话", "Calls")
        case .settings: return controller.text("设置", "Settings")
        case .recordings: return controller.text("录音", "Recordings")
        case .dial: return controller.text("拨号", "Dial")
        }
    }
}

struct ModemDeckHomeLineGrid: View {
    @ObservedObject var controller: ModemDeckSessionController
    let lines: [ModemDeckLine]

    var body: some View {
        Group {
            if lines.isEmpty {
                HStack(spacing: 10) {
                    Image(systemName: "simcard")
                        .foregroundColor(.mdFaint)
                    Text(controller.text("没有可用线路", "No available lines"))
                        .font(.footnote)
                        .foregroundColor(.mdMuted)
                }
                .padding(.horizontal, 12)
                .frame(maxWidth: .infinity, minHeight: 58, alignment: .leading)
                .background(Color.mdSurface)
                .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.medium))
                .overlay(
                    RoundedRectangle.modemDeck(ModemDeckRadius.medium)
                        .stroke(Color.mdBorder, lineWidth: 1)
                )
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(lines.enumerated()), id: \.element.id) { index, line in
                        if index > 0 { Divider().padding(.leading, 62) }
                        ModemDeckHomeLineCard(controller: controller, line: line)
                    }
                }
                .padding(4)
                .background(Color.mdSurface)
                .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.large))
            }
        }
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
    }
}

private struct ModemDeckHomeLineCard: View {
    @ObservedObject var controller: ModemDeckSessionController
    let line: ModemDeckLine

    private let onlineStates = Set(["active", "connected", "online", "ready", "registered"])

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: "simcard.fill")
                .font(.subheadline.weight(.semibold))
                .foregroundColor(line.tint)
                .frame(width: 34, height: 34)
                .background(line.palette.soft)
                .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.small))

            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 5) {
                    Text(line.displayName)
                        .font(.subheadline.weight(.semibold))
                        .foregroundColor(.mdText)
                        .lineLimit(1)
                    Spacer(minLength: 2)
                    Circle()
                        .fill(controller.isOnline && isOnline ? Color.mdAccent : Color.mdFaint)
                        .frame(width: 6, height: 6)
                    Text(statusText)
                        .font(.caption2)
                        .foregroundColor(controller.isOnline && isOnline ? .mdAccentStrong : .mdMuted)
                        .fixedSize()
                }
                if !detailText.isEmpty {
                    Text(detailText)
                        .font(.caption)
                        .foregroundColor(.mdMuted)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 10)
        .frame(maxWidth: .infinity, minHeight: 76, alignment: .leading)
        .background(Color.mdSurface)
        .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.medium))
        .accessibilityElement(children: .combine)
        .modemDeckCopyMenu([
            .init(label: controller.text("复制号码", "Copy Number"), value: line.phoneNumber),
            .init(label: controller.text("复制线路名称", "Copy Line Name"), value: line.displayName)
        ])
    }

    private var isOnline: Bool {
        onlineStates.contains(normalizedState) ||
            onlineStates.contains(line.registrationState.lowercased())
    }

    private var normalizedState: String {
        (line.state ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .lowercased()
    }

    private var statusText: String {
        guard controller.isOnline else { return controller.text("待同步", "Not synced") }
        return isOnline
            ? controller.text("在线", "Online")
            : controller.text("离线", "Offline")
    }

    private var detailText: String {
        let number = line.phoneNumber.trimmingCharacters(in: .whitespacesAndNewlines)
        if !number.isEmpty && number != line.displayName {
            return number
        }
        let network = line.networkName.trimmingCharacters(in: .whitespacesAndNewlines)
        return network.isEmpty ? line.id : network
    }
}

struct ModemDeckLoadErrorState: View {
    @ObservedObject var controller: ModemDeckSessionController
    let detail: String
    let retry: () -> Void

    var body: some View {
        VStack(spacing: 14) {
            Image(systemName: "wifi.exclamationmark")
                .font(.largeTitle)
                .foregroundColor(.mdMuted)
            Text(controller.text("暂时无法载入", "Unable to Load"))
                .font(.headline)
                .foregroundColor(.mdText)
            Text(detail)
                .font(.subheadline)
                .foregroundColor(.mdMuted)
                .multilineTextAlignment(.center)
            Button(controller.text("重试", "Try Again"), action: retry)
                .buttonStyle(.borderedProminent)
                .tint(.mdAccent)
        }
        .padding(28)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .contain)
    }
}

/// Gives a top-level page the system large title, with the dialer in the toolbar where
/// dialing belongs. It draws nothing itself.
struct ModemDeckPageHeader: View {
    let title: String
    @Environment(\.modemDeckPadDialerAction) private var padDialerAction

    var body: some View {
        Color.clear
            .frame(height: 0)
            .accessibilityHidden(true)
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.large)
            .toolbar {
                if let padDialerAction {
                    ToolbarItem(placement: .topBarTrailing) {
                        ModemDeckDialerToolbarButton(action: padDialerAction)
                    }
                }
            }
    }
}

struct ModemDeckDialerToolbarButton: View {
    let action: ModemDeckPadDialerAction

    var body: some View {
        Button(action: action.open) {
            Image(systemName: "circle.grid.3x3.fill")
        }
        .accessibilityLabel(action.accessibilityLabel)
        .accessibilityIdentifier("open-dialer")
    }
}

/// A list row's title and date share a line; at accessibility sizes the title wraps and the
/// date moves below it instead of truncating the name to a few characters.
struct ModemDeckRowTitleLine<Title: View, Trailing: View>: View {
    @ViewBuilder var title: Title
    @ViewBuilder var trailing: Trailing
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 2) {
                title.lineLimit(3)
                trailing
            }
        } else {
            HStack(spacing: 8) {
                title.lineLimit(1)
                Spacer(minLength: 6)
                trailing
            }
        }
    }
}

/// Gives a collection page the system large title, search field and toolbar. While the
/// page is selecting, the selection bar's header supplies the title and buttons instead.
/// The page keeps its query and filters when a retained tab is revisited.
struct ModemDeckCollectionHeader<Actions: View>: View {
    @ObservedObject var controller: ModemDeckSessionController
    let title: String
    @Binding var query: String
    var selecting = false
    @ViewBuilder var actions: () -> Actions
    @Environment(\.modemDeckPadDialerAction) private var dialerAction

    var body: some View {
        if selecting {
            // The selection bar's header owns the title and bar buttons while selecting.
            Color.clear.frame(height: 0).accessibilityHidden(true)
        } else {
            page
        }
    }

    private var page: some View {
        Color.clear
            .frame(height: 0)
            .accessibilityHidden(true)
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.large)
            .searchable(
                text: $query,
                placement: .navigationBarDrawer(displayMode: .automatic),
                prompt: controller.text("搜索", "Search")
            )
            .toolbar {
                ToolbarItemGroup(placement: .topBarTrailing) {
                    actions()
                    if let dialerAction {
                        ModemDeckDialerToolbarButton(action: dialerAction)
                    }
                }
            }
            .onChange(of: controller.selectedSection) { _ in
                UIApplication.shared.sendAction(
                    #selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil
                )
            }
    }
}

struct ModemDeckCollectionHeaderButton: View {
    let icon: String
    let title: String
    var disabled = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: icon)
        }
        .disabled(disabled)
        .accessibilityLabel(title)
    }
}

struct ModemDeckCollectionFilters<Extra: View>: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Binding var line: String
    @Binding var favoriteOnly: Bool
    var extraCount = 0
    var clearExtra: () -> Void = {}
    @ViewBuilder var extra: () -> Extra

    private var count: Int { (line.isEmpty ? 0 : 1) + (favoriteOnly ? 1 : 0) + extraCount }

    var body: some View {
        Menu {
            Picker(controller.text("线路", "Line"), selection: $line) {
                Text(controller.text("全部线路", "All lines")).tag("")
                ForEach(controller.bootstrap?.lineCatalog ?? []) { item in
                    Text(item.displayName).tag(item.id)
                }
            }
            Toggle(controller.text("仅收藏", "Favorites only"), isOn: $favoriteOnly)
            extra()
            if count > 0 {
                Button(controller.text("清除筛选", "Clear filters")) {
                    line = ""
                    favoriteOnly = false
                    clearExtra()
                }
            }
        } label: {
            HStack(spacing: 3) {
                Image(systemName: "line.3.horizontal.decrease").font(.body.weight(.medium))
                if count > 0 { Text("\(count)").font(.caption.weight(.bold)) }
            }
            .dynamicTypeSize(...ModemDeckLayout.headerGlyphMaximumSize)
            .foregroundColor(count > 0 ? .mdAccent : .mdMuted)
            .frame(width: 44, height: 44)
            .background(count > 0 ? Color.mdAccentSoft : .clear)
            .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.medium))
            .contentShape(Rectangle())
        }
        .accessibilityIdentifier("collection-filters")
        .accessibilityLabel(controller.text("筛选", "Filters"))
        .accessibilityValue(count > 0 ? controller.text("\(count) 项筛选", "\(count) filters") : controller.text("未筛选", "No filters"))
        .accessibilityAddTraits(count > 0 ? .isSelected : [])
    }
}

struct ModemDeckToolbarButton: View {
    let icon: String
    var active = false
    var accessibilityText = ""
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                RoundedRectangle.modemDeck(ModemDeckRadius.small)
                    .fill(active ? Color.mdAccentSoft : Color.mdSurface)
                    .overlay(
                        RoundedRectangle.modemDeck(ModemDeckRadius.small)
                            .stroke(active ? Color.mdAccent.opacity(0.45) : Color.mdBorder, lineWidth: 1)
                    )
                Image(systemName: icon)
                    .font(.system(size: 17, weight: .medium))
                    .foregroundColor(active ? .mdAccent : .mdMuted)
            }
            .frame(
                width: ModemDeckLayout.controlVisualSize,
                height: ModemDeckLayout.controlVisualSize
            )
            .frame(
                width: ModemDeckLayout.controlHitSize,
                height: ModemDeckLayout.controlHitSize
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(accessibilityText)
        .accessibilityAddTraits(active ? .isSelected : [])
    }
}

struct ModemDeckSegmentOption: Identifiable {
    let id: String
    let title: String
}

struct ModemDeckSegmentPicker: View {
    let options: [ModemDeckSegmentOption]
    @Binding var selection: String
    var compact = false
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        // Accessibility sizes cannot fit several tabs on one line; one menu keeps every label whole.
        if dynamicTypeSize.isAccessibilitySize {
            accessibilityMenu
        } else {
            segments
        }
    }

    private var accessibilityMenu: some View {
        Menu {
            Picker(selection: $selection) {
                ForEach(options) { option in Text(option.title).tag(option.id) }
            } label: { EmptyView() }
        } label: {
            HStack(spacing: 6) {
                Text(options.first { $0.id == selection }?.title ?? "")
                    .font(.subheadline.weight(.semibold))
                    .lineLimit(2)
                Image(systemName: "chevron.down").font(.caption.weight(.semibold))
            }
            .foregroundColor(.mdAccent)
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .background(Capsule().fill(Color.mdAccentSoft))
            .frame(minHeight: ModemDeckLayout.controlHitSize)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
    }

    private var segments: some View {
        HStack(spacing: 2) {
            ForEach(options) { option in
                Button {
                    selection = option.id
                } label: {
                    ZStack {
                        if selection == option.id && !compact {
                            RoundedRectangle.modemDeck(ModemDeckRadius.small)
                                .fill(Color.mdSurface)
                                .padding(.vertical, 4)
                        }
                        Text(option.title)
                            .font((compact ? Font.subheadline : .caption).weight(selection == option.id ? .semibold : .medium))
                            .foregroundColor(selection == option.id ? (compact ? .mdAccent : .mdText) : .mdMuted)
                            .lineLimit(1)
                            .minimumScaleFactor(0.85)
                    }
                    .frame(maxWidth: .infinity, minHeight: ModemDeckLayout.controlHitSize)
                    .contentShape(Rectangle())
                    .overlay(alignment: .bottom) {
                        if compact && selection == option.id {
                            Capsule().fill(Color.mdAccent).frame(height: 2).padding(.horizontal, 12)
                        }
                    }
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(selection == option.id ? .isSelected : [])
            }
        }
        .frame(height: ModemDeckLayout.controlHitSize)
        .background(compact ? Color.clear : Color.mdSurfaceHover)
        .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.medium))
    }
}

extension View {
    func modemDeckListToolbar(showsDivider: Bool = false) -> some View {
        padding(.horizontal, ModemDeckLayout.toolbarHorizontalPadding)
            .padding(.vertical, ModemDeckLayout.toolbarVerticalPadding)
            .background(Color.mdBackground)
    }

    func modemDeckListFilterBar() -> some View {
        padding(.horizontal, ModemDeckLayout.toolbarHorizontalPadding)
            .padding(.bottom, ModemDeckLayout.toolbarVerticalPadding)
            .background(Color.mdBackground)
    }
}

struct ModemDeckLineTag: View {
    let line: ModemDeckLine
    /// Dot + name without the tag chrome, for secondary rows such as the call subtitle.
    var compact = false

    var body: some View {
        if compact {
            HStack(spacing: 6) {
                Circle()
                    .fill(line.palette.foreground)
                    .frame(width: 8, height: 8)
                    .accessibilityHidden(true)
                Text(line.displayName)
                    .lineLimit(1)
            }
        } else {
            tag
        }
    }

    private var tag: some View {
        Text(line.displayName)
            .font(.caption.weight(.semibold))
            .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
            .foregroundColor(line.palette.foreground)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .frame(minHeight: 22)
            .background(Capsule().fill(line.palette.soft))
    }
}

struct ModemDeckListDivider: View {
    var leading: CGFloat = ModemDeckLayout.listTextLeading

    var body: some View {
        Rectangle()
            .fill(Color.mdBorder)
            .frame(height: 1)
            .padding(.leading, leading)
    }
}

struct ModemDeckSelectionMark: View {
    let selected: Bool

    var body: some View {
        Image(systemName: selected ? "checkmark.circle.fill" : "circle")
            .font(.system(size: 20, weight: .medium))
            .foregroundColor(selected ? .mdAccent : .mdFaint)
            .frame(width: ModemDeckLayout.listSelectionMarkWidth)
    }
}

/// Long press is the primary selection gesture; this menu also supports switch control and keyboard users.
struct ModemDeckCollectionMenu<Actions: View>: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Binding var selecting: Bool
    let busy: Bool
    let selectTitle: String
    let clearSelection: () -> Void
    @ViewBuilder var actions: () -> Actions

    var body: some View {
        Menu {
            Button {
                selecting.toggle()
                clearSelection()
            } label: {
                Label(selecting ? controller.text("完成", "Done") : selectTitle, systemImage: selecting ? "checkmark" : "checklist")
            }
            actions()
        } label: {
            Image(systemName: "ellipsis.circle")
        }
        .disabled(busy)
        .accessibilityLabel(controller.text("更多操作", "More actions"))
    }
}

struct ModemDeckBatchCopyMenu: View {
    @ObservedObject var controller: ModemDeckSessionController
    let items: [ModemDeckCopyItem]
    var body: some View {
        Menu {
            ForEach(items.filter { !$0.value.isEmpty }) { item in
                Button(item.label) { UIPasteboard.general.string = item.value }
            }
        } label: {
            ModemDeckBatchToolbarLabel(title: controller.text("复制", "Copy"), icon: "doc.on.doc")
        }
        .accessibilityLabel(controller.text("复制", "Copy"))
    }
}

enum ModemDeckBatchActionBarPart {
    /// "Select All · N selected · Done", laid over the page title while selecting.
    case header
    /// The selection's actions, in the tab bar's place as in Mail and Photos.
    case toolbar
}

struct ModemDeckBatchActionBar<Actions: View>: View {
    let part: ModemDeckBatchActionBarPart
    let selectedCount: Int
    let totalCount: Int
    let busy: Bool
    let selectedText: String
    let selectAllText: String
    let clearAllText: String
    let doneText: String
    let selectAll: () -> Void
    let done: () -> Void
    private let actions: Actions

    init(
        part: ModemDeckBatchActionBarPart = .toolbar,
        selectedCount: Int,
        totalCount: Int,
        busy: Bool,
        selectedText: String,
        selectAllText: String,
        clearAllText: String,
        doneText: String,
        selectAll: @escaping () -> Void,
        done: @escaping () -> Void,
        @ViewBuilder actions: () -> Actions
    ) {
        self.part = part
        self.selectedCount = selectedCount
        self.totalCount = totalCount
        self.busy = busy
        self.selectedText = selectedText
        self.selectAllText = selectAllText
        self.clearAllText = clearAllText
        self.doneText = doneText
        self.selectAll = selectAll
        self.done = done
        self.actions = actions()
    }

    private var allSelected: Bool {
        totalCount > 0 && selectedCount == totalCount
    }

    var body: some View {
        switch part {
        case .header: header
        case .toolbar: toolbar
        }
    }

    /// The navigation bar becomes "Select All · N selected · Done", as in Mail.
    private var header: some View {
        Color.clear
            .frame(width: 0, height: 0)
            .accessibilityHidden(true)
            .navigationTitle(String(format: selectedText, selectedCount))
            .navigationBarTitleDisplayMode(.inline)
            .navigationBarBackButtonHidden(true)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button(allSelected ? clearAllText : selectAllText, action: selectAll)
                        .disabled(busy || totalCount == 0)
                }
                ToolbarItem(placement: .topBarTrailing) {
                    if busy {
                        ProgressView()
                    } else {
                        Button(doneText, action: done)
                            .fontWeight(.semibold)
                            .accessibilityIdentifier("selection-done")
                    }
                }
            }
    }

    /// The toolbar takes the tab bar's place: the tab bar steps aside while selecting.
    private var toolbar: some View {
        HStack(alignment: .top, spacing: 4) {
            actions
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 6)
        .frame(minHeight: 56, alignment: .top)
        .frame(maxWidth: .infinity)
        .background(.bar, ignoresSafeAreaEdges: .bottom)
        .overlay(alignment: .top) { ModemDeckHairline() }
        .toolbar(.hidden, for: .tabBar)
    }
}

struct ModemDeckBatchActionButton: View {
    let title: String
    let icon: String
    var destructive = false
    var disabled = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            ModemDeckBatchToolbarLabel(title: title, icon: icon, destructive: destructive)
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .opacity(disabled ? 0.4 : 1)
    }
}

/// A toolbar item: glyph over a short caption, no tile behind it.
struct ModemDeckBatchToolbarLabel: View {
    let title: String
    let icon: String
    var destructive = false

    var body: some View {
        VStack(spacing: 3) {
            Image(systemName: icon)
                .font(.title3)
                .frame(height: 24)
            Text(title)
                .font(.caption2.weight(.medium))
                .lineLimit(1)
                .minimumScaleFactor(0.78)
        }
        .dynamicTypeSize(...DynamicTypeSize.xxLarge)
        .foregroundColor(destructive ? .mdDanger : .mdAccent)
        .frame(maxWidth: .infinity, minHeight: 44)
        .contentShape(Rectangle())
    }
}

extension ModemDeckLine {
    var palette: ModemDeckLineColor {
        ModemDeckLineColor.preset(lineColor ?? "") ?? .automatic(for: id)
    }

    var tint: Color { palette.foreground }
}

struct ModemDeckStateView: View {
    let icon: String
    let title: String
    let detail: String

    var body: some View {
        VStack(spacing: 8) {
            ModemDeckStateIcon(icon: icon)
                .padding(.bottom, 4)
            Text(title)
                .font(.headline)
                .foregroundColor(.mdText)
                .multilineTextAlignment(.center)
            Text(detail)
                .font(.subheadline)
                .foregroundColor(.mdMuted)
                .multilineTextAlignment(.center)
        }
        .padding(.horizontal, 28)
        .padding(.vertical, 36)
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .combine)
    }
}

/// The shared empty/state glyph: a muted symbol on a soft circular tile.
struct ModemDeckStateIcon: View {
    let icon: String
    @ScaledMetric(relativeTo: .title) private var size: CGFloat = 56

    var body: some View {
        Image(systemName: icon)
            .font(.title2.weight(.medium))
            .symbolRenderingMode(.hierarchical)
            .foregroundColor(.mdMuted)
            .frame(width: size, height: size)
            .background(Circle().fill(Color.mdSurfaceHover))
            .accessibilityHidden(true)
    }
}

struct ModemDeckWorkspaceEmptyView: View {
    let icon: String
    let title: String
    var detail = ""

    var body: some View {
        VStack(spacing: 8) {
            ModemDeckStateIcon(icon: icon)
                .padding(.bottom, 4)
            Text(title)
                .font(.headline)
                .foregroundColor(.mdText)
            if !detail.isEmpty {
                Text(detail)
                    .font(.footnote)
                    .foregroundColor(.mdFaint)
                    .multilineTextAlignment(.center)
            }
        }
        .padding(28)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.mdBackground)
    }
}

struct ModemDeckInlineError: View {
    let message: String

    var body: some View {
        if !message.isEmpty {
            Label(message, systemImage: "exclamationmark.triangle.fill")
                .font(.footnote)
                .foregroundColor(.mdDanger)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(Color.mdSurface)
                .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.small))
        }
    }
}
