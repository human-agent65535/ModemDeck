import SwiftUI
import UIKit

extension Color {
    static let mdBackground = Color(red: 0.965, green: 0.969, blue: 0.976)
    static let mdSurface = Color.white
    static let mdSurfaceSubtle = Color(red: 0.980, green: 0.984, blue: 0.988)
    static let mdSurfaceHover = Color(red: 0.941, green: 0.953, blue: 0.961)
    static let mdSelected = Color(red: 0.902, green: 0.953, blue: 0.941)
    static let mdText = Color(red: 0.114, green: 0.161, blue: 0.224)
    static let mdMuted = Color(red: 0.400, green: 0.439, blue: 0.514)
    static let mdFaint = Color(red: 0.596, green: 0.635, blue: 0.702)
    static let mdBorder = Color(red: 0.894, green: 0.906, blue: 0.925)
    static let mdAccent = Color(red: 0.067, green: 0.471, blue: 0.392)
    static let mdAccentStrong = Color(red: 0.047, green: 0.384, blue: 0.310)
    static let mdAccentSoft = Color(red: 0.875, green: 0.945, blue: 0.929)
    static let mdBlue = Color(red: 0.145, green: 0.388, blue: 0.663)
    static let mdBlueSoft = Color(red: 0.918, green: 0.949, blue: 0.984)
    static let mdDanger = Color(red: 0.769, green: 0.239, blue: 0.294)
    static let modemDeckTint = mdAccent
}

private struct ModemDeckInteractivePopSupport: UIViewControllerRepresentable {
    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        weak var navigationController: UINavigationController?

        func install(on navigationController: UINavigationController?) {
            guard let navigationController,
                  let gesture = navigationController.interactivePopGestureRecognizer else { return }
            self.navigationController = navigationController
            gesture.delegate = self
            gesture.isEnabled = true
        }

        func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
            (navigationController?.viewControllers.count ?? 0) > 1
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

