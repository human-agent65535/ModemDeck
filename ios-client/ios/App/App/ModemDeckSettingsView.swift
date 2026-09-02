import SwiftUI
import UIKit

private enum ModemDeckSettingsSection: String, Identifiable {
    case preferences
    case security
    case notifications
    case calls
    case pairing
    case contacts
    case users
    case devices
    case telegram
    case access
    case diagnostics
    case about

    var id: String { rawValue }
}

struct ModemDeckSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Environment(\.modemDeckUsesSplitWorkspace) private var usesSplitWorkspace
    @State private var selectedSection: ModemDeckSettingsSection = .preferences

    private var usesTwoColumns: Bool {
        usesSplitWorkspace
    }

    var body: some View {
        VStack(spacing: 0) {
            ModemDeckPageHeader(title: controller.text("设置", "Settings"))
            if usesTwoColumns {
                HStack(spacing: 0) {
                    settingsDirectory
                        .frame(width: 370)
                    Rectangle().fill(Color.mdBorder).frame(width: 1)
                    settingsDestination(selectedSection)
                        .environment(\.modemDeckSettingsShowsBackButton, false)
                        .id(selectedSection)
                }
            } else {
                settingsDirectory
            }
        }
        .background(Color.mdBackground)
        .navigationBarHidden(true)
        .task {
            await controller.refreshNotificationStatus()
            controller.refreshMicrophoneStatus()
        }
    }

    private var settingsDirectory: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                settingsGroupLabel(controller.text("个人", "Personal"))
                directoryItem(
                    .preferences,
                    icon: "slider.horizontal.3",
                    title: controller.text("偏好设置", "Preferences"),
                    subtitle: controller.text("语言、默认线路与账户资料", "Language, default line, and profile")
                )
                ModemDeckListDivider(leading: 74)
                directoryItem(
                    .security,
                    icon: "person.badge.key",
                    title: controller.text("账户与安全", "Account & Security"),
                    subtitle: controller.text("密码、权限与 Apple 设备", "Password, access, and Apple devices")
                )
                ModemDeckListDivider(leading: 74)
                directoryItem(
                    .notifications,
                    icon: "bell.badge",
                    title: controller.text("通知", "Notifications"),
                    subtitle: notificationSubtitle
                )
                ModemDeckListDivider(leading: 74)
                directoryItem(
                    .calls,
                    icon: "phone.connection",
                    title: controller.text("来电与音频", "Calls & Audio"),
                    subtitle: controller.text("CallKit、麦克风与通话录音", "CallKit, microphone, and call recording")
                )
                ModemDeckListDivider(leading: 74)
                directoryItem(
                    .pairing,
                    icon: "qrcode",
                    title: controller.text("配对", "Pairing"),
                    subtitle: controller.serverDisplayName
                )
                ModemDeckListDivider(leading: 74)
                directoryItem(
                    .contacts,
                    icon: "person.crop.circle.badge.plus",
                    title: controller.text("联系人", "Contacts"),
                    subtitle: controller.text("从本机通讯录导入", "Import from this device")
                )

                settingsGroupLabel(controller.text("管理", "Management"))
                if controller.session?.role == "admin" {
                    directoryItem(
                        .users,
                        icon: "person.3",
                        title: controller.text("用户", "Users"),
                        subtitle: controller.text("账户、线路权限与 Apple 设备", "Accounts, line access, and Apple devices")
                    )
                    ModemDeckListDivider(leading: 74)
                }
                directoryItem(
                    .devices,
                    icon: "simcard.2",
                    title: controller.text("线路与设备", "Lines & Devices"),
                    subtitle: controller.text("状态、信号、型号与固件", "Status, signal, model, and firmware")
                )
                ModemDeckListDivider(leading: 74)
                directoryItem(
                    .telegram,
                    icon: "paperplane",
                    title: "Telegram",
                    subtitle: controller.text("通知机器人与线路范围", "Notification bots and line scopes")
                )
                if controller.session?.role == "admin" {
                    ModemDeckListDivider(leading: 74)
                    directoryItem(
                        .access,
                        icon: "network.badge.shield.half.filled",
                        title: controller.text("访问", "Access"),
                        subtitle: controller.text("API 连接、凭据与 Apple 推送", "API connection, credential, and Apple push")
                    )
                    ModemDeckListDivider(leading: 74)
                    directoryItem(
                        .diagnostics,
                        icon: "stethoscope",
                        title: controller.text("诊断", "Diagnostics"),
                        subtitle: controller.text("数据库、硬件服务与通话运行状态", "Database, hardware service, and call runtime")
                    )
                    ModemDeckListDivider(leading: 74)
                    directoryItem(
                        .about,
                        icon: "info.circle",
                        title: controller.text("关于", "About"),
                        subtitle: "ModemDeck \(ModemDeckDeviceInfo.appVersion)"
                    )
                }

                settingsGroupLabel(controller.text("当前账户", "Current Account"))
                HStack(spacing: 13) {
                    Image(systemName: "person.crop.circle.fill")
                        .font(.title3)
                        .foregroundColor(.mdMuted)
                        .frame(
                            width: ModemDeckLayout.controlVisualSize,
                            height: ModemDeckLayout.controlVisualSize
                        )
                        .background(Color.mdSurfaceHover)
                        .clipShape(Circle())
                    VStack(alignment: .leading, spacing: 3) {
                        Text(controller.session?.username ?? "—")
                            .font(.subheadline.weight(.semibold))
                            .foregroundColor(.mdText)
                        Text(controller.session?.role == "admin"
                             ? controller.text("管理员账户", "Administrator account")
                             : controller.text("成员账户", "Member account"))
                            .font(.footnote)
                            .foregroundColor(.mdMuted)
                    }
                    Spacer()
                }
                .padding(.horizontal, ModemDeckLayout.listHorizontalPadding)
                .frame(minHeight: ModemDeckLayout.listRowMinHeight)
                .background(Color.mdSurface)
            }
        }
        .background(Color.mdSurface)
    }

    @ViewBuilder
    private func directoryItem(
        _ section: ModemDeckSettingsSection,
        icon: String,
        title: String,
        subtitle: String
    ) -> some View {
        if usesTwoColumns {
            Button { selectedSection = section } label: {
                ModemDeckSettingsDirectoryRow(
                    icon: icon,
                    title: title,
                    subtitle: subtitle,
                    selected: selectedSection == section
                )
            }
            .buttonStyle(.plain)
        } else {
            NavigationLink {
                settingsDestination(section)
            } label: {
                ModemDeckSettingsDirectoryRow(icon: icon, title: title, subtitle: subtitle)
            }
            .buttonStyle(.plain)
        }
    }

    @ViewBuilder
    private func settingsDestination(_ section: ModemDeckSettingsSection) -> some View {
        switch section {
        case .preferences: ModemDeckPreferencesSettingsView(controller: controller)
        case .security: ModemDeckSecuritySettingsView(controller: controller)
        case .notifications: ModemDeckNotificationSettingsView(controller: controller)
        case .calls: ModemDeckCallSettingsView(controller: controller)
        case .pairing: ModemDeckConnectionSettingsView(controller: controller)
        case .contacts: ModemDeckContactImportSettingsView(controller: controller)
        case .users: ModemDeckUsersSettingsView(controller: controller)
        case .devices: ModemDeckDevicesSettingsView(controller: controller)
        case .telegram: ModemDeckTelegramSettingsView(controller: controller)
        case .access: ModemDeckAccessSettingsView(controller: controller)
        case .diagnostics: ModemDeckDiagnosticsSettingsView(controller: controller)
        case .about: ModemDeckAboutSettingsView(controller: controller)
        }
    }

    private var notificationSubtitle: String {
        switch controller.notificationStatus {
        case "authorized": return controller.text("已允许", "Allowed")
        case "provisional": return controller.text("临时允许", "Provisional")
        case "denied": return controller.text("已关闭", "Off")
        default: return controller.text("管理推送通知", "Manage push notifications")
        }
    }

    private func settingsGroupLabel(_ title: String) -> some View {
        Text(title.uppercased())
            .font(.caption2.weight(.bold))
            .tracking(0.7)
            .foregroundColor(.mdFaint)
            .padding(.horizontal, ModemDeckLayout.listHorizontalPadding)
            .padding(.top, 16)
            .padding(.bottom, 6)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.mdBackground)
    }
}

