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
    static let padListWidth: CGFloat = 340
    /// Measured from the scene, not the device idiom: iPhone Duo's unfolded display reports the
    /// phone idiom, and its 951 pt wide scene fits the rail and two columns.
    static let splitWorkspaceMinimumWidth: CGFloat = 900
    /// A trailing safe area this wide is a status-bar column rather than a rounded-corner inset.
    static let statusColumnMinimumWidth: CGFloat = 60

    /// iPhone Duo centres its clock and status icons under the camera cutout, 48 pt from the
    /// trailing edge on both screens, while the safe-area column is 84 pt wide. UIKit reports no
    /// side status-bar frame, so the column's items take that axis from this measured offset.
    static let statusColumnAxisFromTrailingEdge: CGFloat = 48
    /// Room the clock and status icons take at the top of the Duo status column.
    static let statusColumnStatusClearance: CGFloat = 132
    /// The navigation items stop this far above the bottom edge, within thumb reach.
    static let statusColumnBottomInset: CGFloat = 40

    /// Extra trailing inset that moves column content onto the status items' axis.
    static func statusColumnAxisInset(columnWidth: CGFloat) -> CGFloat {
        min(columnWidth / 3, max(0, 2 * (statusColumnAxisFromTrailingEdge - columnWidth / 2)))
    }
    static let pageHorizontalPadding: CGFloat = 20
    static let pageHeaderHeight: CGFloat = 76
    static let collectionHeaderHeight: CGFloat = 56
    static let toolbarHorizontalPadding: CGFloat = 20
    static let toolbarVerticalPadding: CGFloat = 4
    static let toolbarGap: CGFloat = 6
    static let controlHitSize: CGFloat = 44
    static let controlVisualSize: CGFloat = 36
    static let searchVisualHeight: CGFloat = 44
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

private struct ModemDeckSectionTabs: View {
    @ObservedObject var controller: ModemDeckSessionController
    @EnvironmentObject private var retainedSections: ModemDeckLazySectionHost.Coordinator
    @Environment(\.modemDeckUsesSplitWorkspace) private var usesSplitWorkspace
    @Environment(\.modemDeckPadDialerAction) private var padDialerAction

    var body: some View {
        ModemDeckLazySectionHost(
            controller: controller,
            activeSection: activeSection,
            usesSplitWorkspace: usesSplitWorkspace,
            padDialerAction: padDialerAction,
            retainedCoordinator: retainedSections
        )
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.mdBackground)
        .animation(nil, value: activeSection)
        .onChange(of: activeSection) { _ in
            UIApplication.shared.sendAction(
                #selector(UIResponder.resignFirstResponder),
                to: nil,
                from: nil,
                for: nil
            )
        }
    }

    private var activeSection: ModemDeckSection {
        controller.selectedSection == .dial
            ? .home
            : controller.selectedSection
    }

}

@MainActor
private final class ModemDeckSectionNavigation: ObservableObject {
    @Published var path: [ModemDeckRoute] = []
}

private struct ModemDeckSectionRoot: View {
    let section: ModemDeckSection
    @ObservedObject var controller: ModemDeckSessionController
    let usesSplitWorkspace: Bool
    let padDialerAction: ModemDeckPadDialerAction?
    @ObservedObject var navigation: ModemDeckSectionNavigation