    @ViewBuilder
    func body(content: Content) -> some View {
        if items.isEmpty && actions.isEmpty {
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
        let language = session?.language ?? Locale.preferredLanguages.first ?? "en"
        return language.lowercased().hasPrefix("zh")
    }

    func text(_ chinese: String, _ english: String) -> String {
        usesChinese ? chinese : english
    }

    func dateText(_ value: String) -> String {
        ModemDeckDateText.short(
            value,
            locale: Locale(identifier: usesChinese ? "zh_CN" : "en_US")
        )
    }

    func compactDateText(_ value: String) -> String {
        ModemDeckDateText.compact(
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

    static func duration(_ seconds: Int) -> String {
        let safe = max(0, seconds)
        return String(format: "%d:%02d", safe / 60, safe % 60)
    }
}

enum ModemDeckLayout {
    static let padListWidth: CGFloat = 360
    static let splitWorkspaceMinimumWidth: CGFloat = 780
    static let pageHorizontalPadding: CGFloat = 14
    static let pageHeaderHeight: CGFloat = 52
    static let toolbarHorizontalPadding: CGFloat = 10
    static let toolbarVerticalPadding: CGFloat = 4
    static let toolbarGap: CGFloat = 6
    static let controlHitSize: CGFloat = 44
    static let controlVisualSize: CGFloat = 36
    static let searchVisualHeight: CGFloat = 38
    static let listHorizontalPadding: CGFloat = 14
    static let listRowMinHeight: CGFloat = 66
    static let listAvatarSize: CGFloat = 40
    static var isPad: Bool { UIDevice.current.userInterfaceIdiom == .pad }
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

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        _callController = ObservedObject(wrappedValue: controller.callController)
    }

    var body: some View {
        ZStack {
            content
                .allowsHitTesting(callController.call == nil)
                .accessibilityHidden(callController.call != nil)

            if let call = callController.call {
                ModemDeckActiveCallView(
                    call: call,
                    controller: controller,
                    callController: callController
                )
                .accessibilityElement(children: .contain)
                .accessibilityAddTraits(.isModal)
                .transition(reduceMotion ? .identity : .opacity)
                .zIndex(20)
            }
        }
        .preferredColorScheme(callController.call == nil ? .light : .dark)
        .accentColor(.mdAccent)
        .textSelection(.enabled)
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
            ModemDeckAdaptiveShell(controller: controller)
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
        .background(Color.orange.opacity(0.16))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("connection-offline")
        .overlay(alignment: .bottom) {
            Rectangle().fill(Color.orange.opacity(0.25)).frame(height: 1)
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
                .foregroundColor(.orange)
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
        allCases.filter { $0 != .dial }
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

private enum ModemDeckDialerMotion {
    static let animation = Animation.timingCurve(
        0.2,
        0.9,
        0.25,
        1,
        duration: 0.22
    )

    static let phoneTransition = AnyTransition.opacity
        .combined(with: .offset(y: 12))
        .combined(with: .scale(scale: 0.99, anchor: .bottom))

    static let padTransition = AnyTransition.opacity
        .combined(with: .offset(x: 12))
        .combined(with: .scale(scale: 0.99, anchor: .bottomTrailing))
}

private struct ModemDeckSectionTabs: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Environment(\.modemDeckUsesSplitWorkspace) private var usesSplitWorkspace
    @Environment(\.modemDeckPadDialerAction) private var padDialerAction

    var body: some View {
        ModemDeckLazySectionHost(
            controller: controller,
            activeSection: activeSection,
            usesSplitWorkspace: usesSplitWorkspace,
            padDialerAction: padDialerAction
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

private struct ModemDeckSectionRoot: View {
    let section: ModemDeckSection
    @ObservedObject var controller: ModemDeckSessionController
    let usesSplitWorkspace: Bool
    let padDialerAction: ModemDeckPadDialerAction?
    @State private var path: [ModemDeckRoute] = []

    var body: some View {
        NavigationStack(path: $path) {
            destination
                .navigationDestination(for: ModemDeckRoute.self) { route in
                    ModemDeckRouteContent(route: route, controller: controller)
                }
        }
        .environment(\.modemDeckNavigate, { path.append($0) })
        .environment(\.modemDeckUsesSplitWorkspace, usesSplitWorkspace)
        .environment(\.modemDeckPadDialerAction, padDialerAction)
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
    final class Coordinator {
        var sections: [ModemDeckSection: UIHostingController<ModemDeckSectionRoot>] = [:]
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
            let actionAvailabilityChanged = !isEnvironmentConfigured ||
                (self.padDialerAction == nil) != (padDialerAction == nil)
            self.usesSplitWorkspace = usesSplitWorkspace
            if actionAvailabilityChanged || self.padDialerAction == nil {
                self.padDialerAction = padDialerAction
            }
            isEnvironmentConfigured = true
            guard layoutChanged || actionAvailabilityChanged else { return }
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
            ModemDeckSectionRoot(
                section: section,
                controller: controller,
                usesSplitWorkspace: usesSplitWorkspace,
                padDialerAction: padDialerAction
            )
        }
    }

    @ObservedObject var controller: ModemDeckSessionController
    let activeSection: ModemDeckSection
    let usesSplitWorkspace: Bool
    let padDialerAction: ModemDeckPadDialerAction?

    func makeCoordinator() -> Coordinator { Coordinator() }

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

    var body: some View {
        GeometryReader { geometry in
            let usesSplitWorkspace = ModemDeckLayout.isPad &&
                geometry.size.width >= ModemDeckLayout.splitWorkspaceMinimumWidth
            Group {
                if usesSplitWorkspace {
                    ModemDeckPadShell(controller: controller)
                } else {
                    ModemDeckPhoneShell(controller: controller)
                }
            }
            .environment(\.modemDeckUsesSplitWorkspace, usesSplitWorkspace)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

struct ModemDeckPhoneShell: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var showingDialer: Bool

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        let initial = ModemDeckSection.initialSection
        _showingDialer = State(initialValue: initial == .dial)
    }

    var body: some View {
        VStack(spacing: 0) {
            ZStack(alignment: .bottom) {
                ModemDeckSectionTabs(controller: controller)

                if showingDialer {
                    Color.black.opacity(0.16)
                        .ignoresSafeArea()
                        .onTapGesture { closeDialer() }
                        .transition(.opacity)
                }

                GeometryReader { geometry in
                    if showingDialer {
                        VStack(spacing: 0) {
                            Spacer(minLength: 8)
                            ModemDeckDialerPanel(controller: controller, close: closeDialer)
                                .frame(
                                    width: geometry.size.width,
                                    height: min(720, max(0, geometry.size.height - 8))
                                )
                        }
                        .transition(ModemDeckDialerMotion.phoneTransition)
                    }
                }
                .allowsHitTesting(showingDialer)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)

            ModemDeckPhoneTabBar(
                controller: controller,
                selection: Binding(
                    get: { controller.selectedSection },
                    set: { controller.selectedSection = $0 }
                ),
                showingDialer: $showingDialer
            )
        }
        .background(Color.mdSurface.ignoresSafeArea(edges: .bottom))
    }

    private func closeDialer() {
        withAnimation(dialerAnimation) { showingDialer = false }
    }

    private var dialerAnimation: Animation? {
        reduceMotion ? nil : ModemDeckDialerMotion.animation
    }

}

private struct ModemDeckPhoneTabBar: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Binding var selection: ModemDeckSection
    @Binding var showingDialer: Bool

    var body: some View {
        HStack(spacing: 0) {
            ForEach(ModemDeckPhoneTab.allCases) { tab in
                Button {
                    if tab == .dial {
                        guard !showingDialer else { return }
                        withAnimation(dialerAnimation) { showingDialer = true }
                    } else {
                        withAnimation(dialerAnimation) { showingDialer = false }
                        if let section = tab.section {
                            selection = section
                        }
                    }
                } label: {
                    if tab == .dial {
                        VStack(spacing: 2) {
                            ModemDeckLucideIcon(
                                asset: ModemDeckLucideAsset.phoneCall,
                                size: 23
                            )
                                .foregroundColor(.white)
                                .frame(width: 52, height: 52)
                                .background(showingDialer ? Color.mdAccentStrong : Color.mdAccent)
                                .clipShape(Circle())
                                .overlay(Circle().stroke(Color.mdSurface, lineWidth: 5))
                                .shadow(
                                    color: Color.black.opacity(0.13),
                                    radius: 6,
                                    y: 2
                                )
                                .scaleEffect(showingDialer ? 0.96 : 1)
                            Text(title(for: tab))
                                .font(.caption2.weight(.medium))
                                .foregroundColor(.mdAccent)
                        }
                        .offset(y: -10)
                    } else {
                        VStack(spacing: 4) {
                            ModemDeckLucideIcon(asset: tab.icon, size: 21)
                                .frame(height: 25)
                            Text(title(for: tab))
                                .font(.caption2.weight(.medium))
                                .lineLimit(1)
                                .minimumScaleFactor(0.55)
                        }
                        .foregroundColor(isSelected(tab) ? .mdAccent : .mdMuted)
                    }
                }
                .buttonStyle(.plain)
                .frame(maxWidth: .infinity, minHeight: 66)
                .accessibilityLabel(title(for: tab))
                .accessibilityIdentifier("section-\(tab.id)")
                .accessibilityValue(isSelected(tab) ? controller.text("已选择", "Selected") : "")
                .accessibilityAddTraits(isSelected(tab) ? .isSelected : [])
                .zIndex(tab == .dial ? 1 : 0)
            }
        }
        .dynamicTypeSize(...DynamicTypeSize.large)
        .background(Color.mdSurface)
        .background(alignment: .top) {
            Rectangle().fill(Color.mdBorder).frame(height: 1)
        }
    }

    private var dialerAnimation: Animation? {
        reduceMotion ? nil : ModemDeckDialerMotion.animation
    }

    private func isSelected(_ tab: ModemDeckPhoneTab) -> Bool {
        guard !showingDialer else { return tab == .dial }
        return tab.section == selection
    }

    private func title(for tab: ModemDeckPhoneTab) -> String {
        switch tab {
        case .home: return controller.text("首页", "Home")
        case .contacts: return controller.text("联系人", "Contacts")
        case .messages: return controller.text("消息", "Messages")
        case .dial: return controller.text("拨号", "Dial")
        case .calls: return controller.text("通话", "Calls")
        case .recordings: return controller.text("录音", "Recordings")
        case .settings: return controller.text("设置", "Settings")
        }
    }
}

private enum ModemDeckPhoneTab: String, CaseIterable, Identifiable {
    case home
    case contacts
    case messages
    case dial
    case calls
    case recordings
    case settings

    var id: String { rawValue }

    var section: ModemDeckSection? {
        switch self {
        case .home: return .home
        case .contacts: return .contacts
        case .messages: return .messages
        case .calls: return .calls
        case .recordings: return .recordings
        case .settings: return .settings
        case .dial: return nil
        }
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

struct ModemDeckPadShell: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var showingDialer: Bool

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        let initial = ModemDeckSection.initialSection
        _showingDialer = State(initialValue: initial == .dial)
    }

    var body: some View {
        ZStack(alignment: .bottomTrailing) {
            HStack(spacing: 0) {
                ModemDeckPadNavigationRail(
                    controller: controller,
                    selection: Binding(
                        get: { controller.selectedSection },
                        set: { controller.selectedSection = $0 }
                    ),
                    showingDialer: $showingDialer
                )
                Rectangle()
                    .fill(Color.mdBorder)
                    .frame(width: 1)
                ModemDeckSectionTabs(controller: controller)
                .environment(
                    \.modemDeckPadDialerAction,
                    ModemDeckPadDialerAction(
                        accessibilityLabel: controller.text("打开拨号盘", "Open dialer"),
                        open: openDialer
                    )
                )
            }

            if showingDialer {
                ModemDeckDialerPanel(
                    controller: controller,
                    close: closeDialer,
                    floating: true
                )
                .frame(width: 390, height: 700)
                .padding(16)
                .transition(ModemDeckDialerMotion.padTransition)
                .zIndex(10)
            }
        }
        .background(Color.mdBackground.ignoresSafeArea())
        .accentColor(.mdAccent)
    }

    private func openDialer() {
        guard !showingDialer else { return }
        withAnimation(dialerAnimation) { showingDialer = true }
    }

    private func closeDialer() {
        withAnimation(dialerAnimation) { showingDialer = false }
    }

    private var dialerAnimation: Animation? {
        reduceMotion ? nil : ModemDeckDialerMotion.animation
    }

}

private struct ModemDeckPadNavigationRail: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Binding var selection: ModemDeckSection
    @Binding var showingDialer: Bool

    var body: some View {
        VStack(spacing: 4) {
            HStack(spacing: 5) {
                Text("M")
                    .font(.headline.weight(.bold))
                    .foregroundColor(.white)
                    .frame(width: 34, height: 34)
                    .background(Color.mdAccent)
                    .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                Text("Modem\nDeck")
                    .font(.caption2.weight(.semibold))
                    .foregroundColor(.mdText)
                    .multilineTextAlignment(.leading)
                    .lineSpacing(-1)
            }
            .frame(height: 54)

            ForEach(ModemDeckSection.allCases) { section in
                Button {
                    if section == .dial {
                        withAnimation(dialerAnimation) { showingDialer.toggle() }
                    } else {
                        withAnimation(dialerAnimation) { showingDialer = false }
                        selection = section
                    }
                } label: {
                    VStack(spacing: 4) {
                        ModemDeckLucideIcon(asset: section.icon, size: 21)
                            .frame(height: 24)
                        Text(title(for: section))
                            .font(.caption2.weight(.medium))
                            .lineLimit(1)
                            .minimumScaleFactor(0.75)
                    }
                    .foregroundColor(isSelected(section) ? .mdAccentStrong : .mdMuted)
                    .frame(width: 78, height: 62)
                    .background(isSelected(section) ? Color.mdSelected : Color.clear)
                    .clipShape(RoundedRectangle(cornerRadius: 9, style: .continuous))
                    .overlay(alignment: .leading) {
                        if isSelected(section) {
                            RoundedRectangle(cornerRadius: 1.5)
                                .fill(Color.mdAccent)
                                .frame(width: 3, height: 28)
                        }
                    }
                }
                .buttonStyle(.plain)
                .accessibilityLabel(title(for: section))
                .accessibilityIdentifier("section-\(section.id)")
                .accessibilityValue(
                    isSelected(section) ? controller.text("已选择", "Selected") : ""
                )
                .accessibilityAddTraits(isSelected(section) ? .isSelected : [])
            }

            Spacer()
        }
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
        .padding(.horizontal, 7)
        .frame(width: 92)
        .background(Color.mdSurface.ignoresSafeArea())
    }

    private var dialerAnimation: Animation? {
        reduceMotion ? nil : ModemDeckDialerMotion.animation
    }

    private func isSelected(_ section: ModemDeckSection) -> Bool {
        section == .dial ? showingDialer : (!showingDialer && selection == section)
    }

    private func title(for section: ModemDeckSection) -> String {
        switch section {
        case .home: return controller.text("首页", "Home")
        case .contacts: return controller.text("联系人", "Contacts")
        case .messages: return controller.text("消息", "Messages")
        case .dial: return controller.text("拨号", "Dial")
        case .calls: return controller.text("通话", "Calls")
        case .recordings: return controller.text("录音", "Recordings")
        case .settings: return controller.text("设置", "Settings")
        }
    }
}


struct ModemDeckHomeLineGrid: View {
    @ObservedObject var controller: ModemDeckSessionController
    let lines: [ModemDeckLine]

    private let columns = [
        GridItem(.adaptive(minimum: 168, maximum: 280), spacing: 8)
    ]

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
                .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .stroke(Color.mdBorder, lineWidth: 1)
                )
            } else {
                LazyVGrid(columns: columns, alignment: .leading, spacing: 8) {
                    ForEach(lines) { line in
                        ModemDeckHomeLineCard(controller: controller, line: line)
                    }
                }
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
                .background(line.tint.opacity(0.1))
                .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))

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
        .frame(maxWidth: .infinity, minHeight: 62, alignment: .leading)
        .background(Color.mdSurface)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(Color.mdBorder, lineWidth: 1)
        )
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
    var actionIcon: String? = nil
    var actionAccessibilityText = ""
    var actionDisabled = false
    var action: (() -> Void)? = nil
    @Environment(\.modemDeckPadDialerAction) private var padDialerAction

    var body: some View {
        HStack {
            Text(title)
                .font(.headline)
                .foregroundColor(.mdText)
                .dynamicTypeSize(...DynamicTypeSize.accessibility2)
            Spacer()
            HStack(spacing: 4) {
                if let actionIcon, let action {
                    Button(action: action) {
                        Image(systemName: actionIcon)
                            .font(.body.weight(.semibold))
                            .foregroundColor(.mdAccent)
                            .frame(width: 44, height: 44)
                    }
                    .buttonStyle(.plain)
                    .disabled(actionDisabled)
                    .opacity(actionDisabled ? 0.45 : 1)
                    .accessibilityLabel(actionAccessibilityText)
                }
                if let padDialerAction {
                    Button(action: padDialerAction.open) {
                        ZStack {
                            RoundedRectangle(cornerRadius: 8, style: .continuous)
                                .fill(Color.mdSurfaceSubtle)
                                .overlay(
                                    RoundedRectangle(cornerRadius: 8, style: .continuous)
                                        .stroke(Color.mdBorder, lineWidth: 1)
                                )
                            Image(systemName: "phone.arrow.up.right")
                                .font(.subheadline.weight(.semibold))
                                .foregroundColor(.mdAccent)
                        }
                        .frame(
                            width: ModemDeckLayout.controlVisualSize,
                            height: ModemDeckLayout.controlVisualSize
                        )
                        .frame(
                            width: ModemDeckLayout.controlHitSize,
                            height: ModemDeckLayout.controlHitSize
                        )
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(padDialerAction.accessibilityLabel)
                }
            }
        }
        .padding(.horizontal, ModemDeckLayout.pageHorizontalPadding)
        .frame(minHeight: ModemDeckLayout.pageHeaderHeight)
        .background(Color.mdSurface)
        .overlay(alignment: .bottom) {
            Rectangle().fill(Color.mdBorder).frame(height: 1)
        }
    }
}

struct ModemDeckSearchField: View {
    @Binding var text: String
    let prompt: String
    let clearAccessibilityText: String
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
                .focused($focused)
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
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
        .frame(minHeight: ModemDeckLayout.controlHitSize)
        .contentShape(Rectangle())
        .onTapGesture { focused = true }
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
                RoundedRectangle(cornerRadius: 9, style: .continuous)
                    .fill(active ? Color.mdAccentSoft : Color.mdSurface)
                    .overlay(
                        RoundedRectangle(cornerRadius: 9, style: .continuous)
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

    var body: some View {
        HStack(spacing: 2) {
            ForEach(options) { option in
                Button {
                    selection = option.id
                } label: {
                    ZStack {
                        if selection == option.id {
                            RoundedRectangle(cornerRadius: 8, style: .continuous)
                                .fill(Color.mdSurface)
                                .padding(.vertical, 4)
                        }
                        Text(option.title)
                            .font(.caption.weight(selection == option.id ? .semibold : .medium))
                            .foregroundColor(selection == option.id ? .mdText : .mdMuted)
                            .lineLimit(1)
                            .minimumScaleFactor(0.85)
                    }
                    .frame(maxWidth: .infinity, minHeight: ModemDeckLayout.controlHitSize)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(selection == option.id ? .isSelected : [])
            }
        }
        .frame(height: ModemDeckLayout.controlHitSize)
        .background(Color.mdSurfaceHover)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
    }
}

extension View {
    func modemDeckListToolbar(showsDivider: Bool = false) -> some View {
        padding(.horizontal, ModemDeckLayout.toolbarHorizontalPadding)
            .padding(.vertical, ModemDeckLayout.toolbarVerticalPadding)
            .background(Color.mdSurfaceSubtle)
            .overlay(alignment: .bottom) {
                if showsDivider {
                    Rectangle().fill(Color.mdBorder).frame(height: 1)
                }
            }
    }

    func modemDeckListFilterBar() -> some View {
        padding(.horizontal, ModemDeckLayout.toolbarHorizontalPadding)
            .padding(.bottom, ModemDeckLayout.toolbarVerticalPadding)
            .background(Color.mdSurfaceSubtle)
            .overlay(alignment: .bottom) {
                Rectangle().fill(Color.mdBorder).frame(height: 1)
            }
    }
}

struct ModemDeckLineTag: View {
    let line: ModemDeckLine

    var body: some View {
        Text(line.displayName)
            .font(.caption.weight(.semibold))
            .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
            .foregroundColor(line.tint)
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .frame(minHeight: 23)
            .background(line.tint.opacity(0.09))
            .clipShape(RoundedRectangle(cornerRadius: 4, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 4, style: .continuous)
                    .stroke(line.tint.opacity(0.55), lineWidth: 1)
            )
    }
}

struct ModemDeckListDivider: View {
    var leading: CGFloat = 72

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
            .frame(width: 24)
    }
}

struct ModemDeckBatchActionBar<Actions: View>: View {
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
        VStack(spacing: 10) {
            HStack(spacing: 12) {
                HStack(spacing: 7) {
                    if busy {
                        ProgressView()
                            .controlSize(.small)
                    }
                    Text(String(format: selectedText, selectedCount))
                        .font(.footnote.weight(.bold))
                        .foregroundColor(.mdText)
                        .lineLimit(1)
                }
                .padding(.horizontal, 10)
                .frame(minHeight: 30)
                .background(Color.mdSelected)
                .clipShape(Capsule())
                Spacer(minLength: 8)
                Button(allSelected ? clearAllText : selectAllText, action: selectAll)
                    .font(.footnote.weight(.semibold))
                    .foregroundColor(.mdAccent)
                    .disabled(busy || totalCount == 0)
                Button(doneText, action: done)
                    .font(.footnote.weight(.semibold))
                    .foregroundColor(.mdAccent)
                    .disabled(busy)
            }

            HStack(spacing: 8) {
                actions
            }
        }
        .padding(12)
        .background(Color.mdSurface)
        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .stroke(Color.mdBorder, lineWidth: 1)
        )
        .shadow(color: Color.black.opacity(0.10), radius: 14, y: 4)
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(Color.mdBackground)
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
            VStack(spacing: 4) {
                Image(systemName: icon)
                    .font(.body.weight(.semibold))
                Text(title)
                    .font(.caption.weight(.semibold))
                    .lineLimit(1)
                    .minimumScaleFactor(0.78)
            }
            .foregroundColor(destructive ? .mdDanger : .mdText)
            .frame(maxWidth: .infinity, minHeight: 52)
            .background(destructive ? Color.mdDanger.opacity(0.07) : Color.mdSurfaceHover)
            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .stroke(destructive ? Color.mdDanger.opacity(0.18) : Color.mdBorder, lineWidth: 1)
            )
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .opacity(disabled ? 0.5 : 1)
    }
}

extension ModemDeckLine {
    var tint: Color {
        switch lineColor {
        case "blue": return .mdBlue
        case "indigo": return .indigo
        case "violet": return .purple
        case "green", "teal": return .mdAccent
        case "amber", "orange": return .orange
        case "red": return .mdDanger
        default: return .mdAccent
        }
    }
}

struct ModemDeckStateView: View {
    let icon: String
    let title: String
    let detail: String

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: icon)
                .font(.largeTitle)
                .foregroundColor(.mdFaint)
            Text(title)
                .font(.headline)
                .foregroundColor(.mdText)
            Text(detail)
                .font(.subheadline)
                .foregroundColor(.mdMuted)
                .multilineTextAlignment(.center)
        }
        .padding(28)
        .frame(maxWidth: .infinity)
    }
}

struct ModemDeckWorkspaceEmptyView: View {
    let icon: String
    let title: String
    var detail = ""

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: icon)
                .font(.title.weight(.medium))
                .foregroundColor(.mdFaint)
            Text(title)
                .font(.subheadline.weight(.semibold))
                .foregroundColor(.mdMuted)
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
                .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
        }
    }
}