private struct ModemDeckSettingsDirectoryRow: View {
    let icon: String
    let title: String
    let subtitle: String
    var selected = false

    var body: some View {
        HStack(spacing: 11) {
            Image(systemName: icon)
                .font(.subheadline.weight(.semibold))
                .foregroundColor(.mdBlue)
                .frame(
                    width: ModemDeckLayout.controlVisualSize,
                    height: ModemDeckLayout.controlVisualSize
                )
                .background(Color.mdBlueSoft)
                .clipShape(RoundedRectangle(cornerRadius: 9, style: .continuous))
            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(.subheadline.weight(.semibold))
                    .foregroundColor(.mdText)
                Text(subtitle)
                    .font(.footnote)
                    .foregroundColor(.mdMuted)
                    .lineLimit(2)
            }
            Spacer(minLength: 10)
            Image(systemName: "chevron.right")
                .font(.system(size: 13, weight: .semibold))
                .foregroundColor(.mdFaint)
        }
        .padding(.horizontal, ModemDeckLayout.listHorizontalPadding)
        .frame(minHeight: ModemDeckLayout.listRowMinHeight)
        .background(selected ? Color.mdSelected : Color.clear)
        .overlay(alignment: .leading) {
            if selected {
                Capsule()
                    .fill(Color.mdAccent)
                    .frame(width: 3, height: 34)
                    .padding(.leading, 4)
            }
        }
        .contentShape(Rectangle())
    }
}

private struct ModemDeckSettingsShowsBackButtonKey: EnvironmentKey {
    static let defaultValue = true
}

private extension EnvironmentValues {
    var modemDeckSettingsShowsBackButton: Bool {
        get { self[ModemDeckSettingsShowsBackButtonKey.self] }
        set { self[ModemDeckSettingsShowsBackButtonKey.self] = newValue }
    }
}

private struct ModemDeckSettingsDetailScaffold<Content: View>: View {
    let title: String
    let backTitle: String
    let content: Content
    @Environment(\.presentationMode) private var presentationMode
    @Environment(\.modemDeckSettingsShowsBackButton) private var showsBackButton

    init(title: String, backTitle: String, @ViewBuilder content: () -> Content) {
        self.title = title
        self.backTitle = backTitle
        self.content = content()
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                if showsBackButton {
                    Button { presentationMode.wrappedValue.dismiss() } label: {
                        Image(systemName: "chevron.left")
                            .font(.system(size: 19, weight: .semibold))
                            .foregroundColor(.mdText)
                            .frame(width: 34, height: 44)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(backTitle)
                }
                Text(title)
                    .font(.system(size: 19, weight: .bold))
                    .foregroundColor(.mdText)
                Spacer()
            }
            .padding(.horizontal, 10)
            .frame(height: 56)
            .background(Color.mdSurface)
            .overlay(alignment: .bottom) { Rectangle().fill(Color.mdBorder).frame(height: 1) }

            ScrollView {
                content
                    .frame(maxWidth: 720)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 16)
            }
        }
        .background(Color.mdBackground)
        .navigationBarHidden(true)
        .modemDeckInteractiveBack(showsBackButton)
    }
}

private struct ModemDeckSettingsModule<Content: View>: View {
    let title: String
    let footer: String
    let content: Content

    init(title: String, footer: String = "", @ViewBuilder content: () -> Content) {
        self.title = title
        self.footer = footer
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(title.uppercased())
                .font(.system(size: 11, weight: .bold))
                .tracking(0.6)
                .foregroundColor(.mdFaint)
                .padding(.horizontal, 18)
                .padding(.bottom, 7)
            VStack(spacing: 0) {
                content
            }
            .background(Color.mdSurface)
            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .stroke(Color.mdBorder, lineWidth: 1)
            )
            .padding(.horizontal, 12)
            if !footer.isEmpty {
                Text(footer)
                    .font(.system(size: 12))
                    .foregroundColor(.mdMuted)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, 18)
                    .padding(.top, 7)
            }
        }
        .padding(.bottom, 20)
    }
}

private struct ModemDeckSettingsValueRow: View {
    let title: String
    let value: String
    var icon: String?
    var valueColor = Color.mdMuted

    var body: some View {
        HStack(spacing: 11) {
            if let icon {
                Image(systemName: icon)
                    .font(.system(size: 16, weight: .medium))
                    .foregroundColor(.mdMuted)
                    .frame(width: 22)
            }
            Text(title)
                .font(.system(size: 15))
                .foregroundColor(.mdText)
            Spacer(minLength: 14)
            Text(value)
                .font(.system(size: 14))
                .foregroundColor(valueColor)
                .multilineTextAlignment(.trailing)
                .lineLimit(3)
        }
        .padding(.horizontal, 14)
        .frame(minHeight: 52)
    }
}

private struct ModemDeckSettingsActionRow: View {
    let title: String
    let icon: String
    var destructive = false
    var busy = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 11) {
                if busy {
                    ProgressView().frame(width: 22)
                } else {
                    Image(systemName: icon)
                        .font(.system(size: 16, weight: .medium))
                        .frame(width: 22)
                }
                Text(title)
                    .font(.system(size: 15, weight: .medium))
                Spacer()
                Image(systemName: "chevron.right")
                    .font(.system(size: 12, weight: .semibold))
                    .opacity(0.45)
            }
            .foregroundColor(destructive ? .mdDanger : .mdAccent)
            .padding(.horizontal, 14)
            .frame(minHeight: 52)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(busy)
    }
}

private struct ModemDeckSettingsDivider: View {
    var body: some View {
        Rectangle().fill(Color.mdBorder).frame(height: 1).padding(.leading, 48)
    }
}

private struct ModemDeckSettingsChoice: Identifiable {
    let id: String
    let title: String
}

private struct ModemDeckSettingsMenuRow: View {
    let title: String
    let icon: String
    let value: String
    let choices: [ModemDeckSettingsChoice]
    var disabled = false
    let select: (String) -> Void

    var body: some View {
        Menu {
            ForEach(choices) { choice in
                Button {
                    select(choice.id)
                } label: {
                    if choice.title == value {
                        Label(choice.title, systemImage: "checkmark")
                    } else {
                        Text(choice.title)
                    }
                }
            }
        } label: {
            HStack(spacing: 11) {
                Image(systemName: icon)
                    .font(.system(size: 16, weight: .medium))
                    .foregroundColor(.mdMuted)
                    .frame(width: 22)
                Text(title)
                    .font(.system(size: 15))
                    .foregroundColor(.mdText)
                Spacer(minLength: 14)
                Text(value)
                    .font(.system(size: 14))
                    .foregroundColor(.mdAccent)
                    .lineLimit(1)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundColor(.mdFaint)
            }
            .padding(.horizontal, 14)
            .frame(minHeight: 52)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(disabled)
    }
}

private struct ModemDeckSettingsStatusRow: View {
    let icon: String
    let title: String
    let detail: String
    var active = true

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: icon)
                .font(.system(size: 17, weight: .semibold))
                .foregroundColor(active ? .mdAccent : .mdFaint)
                .frame(width: 34, height: 34)
                .background(active ? Color.mdAccentSoft : Color.mdSurfaceHover)
                .clipShape(Circle())
            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundColor(.mdText)
                Text(detail)
                    .font(.system(size: 12))
                    .foregroundColor(.mdMuted)
                    .lineLimit(3)
            }
            Spacer(minLength: 10)
            Circle()
                .fill(active ? Color.mdAccent : Color.mdFaint)
                .frame(width: 8, height: 8)
        }
        .padding(.horizontal, 14)
        .frame(minHeight: 62)
    }
}