    var body: some View {
        NavigationStack(path: $navigation.path) {
            destination
                .modemDeckTabBarClearance()
                .navigationDestination(for: ModemDeckRoute.self) { route in
                    if route.opensConversation {
                        ModemDeckRouteContent(route: route, controller: controller)
                    } else {
                        ModemDeckRouteContent(route: route, controller: controller)
                            .modemDeckTabBarClearance()
                    }
                }
        }
        .toolbar(.hidden, for: .navigationBar)
        .environment(\.modemDeckNavigate, { navigation.path.append($0) })
        .environment(\.modemDeckUsesSplitWorkspace, usesSplitWorkspace)
        // Dialing belongs to Recents and Calls; other sections keep their headers uncluttered.
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

private struct ModemDeckLazySectionHost: UIViewControllerRepresentable {
    final class ContainerViewController: UIViewController {
        private weak var visibleController: UIViewController?

        override func viewDidLoad() {
            super.viewDidLoad()
            view.backgroundColor = UIColor(Color.mdBackground)
        }

        func show(_ controller: UIViewController) {
            guard visibleController !== controller else { return }
            if let visibleController {
                visibleController.willMove(toParent: nil)
                visibleController.view.removeFromSuperview()
                visibleController.removeFromParent()
            }
            if controller.parent != nil {
                controller.willMove(toParent: nil)
                controller.view.removeFromSuperview()
                controller.removeFromParent()
            }
            addChild(controller)
            controller.view.translatesAutoresizingMaskIntoConstraints = false
            view.addSubview(controller.view)
            NSLayoutConstraint.activate([
                controller.view.leadingAnchor.constraint(equalTo: view.leadingAnchor),
                controller.view.trailingAnchor.constraint(equalTo: view.trailingAnchor),
                controller.view.topAnchor.constraint(equalTo: view.topAnchor),
                controller.view.bottomAnchor.constraint(equalTo: view.bottomAnchor)
            ])
            controller.didMove(toParent: self)
            visibleController = controller
        }

    }

    @MainActor
    final class Coordinator: ObservableObject {
        var sections: [ModemDeckSection: UIHostingController<ModemDeckSectionRoot>] = [:]
        private var navigation: [ModemDeckSection: ModemDeckSectionNavigation] = [:]
        private var isEnvironmentConfigured = false
        private var usesSplitWorkspace = false
        private var padDialerAction: ModemDeckPadDialerAction?

        func updateEnvironment(
            usesSplitWorkspace: Bool,
            padDialerAction: ModemDeckPadDialerAction?,
            controller: ModemDeckSessionController
        ) {
            let layoutChanged = !isEnvironmentConfigured ||
                self.usesSplitWorkspace != usesSplitWorkspace
            let actionChanged = !isEnvironmentConfigured ||
                self.padDialerAction?.accessibilityLabel != padDialerAction?.accessibilityLabel
            self.usesSplitWorkspace = usesSplitWorkspace
            if actionChanged || self.padDialerAction == nil {
                self.padDialerAction = padDialerAction
            }
            isEnvironmentConfigured = true
            guard layoutChanged || actionChanged else { return }
            for (section, hosted) in sections {
                hosted.rootView = rootView(for: section, controller: controller)
            }
        }

        func sectionController(
            for section: ModemDeckSection,
            controller: ModemDeckSessionController
        ) -> UIHostingController<ModemDeckSectionRoot> {
            if let existing = sections[section] { return existing }
            let hosted = UIHostingController(rootView: rootView(for: section, controller: controller))
            hosted.view.backgroundColor = UIColor(Color.mdBackground)
            sections[section] = hosted
            return hosted
        }

        private func rootView(
            for section: ModemDeckSection,
            controller: ModemDeckSessionController
        ) -> ModemDeckSectionRoot {
            let state = navigation[section] ?? ModemDeckSectionNavigation()
            navigation[section] = state
            return ModemDeckSectionRoot(
                section: section,
                controller: controller,
                usesSplitWorkspace: usesSplitWorkspace,
                padDialerAction: padDialerAction,
                navigation: state
            )
        }
    }

    @ObservedObject var controller: ModemDeckSessionController
    let activeSection: ModemDeckSection
    let usesSplitWorkspace: Bool
    let padDialerAction: ModemDeckPadDialerAction?
    let retainedCoordinator: Coordinator

    func makeCoordinator() -> Coordinator { retainedCoordinator }

    func makeUIViewController(context: Context) -> ContainerViewController {
        ContainerViewController()
    }

    func updateUIViewController(
        _ container: ContainerViewController,
        context: Context
    ) {
        context.coordinator.updateEnvironment(
            usesSplitWorkspace: usesSplitWorkspace,
            padDialerAction: padDialerAction,
            controller: controller
        )
        container.show(
            context.coordinator.sectionController(for: activeSection, controller: controller)
        )
    }
}

private struct ModemDeckAdaptiveShell: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Binding var callExpanded: Bool
    @StateObject private var retainedSections = ModemDeckLazySectionHost.Coordinator()

    var body: some View {
        GeometryReader { geometry in
            // iPhone Duo keeps its status bar in a trailing safe-area column, so measure the
            // whole scene rather than the inset content width.
            let sceneWidth = geometry.size.width + geometry.safeAreaInsets.leading + geometry.safeAreaInsets.trailing
            let usesSplitWorkspace = sceneWidth >= ModemDeckLayout.splitWorkspaceMinimumWidth
            let insets = geometry.safeAreaInsets
            let statusColumnWidth = insets.trailing >= ModemDeckLayout.statusColumnMinimumWidth && insets.leading < 1
                ? insets.trailing : nil
            let statusAxisInset = statusColumnWidth.map(ModemDeckLayout.statusColumnAxisInset) ?? 0
            ModemDeckWorkspaceShell(
                controller: controller, usesSplitWorkspace: usesSplitWorkspace,
                statusColumnWidth: statusColumnWidth, statusAxisInset: statusAxisInset,
                callExpanded: $callExpanded
            )
            .environment(\.modemDeckUsesSplitWorkspace, usesSplitWorkspace)
            .environmentObject(retainedSections)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

private struct ModemDeckWorkspaceShell: View {
    @ObservedObject var controller: ModemDeckSessionController
    @ObservedObject private var callController: ModemDeckCallController
    @Binding var callExpanded: Bool
    let usesSplitWorkspace: Bool
    /// Width of a trailing status-bar column (iPhone Duo) that hosts section navigation.
    let statusColumnWidth: CGFloat?
    /// Trailing inset that puts the column's items on the system status items' axis.
    let statusAxisInset: CGFloat
    @State private var showingDialer: Bool
    @State private var tabBarHeight: CGFloat = 61
    @State private var miniCallBarHeight: CGFloat = 72
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(
        controller: ModemDeckSessionController, usesSplitWorkspace: Bool,
        statusColumnWidth: CGFloat?, statusAxisInset: CGFloat = 0, callExpanded: Binding<Bool>
    ) {
        self.controller = controller
        self.usesSplitWorkspace = usesSplitWorkspace
        self.statusColumnWidth = statusColumnWidth
        self.statusAxisInset = statusAxisInset
        _callController = ObservedObject(wrappedValue: controller.callController)
        _callExpanded = callExpanded
        _showingDialer = State(initialValue: ModemDeckSection.initialSection == .dial)
    }

    var body: some View {
        Group {
            if let statusColumnWidth {
                // The column beside the status bar replaces both the tab bar and the rail, so
                // pages take the column's place in the trailing safe area.
                HStack(spacing: 0) {
                    VStack(spacing: 0) {
                        sectionTabs()
                        if let call = miniCall { miniCallBar(call) }
                    }
                    ModemDeckStatusColumnNavigation(
                        controller: controller, selection: $controller.selectedSection,
                        axisInset: statusAxisInset
                    )
                        .frame(width: statusColumnWidth)
                        // Stay put under the status items while the keyboard is up.
                        .ignoresSafeArea(.keyboard)
                }
                .ignoresSafeArea(.container, edges: .trailing)
            } else if usesSplitWorkspace {
                HStack(spacing: 0) {
                    ModemDeckPadNavigationRail(
                        controller: controller, selection: $controller.selectedSection,
                        openDialer: openDialer
                    )
                    Rectangle().fill(Color.mdBorder).frame(width: 1)
                    VStack(spacing: 0) {
                        sectionTabs(headerDialer: false)
                        if let call = miniCall { miniCallBar(call) }
                    }
                }
            } else {
                sectionTabs()
                    // Pages extend under the floating bottom chrome and reserve its space
                    // themselves, so hiding the tab bar for a conversation never resizes the
                    // navigation container mid-push.
                    .environment(\.modemDeckBottomChrome, ModemDeckBottomChrome(
                        tabBar: tabBarHeight,
                        miniCallBar: miniCall == nil ? 0 : miniCallBarHeight
                    ))
                    .overlay(alignment: .bottom) { phoneBottomChrome }
            }
        }
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
    }

    /// The iPad rail has its own Dial button, so page headers offer the dialer only without it.
    private func sectionTabs(headerDialer: Bool = true) -> some View {
        ModemDeckSectionTabs(controller: controller)
            .environment(\.modemDeckPadDialerAction, headerDialer ? ModemDeckPadDialerAction(
                accessibilityLabel: controller.text("打开拨号盘", "Open dialer"),
                open: openDialer
            ) : nil)
    }

    private var miniCall: ModemDeckPresentedCall? {
        callExpanded ? nil : callController.visibleCall
    }

    private func openDialer() {
        if callController.call != nil { callExpanded = true } else { showingDialer = true }
    }

    private func miniCallBar(_ call: ModemDeckPresentedCall) -> some View {
        ModemDeckMiniCallBar(call: call, controller: controller, callController: callController,
                             restore: { callExpanded = true })
            .padding(.horizontal, 12).padding(.vertical, 8)
    }

    /// The iPhone tab bar and mini call bar float over the reserved bottom space; the tab bar
    /// slides away while a conversation is open instead of changing the layout under it.
    private var phoneBottomChrome: some View {
        let showsTabBar = controller.immersiveScreenCount == 0
        return VStack(spacing: 0) {
            if let call = miniCall {
                miniCallBar(call).modemDeckReadHeight($miniCallBarHeight)
            }
            if showsTabBar {
                ModemDeckPhoneTabBar(controller: controller, selection: $controller.selectedSection)
                    .modemDeckReadHeight($tabBarHeight)
                    .transition(.move(edge: .bottom))
            }
        }
        .animation(reduceMotion ? nil : .easeInOut(duration: 0.3), value: showsTabBar)
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

private struct ModemDeckSectionBadge: View {
    let count: Int

    var body: some View {
        if count > 0 {
            Text(count > 99 ? "99+" : "\(count)")
                .font(.caption2.weight(.bold).monospacedDigit())
                .foregroundColor(.white)
                .padding(.horizontal, 5)
                .frame(minWidth: 18, minHeight: 18)
                .background(Capsule().fill(Color.mdCallEnd))
                .accessibilityHidden(true)
                .allowsHitTesting(false)
        }
    }
}

private struct ModemDeckPhoneTabBar: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Binding var selection: ModemDeckSection
    private let badges: ModemDeckSectionBadgeSource

    init(controller: ModemDeckSessionController, selection: Binding<ModemDeckSection>) {
        _controller = ObservedObject(wrappedValue: controller)
        _selection = selection
        badges = ModemDeckSectionBadgeSource(controller: controller)
    }

    var body: some View {
        HStack(spacing: 0) {
            ForEach(ModemDeckSection.contentSections) { section in
                let selected = selection == section
                let badge = badges.count(for: section)
                Button { selection = section } label: {
                    VStack(spacing: 4) {
                        Image(systemName: section.systemImage)
                            .symbolVariant(selected ? .fill : .none)
                            .font(.system(size: 21, weight: selected ? .semibold : .regular))
                            .frame(height: 25)
                            .overlay(alignment: .topTrailing) {
                                ModemDeckSectionBadge(count: badge).offset(x: 12, y: -6)
                            }
                        Text(section.title(controller))
                            .font(.caption2.weight(selected ? .semibold : .medium))
                            .lineLimit(1)
                    }
                    .foregroundColor(selected ? .mdAccent : .mdMuted)
                    .frame(maxWidth: .infinity, minHeight: 56)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("section-\(section.id)")
                .accessibilityValue(badge > 0 ? "\(badge)" : "")
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
        }
        .padding(.horizontal, 8)
        .padding(.top, 5)
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
        .modemDeckSelectionFeedback(selection)
        .background(.bar, ignoresSafeAreaEdges: [.bottom, .horizontal])
        .overlay(alignment: .top) { ModemDeckHairline().ignoresSafeArea(edges: .horizontal) }
        .accessibilityElement(children: .contain)
    }
}

/// Section navigation stacked in iPhone Duo's trailing status-bar column, below the status items.
private struct ModemDeckStatusColumnNavigation: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Binding var selection: ModemDeckSection
    let axisInset: CGFloat
    private let badges: ModemDeckSectionBadgeSource
    @State private var keyboardVisible = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(controller: ModemDeckSessionController, selection: Binding<ModemDeckSection>, axisInset: CGFloat) {
        _controller = ObservedObject(wrappedValue: controller)
        _selection = selection
        self.axisInset = axisInset
        badges = ModemDeckSectionBadgeSource(controller: controller)
    }

    var body: some View {
        VStack(spacing: 4) {
            // Clear of the clock and status icons; the items then sit low for thumb reach.
            Spacer(minLength: ModemDeckLayout.statusColumnStatusClearance)
            ForEach(ModemDeckSection.contentSections) { section in
                let selected = selection == section
                let badge = badges.count(for: section)
                Button { selection = section } label: {
                    VStack(spacing: 4) {
                        Image(systemName: section.systemImage)
                            .symbolVariant(selected ? .fill : .none)
                            .font(.system(size: 21, weight: selected ? .semibold : .regular))
                            .frame(height: 25)
                            .overlay(alignment: .topTrailing) {
                                ModemDeckSectionBadge(count: badge).offset(x: 12, y: -6)
                            }
                        Text(section.title(controller))
                            .font(.caption2.weight(selected ? .semibold : .medium))
                            .lineLimit(1)
                            .minimumScaleFactor(0.8)
                    }
                    .foregroundColor(selected ? .mdAccent : .mdMuted)
                    .frame(maxWidth: .infinity, minHeight: 60)
                    // Selection reads by shape as well as colour, like the iPad rail.
                    .background(
                        RoundedRectangle.modemDeck(ModemDeckRadius.medium)
                            .fill(selected ? Color.mdAccentSoft : Color.clear)
                    )
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("section-\(section.id)")
                .accessibilityValue(badge > 0 ? "\(badge)" : "")
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
            Spacer(minLength: 12).frame(maxHeight: ModemDeckLayout.statusColumnBottomInset)
        }
        .padding(.leading, 6)
        // The system centres the clock and status icons under the camera, not on the whole inset.
        .padding(.trailing, 6 + axisInset)
        .frame(maxHeight: .infinity)
        // While typing, the keyboard owns the bottom of the screen; the column steps aside
        // instead of leaving a stub of items above it.
        .opacity(keyboardVisible ? 0 : 1)
        .allowsHitTesting(!keyboardVisible)
        .accessibilityHidden(keyboardVisible)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: keyboardVisible)
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in
            keyboardVisible = true
        }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in
            keyboardVisible = false
        }
        .dynamicTypeSize(...DynamicTypeSize.xxLarge)
        .modemDeckSelectionFeedback(selection)
        .background(.bar, ignoresSafeAreaEdges: .all)
        .overlay(alignment: .leading) {
            Rectangle().fill(Color.mdBorder).frame(width: 1).ignoresSafeArea()
        }
        .accessibilityElement(children: .contain)
    }
}

private struct ModemDeckPadNavigationRail: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Binding var selection: ModemDeckSection
    let openDialer: () -> Void
    private let badges: ModemDeckSectionBadgeSource

    init(controller: ModemDeckSessionController, selection: Binding<ModemDeckSection>, openDialer: @escaping () -> Void) {
        _controller = ObservedObject(wrappedValue: controller)
        _selection = selection
        self.openDialer = openDialer
        badges = ModemDeckSectionBadgeSource(controller: controller)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("ModemDeck")
                .font(.title3.weight(.semibold))
                .foregroundColor(.mdText)
                .padding(.horizontal, 12)
                .padding(.vertical, 24)
            ForEach(ModemDeckSection.contentSections) { section in
                let selected = selection == section
                let badge = badges.count(for: section)
                Button { selection = section } label: {
                    HStack(spacing: 12) {
                        Image(systemName: section.systemImage)
                            .symbolVariant(selected ? .fill : .none)
                            .frame(width: 24)
                        Text(section.title(controller))
                        Spacer(minLength: 4)
                        if badge > 0 {
                            Text("\(badge)")
                                .font(.footnote.weight(.semibold).monospacedDigit())
                                .foregroundColor(selected ? .mdOnAccent : .mdMuted)
                        }
                    }
                    .font(.body.weight(selected ? .semibold : .regular))
                    .foregroundColor(selected ? .mdOnAccent : .mdText)
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .padding(.horizontal, 12)
                    .background(selected ? Color.mdAccent : Color.clear)
                    .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.medium))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .hoverEffect(.highlight)
                .accessibilityIdentifier("section-\(section.id)")
                .accessibilityValue(badge > 0 ? "\(badge)" : "")
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
            Spacer()
            Button(action: openDialer) {
                Label(controller.text("拨号", "Dial"), systemImage: "circle.grid.3x3.fill")
                    .font(.body.weight(.semibold))
                    .frame(maxWidth: .infinity, minHeight: 44)
            }
            .buttonStyle(.borderedProminent)
            .tint(.mdAccent)
            .buttonBorderShape(.capsule)
            .padding(.bottom, 16)
            .accessibilityLabel(controller.text("打开拨号盘", "Open dialer"))
        }
        .padding(.horizontal, 12)
        .frame(width: 196)
        .modemDeckSelectionFeedback(selection)
        .background(.bar)
    }
}

/// Heights of the iPhone shell's floating bottom bars.
struct ModemDeckBottomChrome: Equatable {
    var tabBar: CGFloat = 0
    var miniCallBar: CGFloat = 0
}

private struct ModemDeckBottomChromeKey: EnvironmentKey {
    static let defaultValue = ModemDeckBottomChrome()
}

extension EnvironmentValues {
    var modemDeckBottomChrome: ModemDeckBottomChrome {
        get { self[ModemDeckBottomChromeKey.self] }
        set { self[ModemDeckBottomChromeKey.self] = newValue }
    }
}

private struct ModemDeckBottomChromeClearance: ViewModifier {
    let includesTabBar: Bool
    @Environment(\.modemDeckBottomChrome) private var chrome

    func body(content: Content) -> some View {
        content.safeAreaInset(edge: .bottom, spacing: 0) {
            Color.clear
                .frame(height: chrome.miniCallBar + (includesTabBar ? chrome.tabBar : 0))
                .allowsHitTesting(false)
        }
    }
}

private struct ModemDeckImmersiveScreenModifier: ViewModifier {
    let controller: ModemDeckSessionController

    func body(content: Content) -> some View {
        content
            .modifier(ModemDeckBottomChromeClearance(includesTabBar: false))
            .onAppear { controller.enterImmersiveScreen() }
            .onDisappear { controller.leaveImmersiveScreen() }
    }
}

extension View {
    /// Reserves the iPhone tab bar (and any mini call bar) under a section screen.
    /// Every pushed screen other than a conversation or composer applies this.
    func modemDeckTabBarClearance() -> some View {
        modifier(ModemDeckBottomChromeClearance(includesTabBar: true))
    }

    /// Marks a pushed screen (a conversation or composer) that should own the bottom edge on iPhone.
    func modemDeckImmersiveScreen(_ controller: ModemDeckSessionController) -> some View {
        modifier(ModemDeckImmersiveScreenModifier(controller: controller))
    }

    func modemDeckReadHeight(_ height: Binding<CGFloat>) -> some View {
        background(GeometryReader { proxy in
            Color.clear
                .onAppear { height.wrappedValue = proxy.size.height }
                .onChange(of: proxy.size.height) { height.wrappedValue = $0 }
        })
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

struct ModemDeckPageHeader: View {
    let title: String
    var backAction: (() -> Void)? = nil
    var backAccessibilityText = ""
    var secondaryActionIcon: String? = nil
    var secondaryActionAccessibilityText = ""
    var secondaryActionDisabled = false
    var secondaryAction: (() -> Void)? = nil
    var actionIcon: String? = nil
    var actionAccessibilityText = ""
    var actionDisabled = false
    var action: (() -> Void)? = nil
    @Environment(\.modemDeckPadDialerAction) private var padDialerAction

    var body: some View {
        HStack {
            if let backAction {
                Button(action: backAction) {
                    Image(systemName: "chevron.left").font(.body.weight(.medium))
                        .frame(width: 44, height: 44)
                }
                .buttonStyle(.plain)
                .foregroundColor(.mdAccent)
                .accessibilityLabel(backAccessibilityText)
            }
            Text(title)
                .font(backAction == nil ? .largeTitle.weight(.bold) : .title2.weight(.bold))
                .foregroundColor(.mdText)
                .lineLimit(1).minimumScaleFactor(0.8)
                .dynamicTypeSize(...DynamicTypeSize.accessibility2)
                .accessibilityAddTraits(.isHeader)
            Spacer()
            HStack(spacing: 4) {
                if let secondaryActionIcon, let secondaryAction {
                    Button(action: secondaryAction) {
                        Image(systemName: secondaryActionIcon)
                            .font(.body.weight(.semibold))
                            .foregroundColor(.mdAccent)
                            .dynamicTypeSize(...ModemDeckLayout.headerGlyphMaximumSize)
                            .frame(width: 44, height: 44)
                    }
                    .buttonStyle(.plain)
                    .disabled(secondaryActionDisabled)
                    .opacity(secondaryActionDisabled ? 0.45 : 1)
                    .accessibilityLabel(secondaryActionAccessibilityText)
                }
                if let actionIcon, let action {
                    Button(action: action) {
                        Image(systemName: actionIcon)
                            .font(.body.weight(.semibold))
                            .foregroundColor(.mdAccent)
                            .dynamicTypeSize(...ModemDeckLayout.headerGlyphMaximumSize)
                            .frame(width: 44, height: 44)
                    }
                    .buttonStyle(.plain)
                    .disabled(actionDisabled)
                    .opacity(actionDisabled ? 0.45 : 1)
                    .accessibilityLabel(actionAccessibilityText)
                }
                if let padDialerAction {
                    Button(action: padDialerAction.open) {
                        Image(systemName: "circle.grid.3x3.fill")
                            .font(.body.weight(.medium))
                            .foregroundColor(.mdAccent)
                            .dynamicTypeSize(...ModemDeckLayout.headerGlyphMaximumSize)
                            .frame(width: 44, height: 44)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(padDialerAction.accessibilityLabel)
                }
            }
        }
        .padding(.horizontal, ModemDeckLayout.pageHorizontalPadding)
        .frame(minHeight: ModemDeckLayout.pageHeaderHeight)
        .background(Color.mdBackground)
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

struct ModemDeckSearchField: View {
    @Binding var text: String
    let prompt: String
    let clearAccessibilityText: String
    var focus: FocusState<Bool>.Binding? = nil
    @FocusState private var focused: Bool

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: "magnifyingglass")
                .font(.subheadline.weight(.medium))
                .foregroundColor(.mdMuted)
            TextField(prompt, text: $text)
                .font(.subheadline)
                .foregroundColor(.mdText)
                .disableAutocorrection(true)
                .focused(focus ?? $focused)
            if !text.isEmpty {
                Button { text = "" } label: {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundColor(.mdFaint)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(clearAccessibilityText)
            }
        }
        .padding(.horizontal, 12)
        .frame(minHeight: ModemDeckLayout.searchVisualHeight)
        .background(Color.mdSurfaceHover)
        .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.medium))
        .frame(minHeight: ModemDeckLayout.controlHitSize)
        .contentShape(Rectangle())
        .onTapGesture { (focus ?? $focused).wrappedValue = true }
    }
}

/// Search replaces the title row instead of adding another permanent toolbar.
/// The page keeps its query and filters when a retained tab is revisited.
struct ModemDeckCollectionHeader<Actions: View>: View {
    @ObservedObject var controller: ModemDeckSessionController
    let title: String
    @Binding var query: String
    var backAction: (() -> Void)? = nil
    var backAccessibilityText = ""
    @ViewBuilder var actions: () -> Actions
    @Environment(\.modemDeckPadDialerAction) private var dialerAction
    @State private var searching = false
    @FocusState private var searchFocused: Bool

    var body: some View {
        HStack(spacing: 0) {
            if searching {
                ModemDeckSearchField(text: $query, prompt: controller.text("搜索", "Search"),
                    clearAccessibilityText: controller.text("清除搜索", "Clear search"), focus: $searchFocused)
                Button(controller.text("取消", "Cancel")) {
                    searchFocused = false
                    query = ""
                    searching = false
                }
                .font(.subheadline)
                .foregroundColor(.mdAccent)
                .frame(minWidth: 44, minHeight: 44)
                .padding(.leading, 8)
                .accessibilityIdentifier("collection-search-cancel")
            } else {
                if let backAction {
                    ModemDeckCollectionHeaderButton(icon: "chevron.left", title: backAccessibilityText, action: backAction)
                }
                // Top-level collections share the large title used by Recents and Settings.
                Text(title).font(backAction == nil ? .largeTitle.weight(.bold) : .title2.weight(.bold))
                    .foregroundColor(.mdText)
                    .lineLimit(1).minimumScaleFactor(0.8)
                    .dynamicTypeSize(...DynamicTypeSize.accessibility2)
                    .accessibilityAddTraits(.isHeader)
                Spacer(minLength: 4)
                ModemDeckCollectionHeaderButton(icon: "magnifyingglass", title: controller.text("搜索", "Search")) {
                    searching = true
                    searchFocused = true
                }
                .accessibilityIdentifier("collection-search-open")
                actions()
                if let dialerAction {
                    ModemDeckCollectionHeaderButton(icon: "circle.grid.3x3.fill", title: dialerAction.accessibilityLabel, action: dialerAction.open)
                }
            }
        }
        .padding(.horizontal, ModemDeckLayout.pageHorizontalPadding)
        .frame(minHeight: ModemDeckLayout.collectionHeaderHeight)
        .background(Color.mdBackground)
        .onDisappear { searchFocused = false }
        .onChange(of: controller.selectedSection) { _ in searchFocused = false }
    }
}

struct ModemDeckCollectionHeaderButton: View {
    let icon: String
    let title: String
    var disabled = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: icon).font(.body.weight(.medium))
                .foregroundColor(.mdAccent)
                // Glyphs stop growing inside their fixed hit area so neighbours never overlap.
                .dynamicTypeSize(...ModemDeckLayout.headerGlyphMaximumSize)
                .frame(width: 44, height: 44).contentShape(Rectangle())
        }
        .buttonStyle(.plain).disabled(disabled)
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
            Image(systemName: "ellipsis")
                .font(.body.weight(.semibold))
                .foregroundColor(.mdAccent)
                .frame(width: 44, height: 44)
                .contentShape(Rectangle())
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
    private let controller: ModemDeckSessionController?
    private let actions: Actions
    @Environment(\.modemDeckBottomChrome) private var chrome

    init(
        part: ModemDeckBatchActionBarPart = .toolbar,
        controller: ModemDeckSessionController? = nil,
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
        self.controller = controller
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

    private var header: some View {
        HStack(spacing: 12) {
            Button(allSelected ? clearAllText : selectAllText, action: selectAll)
                .disabled(busy || totalCount == 0)
                .frame(minWidth: 44, minHeight: 44, alignment: .leading)
            Spacer(minLength: 8)
            HStack(spacing: 7) {
                if busy { ProgressView().controlSize(.small) }
                Text(String(format: selectedText, selectedCount))
                    .font(.headline)
                    .foregroundColor(.mdText)
                    .lineLimit(1)
                    .accessibilityAddTraits(.isHeader)
            }
            Spacer(minLength: 8)
            Button(doneText, action: done)
                .fontWeight(.semibold)
                .disabled(busy)
                .frame(minWidth: 44, minHeight: 44, alignment: .trailing)
                .accessibilityIdentifier("selection-done")
        }
        .font(.body)
        .foregroundColor(.mdAccent)
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
        .padding(.horizontal, ModemDeckLayout.pageHorizontalPadding)
        .frame(minHeight: ModemDeckLayout.collectionHeaderHeight)
        .background(Color.mdBackground)
        .overlay(alignment: .bottom) { ModemDeckHairline() }
    }

    /// On iPhone the toolbar takes the hidden tab bar's slot; it reserves no extra space,
    /// because the page already keeps that slot clear.
    private var toolbar: some View {
        let slot = max(0, chrome.tabBar - chrome.miniCallBar)
        return HStack(alignment: .top, spacing: 4) {
            actions
        }
        .padding(.horizontal, 8)
        .padding(.top, 6)
        .padding(.bottom, slot > 0 ? 0 : 6)
        .frame(minHeight: max(slot, 56), alignment: .top)
        .frame(maxWidth: .infinity)
        .background(.bar, ignoresSafeAreaEdges: .bottom)
        .overlay(alignment: .top) { ModemDeckHairline() }
        .padding(.bottom, -slot)
        .onAppear { controller?.enterImmersiveScreen() }
        .onDisappear { controller?.leaveImmersiveScreen() }
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