private struct ModemDeckSettingsLoadState: View {
    let loading: Bool
    let error: String
    let emptyText: String

    var body: some View {
        if loading {
            ProgressView()
                .frame(maxWidth: .infinity, minHeight: 90)
        } else if !error.isEmpty {
            Text(error)
                .font(.system(size: 13))
                .foregroundColor(.mdDanger)
                .padding(14)
                .frame(maxWidth: .infinity, minHeight: 70, alignment: .leading)
        } else {
            Text(emptyText)
                .font(.system(size: 13))
                .foregroundColor(.mdMuted)
                .padding(14)
                .frame(maxWidth: .infinity, minHeight: 70, alignment: .leading)
        }
    }
}

private struct ModemDeckPreferencesSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @StateObject private var contactsStore: ModemDeckContactsStore
    @State private var language = "auto"
    @State private var defaultLineID = ""
    @State private var profileContactID = ""
    @State private var languageBusy = false
    @State private var lineBusy = false
    @State private var profileBusy = false
    @State private var errorMessage = ""

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        _contactsStore = StateObject(wrappedValue: controller.contactsStore)
    }

    private var languageChoices: [ModemDeckSettingsChoice] {
        [
            .init(id: "auto", title: controller.text("自动", "Automatic")),
            .init(id: "zh-CN", title: "简体中文"),
            .init(id: "zh-TW", title: "繁體中文"),
            .init(id: "en-US", title: "English"),
            .init(id: "ja-JP", title: "日本語"),
            .init(id: "vi-VN", title: "Tiếng Việt"),
            .init(id: "es-ES", title: "Español"),
            .init(id: "de-DE", title: "Deutsch"),
            .init(id: "fr-FR", title: "Français"),
            .init(id: "pt-BR", title: "Português")
        ]
    }

    private var lineChoices: [ModemDeckSettingsChoice] {
        (controller.bootstrap?.lineCatalog ?? []).map {
            .init(id: $0.id, title: $0.displayName)
        }
    }

    private var profileChoices: [ModemDeckSettingsChoice] {
        [
            .init(id: "", title: controller.text("不使用资料联系人", "No Profile Contact"))
        ] + contactsStore.contacts.map {
            .init(id: $0.id, title: $0.displayName)
        }
    }

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: controller.text("偏好设置", "Preferences"),
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            ModemDeckSettingsModule(title: controller.text("个人资料", "Profile")) {
                ModemDeckSettingsValueRow(
                    title: controller.text("账户", "Account"),
                    value: controller.session?.username ?? "—",
                    icon: "person.crop.circle"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsMenuRow(
                    title: controller.text("资料联系人", "Profile Contact"),
                    icon: "person.text.rectangle",
                    value: profileContactText,
                    choices: profileChoices,
                    disabled: profileBusy || contactsStore.loading
                ) { updateProfileContact($0) }
            }

            ModemDeckSettingsModule(
                title: controller.text("应用偏好", "App Preferences"),
                footer: controller.text(
                    "语言和默认线路会同步到同一账户的 Web 与 Apple 设备。",
                    "Language and default line sync across Web and Apple devices on this account."
                )
            ) {
                ModemDeckSettingsMenuRow(
                    title: controller.text("语言", "Language"),
                    icon: "globe",
                    value: languageTitle,
                    choices: languageChoices,
                    disabled: languageBusy
                ) { updateLanguage($0) }
                ModemDeckSettingsDivider()
                ModemDeckSettingsMenuRow(
                    title: controller.text("默认线路", "Default Line"),
                    icon: "simcard",
                    value: defaultLineTitle,
                    choices: lineChoices,
                    disabled: lineBusy || lineChoices.isEmpty
                ) { updateDefaultLine($0) }
            }

            if !errorMessage.isEmpty {
                Text(errorMessage)
                    .font(.system(size: 13))
                    .foregroundColor(.mdDanger)
                    .padding(.horizontal, 18)
            }
        }
        .onAppear { synchronize() }
        .onChange(of: controller.bootstrap) { _ in synchronize() }
        .task { await contactsStore.load() }
    }

    private var languageTitle: String {
        languageChoices.first(where: { $0.id == language })?.title ?? language
    }

    private var defaultLineTitle: String {
        lineChoices.first(where: { $0.id == defaultLineID })?.title ??
            controller.text("未设置", "Not Set")
    }

    private var profileContactText: String {
        profileChoices.first(where: { $0.id == profileContactID })?.title ??
            controller.text("未设置", "Not Set")
    }

    private func synchronize() {
        language = controller.bootstrap?.systemSettings.language ?? controller.session?.language ?? "auto"
        defaultLineID = controller.bootstrap?.lineSettings.defaultLineId ?? ""
        profileContactID = controller.session?.profileContactId ?? ""
    }

    private func updateProfileContact(_ next: String) {
        guard next != profileContactID, !profileBusy else { return }
        let previous = profileContactID
        profileContactID = next
        profileBusy = true
        errorMessage = ""
        Task {
            do {
                try await controller.api.setAccountContact(next)
                await controller.refresh()
            } catch {
                profileContactID = previous
                errorMessage = error.localizedDescription
            }
            profileBusy = false
        }
    }

    private func updateLanguage(_ next: String) {
        guard next != language, let revision = controller.bootstrap?.systemSettings.revision else { return }
        let previous = language
        language = next
        languageBusy = true
        errorMessage = ""
        Task {
            do {
                _ = try await controller.api.updateSystemSettings(language: next, revision: revision)
                await controller.refresh()
            } catch {
                language = previous
                errorMessage = error.localizedDescription
            }
            languageBusy = false
        }
    }

    private func updateDefaultLine(_ next: String) {
        guard next != defaultLineID, let revision = controller.bootstrap?.lineSettings.revision else { return }
        let previous = defaultLineID
        defaultLineID = next
        lineBusy = true
        errorMessage = ""
        Task {
            do {
                _ = try await controller.api.updateLineSettings(defaultLineID: next, revision: revision)
                await controller.refresh()
            } catch {
                defaultLineID = previous
                errorMessage = error.localizedDescription
            }
            lineBusy = false
        }
    }
}

private struct ModemDeckSecuritySettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @State private var sessions: [ModemDeckAccountSession] = []
    @State private var loading = true
    @State private var errorMessage = ""
    @State private var passwordSheetOpen = false
    @State private var revokingSessionID = ""
    @State private var sessionToRevoke: ModemDeckAccountSession?

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: controller.text("账户与安全", "Account & Security"),
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            ModemDeckSettingsModule(title: controller.text("当前身份", "Current Identity")) {
                ModemDeckSettingsValueRow(
                    title: controller.text("用户名", "Username"),
                    value: controller.session?.username ?? "—",
                    icon: "person.crop.circle"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("角色", "Role"),
                    value: roleText,
                    icon: "person.badge.key"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("可用线路", "Allowed Lines"),
                    value: allowedLinesText,
                    icon: "simcard.2"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("凭据存储", "Credential Storage"),
                    value: "iOS Keychain",
                    icon: "key.fill",
                    valueColor: .mdAccent
                )
            }

            ModemDeckSettingsModule(
                title: controller.text("密码", "Password"),
                footer: controller.text(
                    "修改密码会注销全部网页会话与已配对 Apple 设备，本机也需要重新配对。",
                    "Changing the password signs out every Web session and paired Apple device, including this one."
                )
            ) {
                ModemDeckSettingsActionRow(
                    title: controller.text("修改账户密码", "Change Account Password"),
                    icon: "key"
                ) {
                    passwordSheetOpen = true
                }
            }

            ModemDeckSettingsModule(
                title: controller.text("Apple 设备", "Apple Devices"),
                footer: controller.text(
                    "可在这里注销其他已配对设备；当前设备请从“配对”中撤销。",
                    "Sign out other paired devices here. Disconnect the current device from Pairing."
                )
            ) {
                if sessions.isEmpty {
                    ModemDeckSettingsLoadState(
                        loading: loading,
                        error: errorMessage,
                        emptyText: controller.text("没有设备信息。", "No device information.")
                    )
                } else {
                    ForEach(Array(sessions.enumerated()), id: \.element.id) { index, session in
                        if index > 0 { ModemDeckSettingsDivider() }
                        ModemDeckAccountSessionSettingsRow(
                            title: sessionTitle(session),
                            detail: sessionDetail(session),
                            current: session.current,
                            busy: revokingSessionID == session.id,
                            signOutTitle: controller.text("注销设备", "Sign Out")
                        ) { sessionToRevoke = session }
                    }
                }
            }
        }
        .task { await load() }
        .sheet(isPresented: $passwordSheetOpen) {
            ModemDeckPasswordChangeSheet(controller: controller) {
                passwordSheetOpen = false
                Task { await controller.disconnect() }
            }
        }
        .alert(item: $sessionToRevoke) { session in
            Alert(
                title: Text(controller.text("注销这台设备？", "Sign Out This Device?")),
                message: Text(controller.text(
                    "\(sessionTitle(session)) 将需要重新扫描配对二维码。",
                    "\(sessionTitle(session)) will need to scan a new pairing QR code."
                )),
                primaryButton: .destructive(Text(controller.text("注销", "Sign Out"))) {
                    revoke(session)
                },
                secondaryButton: .cancel(Text(controller.text("取消", "Cancel")))
            )
        }
    }

    private var roleText: String {
        controller.session?.role == "admin"
            ? controller.text("管理员", "Administrator")
            : controller.text("成员", "Member")
    }

    private var allowedLinesText: String {
        let ids = controller.session?.allowedLineIds ?? []
        if ids.isEmpty { return controller.text("全部", "All") }
        let names = ids.map { id in
            controller.bootstrap?.lineCatalog.first(where: { $0.id == id })?.displayName ?? id
        }
        return names.joined(separator: ", ")
    }

    private func sessionTitle(_ session: ModemDeckAccountSession) -> String {
        if let device = session.device { return device.displayName }
        let agent = session.userAgent?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return agent.isEmpty
            ? (session.kind == "ios" ? controller.text("Apple 设备", "Apple Device") : controller.text("网页会话", "Web Session"))
            : agent
    }

    private func sessionDetail(_ session: ModemDeckAccountSession) -> String {
        var parts: [String] = []
        if let device = session.device {
            let system = [device.osName, device.osVersion]
                .compactMap { $0?.trimmingCharacters(in: .whitespacesAndNewlines) }
                .filter { !$0.isEmpty }
                .joined(separator: " ")
            if !system.isEmpty { parts.append(system) }
            let version = [device.appVersion, device.appBuild.map { "(\($0))" }]
                .compactMap { $0 }
                .joined(separator: " ")
            if !version.isEmpty { parts.append("ModemDeck \(version)") }
        }
        if let last = session.lastSeenAt, !last.isEmpty {
            parts.append(controller.text("最近活动 \(controller.dateText(last))", "Last active \(controller.dateText(last))"))
        }
        if session.current { parts.append(controller.text("当前设备", "Current device")) }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        loading = true
        do {
            sessions = try await controller.api.accountSessions()
                .sorted { left, right in
                    if left.current != right.current { return left.current }
                    return (left.lastSeenAt ?? left.createdAt) > (right.lastSeenAt ?? right.createdAt)
                }
            errorMessage = ""
        } catch {
            errorMessage = error.localizedDescription
        }
        loading = false
    }

    private func revoke(_ session: ModemDeckAccountSession) {
        guard !session.current, revokingSessionID.isEmpty else { return }
        revokingSessionID = session.id
        errorMessage = ""
        Task {
            do {
                try await controller.api.revokeAccountSession(id: session.id)
                sessions.removeAll { $0.id == session.id }
            } catch {
                errorMessage = error.localizedDescription
            }
            revokingSessionID = ""
        }
    }
}

private struct ModemDeckAccountSessionSettingsRow: View {
    let title: String
    let detail: String
    let current: Bool
    let busy: Bool
    let signOutTitle: String
    let signOut: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "iphone")
                .font(.system(size: 17, weight: .semibold))
                .foregroundColor(.mdAccent)
                .frame(width: 34, height: 34)
                .background(Color.mdAccentSoft)
                .clipShape(Circle())
            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundColor(.mdText)
                    .lineLimit(1)
                Text(detail)
                    .font(.system(size: 12))
                    .foregroundColor(.mdMuted)
                    .lineLimit(3)
            }
            Spacer(minLength: 8)
            if current {
                Text("CURRENT")
                    .font(.system(size: 9, weight: .bold))
                    .foregroundColor(.mdAccentStrong)
                    .padding(.horizontal, 7)
                    .frame(height: 24)
                    .background(Color.mdAccentSoft)
                    .clipShape(Capsule())
                    .accessibilityLabel("Current device")
            } else {
                Button(action: signOut) {
                    Group {
                        if busy {
                            ProgressView()
                        } else {
                            Image(systemName: "rectangle.portrait.and.arrow.right")
                        }
                    }
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundColor(.mdDanger)
                    .frame(width: 40, height: 40)
                    .background(Color.mdDanger.opacity(0.07))
                    .clipShape(RoundedRectangle(cornerRadius: 9, style: .continuous))
                }
                .buttonStyle(.plain)
                .disabled(busy)
                .accessibilityLabel(signOutTitle)
            }
        }
        .padding(.horizontal, 14)
        .frame(minHeight: 68)
    }
}

private struct ModemDeckPasswordChangeSheet: View {
    @ObservedObject var controller: ModemDeckSessionController
    let changed: () -> Void
    @Environment(\.presentationMode) private var presentationMode
    @State private var currentPassword = ""
    @State private var newPassword = ""
    @State private var confirmation = ""
    @State private var saving = false
    @State private var errorMessage = ""

    private var canSubmit: Bool {
        !currentPassword.isEmpty && !newPassword.isEmpty && !confirmation.isEmpty && !saving
    }

    var body: some View {
        NavigationView {
            Form {
                Section {
                    SecureField(controller.text("当前密码", "Current Password"), text: $currentPassword)
                        .textContentType(.password)
                    SecureField(controller.text("新密码", "New Password"), text: $newPassword)
                        .textContentType(.newPassword)
                    SecureField(controller.text("确认新密码", "Confirm New Password"), text: $confirmation)
                        .textContentType(.newPassword)
                } footer: {
                    Text(controller.text(
                        "至少 8 个字符。修改后所有设备都需要重新登录或配对。",
                        "Use at least 8 characters. Every device must sign in or pair again afterward."
                    ))
                }

                if !errorMessage.isEmpty {
                    Section {
                        Text(errorMessage)
                            .font(.system(size: 13))
                            .foregroundColor(.mdDanger)
                    }
                }
            }
            .navigationTitle(controller.text("修改密码", "Change Password"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(controller.text("取消", "Cancel")) {
                        presentationMode.wrappedValue.dismiss()
                    }
                    .disabled(saving)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        submit()
                    } label: {
                        if saving {
                            ProgressView()
                        } else {
                            Text(controller.text("保存", "Save"))
                        }
                    }
                    .disabled(!canSubmit)
                }
            }
        }
        .navigationViewStyle(.stack)
        .interactiveDismissDisabled(saving)
    }

    private func submit() {
        guard canSubmit else { return }
        if let validation = validationError {
            errorMessage = validation
            return
        }
        saving = true
        errorMessage = ""
        Task {
            do {
                try await controller.api.changePassword(
                    currentPassword: currentPassword,
                    newPassword: newPassword
                )
                changed()
            } catch let error as ModemDeckAPIError {
                errorMessage = passwordError(error)
                saving = false
            } catch {
                errorMessage = error.localizedDescription
                saving = false
            }
        }
    }

    private var validationError: String? {
        if newPassword.count < 8 {
            return controller.text("新密码至少需要 8 个字符。", "The new password needs at least 8 characters.")
        }
        if newPassword.utf8.count > 1024 || newPassword.contains("\0") {
            return controller.text("新密码包含不支持的内容。", "The new password contains unsupported content.")
        }
        if newPassword == currentPassword {
            return controller.text("新密码不能与当前密码相同。", "The new password must differ from the current password.")
        }
        if newPassword != confirmation {
            return controller.text("两次输入的新密码不一致。", "The new passwords do not match.")
        }
        return nil
    }

    private func passwordError(_ error: ModemDeckAPIError) -> String {
        switch error.code {
        case "invalid_current_password":
            return controller.text("当前密码不正确。", "The current password is incorrect.")
        case "password_too_short":
            return controller.text("新密码至少需要 8 个字符。", "The new password needs at least 8 characters.")
        case "password_too_long", "password_invalid":
            return controller.text("新密码包含不支持的内容。", "The new password contains unsupported content.")
        case "password_unchanged":
            return controller.text("新密码不能与当前密码相同。", "The new password must differ from the current password.")
        default:
            return error.localizedDescription
        }
    }
}

private struct ModemDeckNotificationSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @State private var statusMessage = ""
    @State private var statusIsError = false

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: controller.text("通知", "Notifications"),
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            ModemDeckSettingsModule(
                title: controller.text("推送通知", "Push Notifications"),
                footer: controller.text(
                    "短信提醒通过 APNs 到达；来电通过 PushKit 唤醒 CallKit。",
                    "SMS alerts arrive through APNs; incoming calls wake CallKit through PushKit."
                )
            ) {
                ModemDeckSettingsValueRow(
                    title: controller.text("通知权限", "Notification Permission"),
                    value: notificationStatusText,
                    icon: "bell.badge",
                    valueColor: notificationStatusColor
                )
                ModemDeckSettingsDivider()
                if controller.notificationStatus == "not_determined" {
                    ModemDeckSettingsActionRow(
                        title: controller.text("允许通知", "Allow Notifications"),
                        icon: "checkmark.circle"
                    ) {
                        Task { await controller.requestNotificationPermission() }
                    }
                } else {
                    ModemDeckSettingsActionRow(
                        title: controller.text("打开系统通知设置", "Open Notification Settings"),
                        icon: "gearshape"
                    ) {
                        controller.openNotificationSettings()
                    }
                }
                ModemDeckSettingsDivider()
                ModemDeckSettingsActionRow(
                    title: controller.text("发送本机测试通知", "Send Local Test Notification"),
                    icon: "paperplane"
                ) {
                    Task {
                        let scheduled = await controller.scheduleLocalTestNotification()
                        statusIsError = !scheduled
                        statusMessage = scheduled
                            ? controller.text("测试通知已安排。", "Test notification scheduled.")
                            : controller.errorMessage
                    }
                }
            }

            if !statusMessage.isEmpty {
                Text(statusMessage)
                    .font(.system(size: 13))
                    .foregroundColor(statusIsError ? .mdDanger : .mdAccent)
                    .padding(.horizontal, 18)
            }
        }
        .task { await controller.refreshNotificationStatus() }
    }

    private var notificationStatusText: String {
        switch controller.notificationStatus {
        case "authorized": return controller.text("已允许", "Allowed")
        case "provisional": return controller.text("临时允许", "Provisional")
        case "denied": return controller.text("已关闭", "Off")
        case "not_determined": return controller.text("未设置", "Not Set")
        default: return controller.text("未知", "Unknown")
        }
    }

    private var notificationStatusColor: Color {
        switch controller.notificationStatus {
        case "authorized", "provisional": return .mdAccent
        case "denied": return .mdDanger
        default: return .mdMuted
        }
    }
}

private struct ModemDeckCallSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @State private var callSettings: ModemDeckGlobalCallSettings?
    @State private var recordingSettings: ModemDeckRecordingSettings?
    @State private var callSettingsBusy = false
    @State private var recordingBusy = false
    @State private var testCallBusy = false
    @State private var statusMessage = ""

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: controller.text("来电与音频", "Calls & Audio"),
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            ModemDeckSettingsModule(
                title: controller.text("通话行为", "Call Behavior"),
                footer: controller.text(
                    "接听后由 CallKit 管理系统音频；App 内保留静音、键盘和录音控制。",
                    "CallKit owns system audio after answering. Mute, keypad, and recording remain in the app."
                )
            ) {
                Toggle(
                    controller.text("接收来电", "Receive Calls"),
                    isOn: Binding(
                        get: { callSettings?.receiveCalls ?? false },
                        set: { updateReceiveCalls($0) }
                    )
                )
                .font(.system(size: 15))
                .foregroundColor(.mdText)
                .tint(.mdAccent)
                .padding(.horizontal, 14)
                .frame(minHeight: 52)
                .disabled(callSettings == nil || callSettingsBusy)
                ModemDeckSettingsDivider()
                Toggle(
                    controller.text("默认录制通话", "Record Calls by Default"),
                    isOn: Binding(
                        get: { recordingSettings?.defaultEnabled ?? false },
                        set: { updateRecordingDefault($0) }
                    )
                )
                .font(.system(size: 15))
                .foregroundColor(.mdText)
                .tint(.mdAccent)
                .padding(.horizontal, 14)
                .frame(minHeight: 52)
                .disabled(recordingSettings == nil || recordingBusy)
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: "CallKit",
                    value: controller.text("已接入", "Enabled"),
                    icon: "phone.connection",
                    valueColor: .mdAccent
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("麦克风", "Microphone"),
                    value: microphoneStatusText,
                    icon: "mic",
                    valueColor: controller.microphoneStatus == "denied" ? .mdDanger : .mdMuted
                )
                if controller.microphoneStatus != "authorized" {
                    ModemDeckSettingsDivider()
                    ModemDeckSettingsActionRow(
                        title: controller.microphoneStatus == "denied"
                            ? controller.text("打开麦克风设置", "Open Microphone Settings")
                            : controller.text("允许麦克风", "Allow Microphone"),
                        icon: "mic.badge.plus"
                    ) {
                        requestMicrophone()
                    }
                }
            }

            ModemDeckSettingsModule(title: controller.text("测试", "Test")) {
                ModemDeckSettingsActionRow(
                    title: testCallBusy
                        ? controller.text("正在发送…", "Sending…")
                        : controller.text("发送测试来电", "Send Test Call"),
                    icon: "phone.arrow.down.left",
                    busy: testCallBusy
                ) {
                    sendTestCall()
                }
            }

            if !statusMessage.isEmpty {
                Text(statusMessage)
                    .font(.system(size: 13))
                    .foregroundColor(.mdAccent)
                    .padding(.horizontal, 18)
            }
            if !controller.errorMessage.isEmpty {
                Text(controller.errorMessage)
                    .font(.system(size: 13))
                    .foregroundColor(.mdDanger)
                    .padding(.horizontal, 18)
            }
        }
        .task {
            controller.refreshMicrophoneStatus()
            await loadCallSettings()
            await loadRecordingSettings()
        }
    }

    private var microphoneStatusText: String {
        switch controller.microphoneStatus {
        case "authorized": return controller.text("已允许", "Allowed")
        case "denied": return controller.text("已关闭", "Off")
        case "not_determined": return controller.text("未设置", "Not Set")
        default: return controller.text("未知", "Unknown")
        }
    }

    private func requestMicrophone() {
        if controller.microphoneStatus == "denied" {
            guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
            UIApplication.shared.open(url)
        } else {
            Task { await controller.requestMicrophonePermission() }
        }
    }

    private func loadRecordingSettings() async {
        do {
            recordingSettings = try await controller.api.recordingSettings()
        } catch {
            controller.errorMessage = error.localizedDescription
        }
    }

    private func loadCallSettings() async {
        do {
            callSettings = try await controller.api.callSettings()
        } catch {
            controller.errorMessage = error.localizedDescription
        }
    }

    private func updateReceiveCalls(_ enabled: Bool) {
        guard let current = callSettings, !callSettingsBusy else { return }
        callSettings = ModemDeckGlobalCallSettings(
            receiveCalls: enabled,
            revision: current.revision
        )
        callSettingsBusy = true
        Task {
            do {
                callSettings = try await controller.api.updateCallSettings(
                    receiveCalls: enabled,
                    revision: current.revision
                )
                controller.errorMessage = ""
            } catch {
                callSettings = current
                controller.errorMessage = error.localizedDescription
            }
            callSettingsBusy = false
        }
    }

    private func updateRecordingDefault(_ enabled: Bool) {
        guard let current = recordingSettings, !recordingBusy else { return }
        recordingBusy = true
        Task {
            do {
                recordingSettings = try await controller.api.updateRecordingSettings(
                    enabled: enabled,
                    revision: current.revision
                )
                controller.errorMessage = ""
            } catch {
                controller.errorMessage = error.localizedDescription
            }
            recordingBusy = false
        }
    }

    private func sendTestCall() {
        guard !testCallBusy else { return }
        testCallBusy = true
        statusMessage = ""
        Task {
            do {
                _ = try await controller.api.sendIOSTestCall()
                statusMessage = controller.text("Apple 已接受测试来电。", "Apple accepted the test call.")
                controller.errorMessage = ""
            } catch {
                controller.errorMessage = error.localizedDescription
            }
            testCallBusy = false
        }
    }
}

private struct ModemDeckDevicesSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @State private var devices: [ModemDeckManagedDevice] = []
    @State private var loading = true
    @State private var errorMessage = ""

    private var lines: [ModemDeckLine] {
        controller.bootstrap?.lineCatalog ?? []
    }

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: controller.text("线路与设备", "Lines & Devices"),
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            ModemDeckSettingsModule(title: controller.text("蜂窝线路", "Cellular Lines")) {
                if lines.isEmpty {
                    ModemDeckSettingsLoadState(
                        loading: controller.bootstrap == nil,
                        error: "",
                        emptyText: controller.text("没有线路。", "No lines.")
                    )
                } else {
                    ForEach(Array(lines.enumerated()), id: \.element.id) { index, line in
                        if index > 0 { ModemDeckSettingsDivider() }
                        ModemDeckSettingsStatusRow(
                            icon: "simcard",
                            title: line.displayName,
                            detail: lineDetail(line),
                            active: line.state != "disconnected" && line.registrationState != "unregistered"
                        )
                    }
                }
            }

            ModemDeckSettingsModule(
                title: controller.text("调制解调器", "Modems"),
                footer: controller.text(
                    "设备配置仍由 Web 管理台完成；这里显示当前运行状态。",
                    "Configuration remains in the Web console; this page shows live status."
                )
            ) {
                if devices.isEmpty {
                    ModemDeckSettingsLoadState(
                        loading: loading,
                        error: errorMessage,
                        emptyText: controller.text("没有设备。", "No devices.")
                    )
                } else {
                    ForEach(Array(devices.enumerated()), id: \.element.id) { index, device in
                        if index > 0 { ModemDeckSettingsDivider() }
                        ModemDeckSettingsStatusRow(
                            icon: "antenna.radiowaves.left.and.right",
                            title: device.name.isEmpty ? device.model : device.name,
                            detail: deviceDetail(device),
                            active: device.present
                        )
                    }
                }
            }
        }
        .task { await load() }
    }

    private func lineDetail(_ line: ModemDeckLine) -> String {
        var parts = [line.phoneNumber, line.networkName, line.registrationState]
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        if let signal = line.signalQuality { parts.append("\(signal)%") }
        if !line.deviceName.isEmpty { parts.append(line.deviceName) }
        return parts.joined(separator: " · ")
    }

    private func deviceDetail(_ device: ModemDeckManagedDevice) -> String {
        var parts = [device.model, device.firmware, device.port]
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        if let signal = device.signalQuality { parts.append("\(signal)%") }
        let iccid = device.currentIccid.trimmingCharacters(in: .whitespacesAndNewlines)
        if !iccid.isEmpty { parts.append("SIM ••••\(iccid.suffix(4))") }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        loading = true
        do {
            devices = try await controller.api.managedDevices()
            errorMessage = ""
        } catch {
            errorMessage = error.localizedDescription
        }
        loading = false
    }
}

private struct ModemDeckUsersSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @State private var users: [ModemDeckUserSummary] = []
    @State private var loading = true
    @State private var errorMessage = ""

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: controller.text("用户", "Users"),
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            ModemDeckSettingsModule(
                title: controller.text("账户与权限", "Accounts & Access"),
                footer: controller.text(
                    "创建用户、修改线路范围和重置密码请使用 Web 管理台。",
                    "Use the Web console to create users, edit line scopes, or reset passwords."
                )
            ) {
                if users.isEmpty {
                    ModemDeckSettingsLoadState(
                        loading: loading,
                        error: errorMessage,
                        emptyText: controller.text("没有用户。", "No users.")
                    )
                } else {
                    ForEach(Array(users.enumerated()), id: \.element.id) { index, user in
                        if index > 0 { ModemDeckSettingsDivider() }
                        ModemDeckSettingsStatusRow(
                            icon: user.role == "admin" ? "person.badge.key.fill" : "person.fill",
                            title: userTitle(user),
                            detail: userDetail(user),
                            active: user.enabled
                        )
                    }
                }
            }
        }
        .task { await load() }
    }

    private func userTitle(_ user: ModemDeckUserSummary) -> String {
        let profileName = user.profileName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return profileName.isEmpty ? user.username : profileName
    }

    private func userDetail(_ user: ModemDeckUserSummary) -> String {
        var parts = [
            user.role == "admin"
                ? controller.text("管理员", "Administrator")
                : controller.text("成员", "Member")
        ]
        if user.role != "admin" {
            let names = user.lineIds.compactMap { id in
                controller.bootstrap?.lineCatalog.first(where: { $0.id == id })?.displayName
            }
            parts.append(names.isEmpty
                         ? controller.text("无线路", "No lines")
                         : names.joined(separator: ", "))
        }
        if user.iosPairingEnabled {
            parts.append(controller.text(
                "Apple 设备 \(user.iosPairingDeviceCount)/3",
                "Apple devices \(user.iosPairingDeviceCount)/3"
            ))
        }
        if !user.enabled { parts.append(controller.text("已停用", "Disabled")) }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        loading = true
        do {
            users = try await controller.api.users()
            errorMessage = ""
        } catch {
            errorMessage = error.localizedDescription
        }
        loading = false
    }
}

private struct ModemDeckTelegramSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @State private var units: [ModemDeckTelegramUnitSummary] = []
    @State private var loading = true
    @State private var errorMessage = ""

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: "Telegram",
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            ModemDeckSettingsModule(
                title: controller.text("通知机器人", "Notification Bots"),
                footer: controller.text(
                    "机器人密钥和收件人配置请在 Web 管理台修改。",
                    "Edit bot secrets and recipients in the Web console."
                )
            ) {
                if units.isEmpty {
                    ModemDeckSettingsLoadState(
                        loading: loading,
                        error: errorMessage,
                        emptyText: controller.text("没有 Telegram 机器人。", "No Telegram bots.")
                    )
                } else {
                    ForEach(Array(units.enumerated()), id: \.element.id) { index, unit in
                        if index > 0 { ModemDeckSettingsDivider() }
                        ModemDeckSettingsStatusRow(
                            icon: "paperplane.fill",
                            title: unit.displayName,
                            detail: unitDetail(unit),
                            active: unit.effectiveEnabled
                        )
                    }
                }
            }
        }
        .task { await load() }
    }

    private func unitDetail(_ unit: ModemDeckTelegramUnitSummary) -> String {
        var parts: [String] = []
        let username = unit.botUsername.trimmingCharacters(in: .whitespacesAndNewlines)
        if !username.isEmpty { parts.append(username.hasPrefix("@") ? username : "@\(username)") }
        if let assigned = unit.assignedUsername, !assigned.isEmpty { parts.append(assigned) }
        if unit.allAssignedLines {
            parts.append(controller.text("全部获授权线路", "All assigned lines"))
        } else if !unit.lineScopes.isEmpty {
            let names = unit.lineScopes.map { id in
                controller.bootstrap?.lineCatalog.first(where: { $0.id == id })?.displayName ?? id
            }
            parts.append(names.joined(separator: ", "))
        }
        var events: [String] = []
        if unit.incomingSms { events.append(controller.text("短信", "SMS")) }
        if unit.missedCalls { events.append(controller.text("未接来电", "Missed calls")) }
        if !events.isEmpty { parts.append(events.joined(separator: ", ")) }
        if !unit.tokenConfigured { parts.append(controller.text("未配置密钥", "Token missing")) }
        if !unit.lastErrorClass.isEmpty { parts.append(unit.lastErrorClass) }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        loading = true
        do {
            units = try await controller.api.telegramUnits()
            errorMessage = ""
        } catch {
            errorMessage = error.localizedDescription
        }
        loading = false
    }
}

private struct ModemDeckAccessSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController

    private var serverURL: String {
        if let credential = try? controller.credentialStore.load() {
            return credential.serverURL
        }
        return controller.serverDisplayName
    }

    private var secureTransport: Bool {
        URL(string: serverURL)?.scheme?.lowercased() == "https"
    }

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: controller.text("访问", "Access"),
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            ModemDeckSettingsModule(title: controller.text("当前连接", "Current Connection")) {
                ModemDeckSettingsValueRow(
                    title: controller.text("API 地址", "API Address"),
                    value: serverURL,
                    icon: "network"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("传输安全", "Transport Security"),
                    value: secureTransport ? "HTTPS" : "HTTP",
                    icon: secureTransport ? "lock.fill" : "lock.open",
                    valueColor: secureTransport ? .mdAccent : .mdDanger
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("验证方式", "Authentication"),
                    value: controller.text("配对设备凭据", "Paired device credential"),
                    icon: "key.fill"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("凭据存储", "Credential Storage"),
                    value: "iOS Keychain",
                    icon: "lock.shield.fill",
                    valueColor: .mdAccent
                )
            }

            ModemDeckSettingsModule(
                title: controller.text("Apple 服务", "Apple Services"),
                footer: controller.text(
                    "APNs 用于短信通知；PushKit 与 CallKit 用于系统来电界面。",
                    "APNs delivers SMS alerts; PushKit and CallKit present incoming calls."
                )
            ) {
                ModemDeckSettingsValueRow(
                    title: "APNs",
                    value: ModemDeckDeviceInfo.apnsEnvironment,
                    icon: "bell.and.waves.left.and.right"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: "PushKit",
                    value: controller.text("已接入", "Enabled"),
                    icon: "phone.arrow.down.left",
                    valueColor: .mdAccent
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: "CallKit",
                    value: controller.text("已接入", "Enabled"),
                    icon: "phone.connection",
                    valueColor: .mdAccent
                )
            }
        }
    }
}

private struct ModemDeckDiagnosticsSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @State private var snapshot: ModemDeckDiagnosticSnapshot?
    @State private var loading = true
    @State private var errorMessage = ""

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: controller.text("诊断", "Diagnostics"),
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            if let snapshot {
                ModemDeckSettingsModule(title: controller.text("运行状态", "Runtime Status")) {
                    ModemDeckSettingsStatusRow(
                        icon: "externaldrive.fill",
                        title: controller.text("数据库", "Database"),
                        detail: snapshot.database.error ?? controller.text("可用", "Available"),
                        active: snapshot.database.available
                    )
                    ModemDeckSettingsDivider()
                    ModemDeckSettingsStatusRow(
                        icon: "antenna.radiowaves.left.and.right",
                        title: controller.text("硬件服务", "Hardware Service"),
                        detail: agentDetail(snapshot.hostAgent),
                        active: snapshot.hostAgent.connected
                    )
                    ModemDeckSettingsDivider()
                    ModemDeckSettingsStatusRow(
                        icon: "phone.connection",
                        title: controller.text("通话运行时", "Call Runtime"),
                        detail: snapshot.calls.error ?? controller.text("可用", "Available"),
                        active: snapshot.calls.available
                    )
                    ModemDeckSettingsDivider()
                    ModemDeckSettingsValueRow(
                        title: controller.text("采样时间", "Observed"),
                        value: controller.dateText(snapshot.observedAt),
                        icon: "clock"
                    )
                }

                ModemDeckSettingsModule(title: controller.text("线路", "Lines")) {
                    if snapshot.lines.isEmpty {
                        ModemDeckSettingsLoadState(
                            loading: false,
                            error: "",
                            emptyText: controller.text("没有线路状态。", "No line status.")
                        )
                    } else {
                        ForEach(Array(snapshot.lines.enumerated()), id: \.element.id) { index, line in
                            if index > 0 { ModemDeckSettingsDivider() }
                            ModemDeckSettingsStatusRow(
                                icon: "simcard",
                                title: line.displayName,
                                detail: [line.networkName, line.registrationState]
                                    .filter { !$0.isEmpty }
                                    .joined(separator: " · "),
                                active: line.state != "disconnected"
                            )
                        }
                    }
                }

                if !snapshot.activeCalls.isEmpty {
                    ModemDeckSettingsModule(title: controller.text("活动通话", "Active Calls")) {
                        ForEach(Array(snapshot.activeCalls.enumerated()), id: \.element.id) { index, call in
                            if index > 0 { ModemDeckSettingsDivider() }
                            ModemDeckSettingsStatusRow(
                                icon: "phone.fill",
                                title: call.id,
                                detail: [call.direction, call.phase, call.bearer]
                                    .filter { !$0.isEmpty }
                                    .joined(separator: " · "),
                                active: call.mediaAvailable
                            )
                        }
                    }
                }
            } else {
                ModemDeckSettingsModule(title: controller.text("运行状态", "Runtime Status")) {
                    ModemDeckSettingsLoadState(
                        loading: loading,
                        error: errorMessage,
                        emptyText: controller.text("没有诊断信息。", "No diagnostics available.")
                    )
                }
            }
        }
        .task { await load() }
    }

    private func agentDetail(_ agent: ModemDeckDiagnosticAgent) -> String {
        var parts = [agent.provider, agent.agentVersion, agent.runtimeVersion]
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        if let error = agent.lastError, !error.isEmpty { parts.append(error) }
        return parts.isEmpty
            ? controller.text("没有运行时信息", "No runtime information")
            : parts.joined(separator: " · ")
    }

    private func load() async {
        loading = true
        do {
            snapshot = try await controller.api.diagnostics()
            errorMessage = ""
        } catch {
            errorMessage = error.localizedDescription
        }
        loading = false
    }
}

private struct ModemDeckContactImportSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @StateObject private var importer = ModemDeckContactImporter()
    @State private var showingImportConfirmation = false

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: controller.text("联系人", "Contacts"),
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            ModemDeckSettingsModule(
                title: controller.text("通讯录导入", "Contact Import"),
                footer: controller.text(
                    "读取前会请求系统权限，确认数量后才会上传到当前 ModemDeck 账户。重复号码会跳过。",
                    "System permission is requested first. Contacts upload only after count confirmation; duplicate numbers are skipped."
                )
            ) {
                ModemDeckSettingsActionRow(
                    title: importer.importing
                        ? controller.text("正在导入…", "Importing…")
                        : controller.text("从本机通讯录导入", "Import Device Contacts"),
                    icon: "person.crop.circle.badge.plus",
                    busy: importer.importing
                ) {
                    Task { await importer.prepare() }
                }
            }

            if !importer.resultMessage.isEmpty {
                Text(importer.resultMessage)
                    .font(.system(size: 13))
                    .foregroundColor(.mdAccent)
                    .padding(.horizontal, 18)
            }
            if !importer.errorMessage.isEmpty {
                Text(importer.errorMessage)
                    .font(.system(size: 13))
                    .foregroundColor(.mdDanger)
                    .padding(.horizontal, 18)
            }
        }
        .onChange(of: importer.pendingCount) { count in
            showingImportConfirmation = count > 0
        }
        .alert(
            controller.text("导入通讯录", "Import Contacts"),
            isPresented: $showingImportConfirmation
        ) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {
                importer.cancelPreparedImport()
            }
            Button(controller.text("导入", "Import")) {
                Task { await importer.importPrepared(using: controller.api) }
            }
        } message: {
            Text(controller.text(
                "将尝试导入 \(importer.pendingCount) 位含电话号码的联系人。",
                "Import \(importer.pendingCount) contacts with phone numbers."
            ))
        }
    }
}

private struct ModemDeckConnectionSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @State private var showingDisconnectConfirmation = false
    @State private var disconnecting = false
    @State private var disconnectError = ""

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: controller.text("配对", "Pairing"),
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            ModemDeckSettingsModule(title: controller.text("当前连接", "Current Connection")) {
                ModemDeckSettingsValueRow(
                    title: controller.text("服务器", "Server"),
                    value: controller.serverDisplayName,
                    icon: "server.rack"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("账户", "Account"),
                    value: controller.session?.username ?? "—",
                    icon: "person.crop.circle"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("角色", "Role"),
                    value: controller.session?.role ?? "—",
                    icon: "person.badge.key"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("用户 ID", "User ID"),
                    value: controller.session?.userId ?? "—",
                    icon: "number"
                )
            }

            ModemDeckSettingsModule(
                title: controller.text("设备配对", "Device Pairing"),
                footer: controller.text(
                    "撤销后会清除本机凭证，需要重新扫描二维码才能连接。",
                    "Disconnecting removes the local credential. Scan a new QR code to reconnect."
                )
            ) {
                ModemDeckSettingsActionRow(
                    title: controller.text("撤销本机配对", "Disconnect This Device"),
                    icon: "trash",
                    destructive: true,
                    busy: disconnecting
                ) {
                    showingDisconnectConfirmation = true
                }
            }

            if !disconnectError.isEmpty {
                Text(disconnectError)
                    .font(.system(size: 13))
                    .foregroundColor(.mdDanger)
                    .padding(.horizontal, 18)
            }
        }
        .alert(
            controller.text("撤销本机配对？", "Disconnect This Device?"),
            isPresented: $showingDisconnectConfirmation
        ) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {}
            Button(controller.text("撤销配对", "Disconnect"), role: .destructive) {
                disconnecting = true
                disconnectError = ""
                Task {
                    await controller.disconnect()
                    if controller.phase == .paired || controller.phase == .unavailable {
                        disconnectError = controller.errorMessage
                    }
                    disconnecting = false
                }
            }
        } message: {
            Text(controller.text(
                "服务器确认撤销后，本机才会清除凭证；失败时会保持配对并允许重试。",
                "This device clears its credential only after the server confirms revocation; failures stay paired so you can retry."
            ))
        }
    }
}

private struct ModemDeckAboutSettingsView: View {
    @ObservedObject var controller: ModemDeckSessionController

    var body: some View {
        ModemDeckSettingsDetailScaffold(
            title: controller.text("关于", "About"),
            backTitle: controller.text("返回设置", "Back to Settings")
        ) {
            ModemDeckSettingsModule(title: controller.text("应用", "Application")) {
                ModemDeckSettingsValueRow(
                    title: controller.text("App 版本", "App Version"),
                    value: ModemDeckDeviceInfo.appVersion,
                    icon: "app.badge"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: "Bundle ID",
                    value: Bundle.main.bundleIdentifier ?? "—",
                    icon: "shippingbox"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: "APNs",
                    value: ModemDeckDeviceInfo.apnsEnvironment,
                    icon: "bell.and.waves.left.and.right"
                )
            }

            ModemDeckSettingsModule(title: controller.text("此设备", "This Device")) {
                ModemDeckSettingsValueRow(
                    title: controller.text("名称", "Name"),
                    value: UIDevice.current.name,
                    icon: UIDevice.current.userInterfaceIdiom == .pad ? "ipad" : "iphone"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("设备类型", "Device Type"),
                    value: UIDevice.current.localizedModel,
                    icon: "rectangle.on.rectangle"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("硬件型号", "Hardware Model"),
                    value: ModemDeckDeviceInfo.hardwareIdentifier,
                    icon: "cpu"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("系统", "System"),
                    value: "\(UIDevice.current.systemName) \(UIDevice.current.systemVersion)",
                    icon: "gear"
                )
            }

            ModemDeckSettingsModule(title: controller.text("服务", "Service")) {
                ModemDeckSettingsValueRow(
                    title: controller.text("API 服务器", "API Server"),
                    value: controller.serverDisplayName,
                    icon: "network"
                )
                ModemDeckSettingsDivider()
                ModemDeckSettingsValueRow(
                    title: controller.text("硬件服务", "Hardware Service"),
                    value: controller.bootstrap?.capabilities.agentConnected == true
                        ? controller.text("在线", "Online")
                        : controller.text("离线", "Offline"),
                    icon: "antenna.radiowaves.left.and.right",
                    valueColor: controller.bootstrap?.capabilities.agentConnected == true ? .mdAccent : .mdDanger
                )
            }
        }
    }
}

enum ModemDeckDeviceInfo {
    static var appVersion: String {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "—"
        let build = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "—"
        return "\(version) (\(build))"
    }

    static var apnsEnvironment: String {
        Bundle.main.object(forInfoDictionaryKey: "ModemDeckAPNSEnvironment") as? String ?? "—"
    }

    static var hardwareIdentifier: String {
        var info = utsname()
        uname(&info)
        let mirror = Mirror(reflecting: info.machine)
        let bytes = mirror.children.compactMap { child -> UInt8? in
            guard let value = child.value as? Int8, value != 0 else { return nil }
            return UInt8(bitPattern: value)
        }
        return String(bytes: bytes, encoding: .utf8) ?? UIDevice.current.localizedModel
    }
}
