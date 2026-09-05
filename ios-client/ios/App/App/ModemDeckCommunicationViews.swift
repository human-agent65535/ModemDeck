import SwiftUI
import UIKit

struct ModemDeckContactsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @StateObject private var store: ModemDeckContactsStore
    @Environment(\.modemDeckUsesSplitWorkspace) private var usesSplitWorkspace
    @Environment(\.modemDeckNavigate) private var navigate
    @State private var query = ""
    @State private var selecting = false
    @State private var selectedIDs = Set<String>()
    @State private var selectedContactID: String?
    @State private var editorOpen = false
    @State private var editingContact: ModemDeckContact?
    @State private var batchBusy = false
    @State private var confirmBatchDelete = false

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        _store = StateObject(wrappedValue: controller.contactsStore)
    }

    private var filteredContacts: [ModemDeckContact] {
        let normalized = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !normalized.isEmpty else { return store.contacts }
        return store.contacts.filter { contact in
            contact.displayName.localizedCaseInsensitiveContains(normalized) ||
                contact.phones.contains { $0.displayNumber.localizedCaseInsensitiveContains(normalized) }
        }
    }

    private var selectedContact: ModemDeckContact? {
        guard let id = selectedContactID else { return nil }
        return store.contacts.first(where: { $0.id == id })
    }

    private var selectedContacts: [ModemDeckContact] {
        filteredContacts.filter { selectedIDs.contains($0.id) }
    }

    var body: some View {
        Group {
            if usesSplitWorkspace {
                HStack(spacing: 0) {
                    contactList
                        .frame(width: ModemDeckLayout.padListWidth)
                    Rectangle().fill(Color.mdBorder).frame(width: 1)
                    contactDetail
                }
            } else {
                contactList
            }
        }
        .background(Color.mdBackground)
        .navigationBarHidden(true)
        .overlay(alignment: .bottom) {
            ModemDeckInlineError(message: store.errorMessage)
                .padding(.horizontal, 16)
        }
        .task { await store.load() }
        .onChange(of: filteredContacts.map(\.id)) { visibleIDs in
            guard selecting else { return }
            selectedIDs.formIntersection(Set(visibleIDs))
        }
        .sheet(isPresented: $editorOpen) {
            ModemDeckContactEditor(
                controller: controller,
                contact: editingContact
            ) { saved in
                store.upsert(saved)
                selectedContactID = saved.id
            }
        }
        .alert(
            controller.text("删除所选联系人？", "Delete Selected Contacts?"),
            isPresented: $confirmBatchDelete
        ) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {}
            Button(controller.text("删除", "Delete"), role: .destructive) {
                deleteSelectedContacts()
            }
        } message: {
            Text(controller.text(
                "将删除 \(selectedContacts.count) 个联系人。此操作无法撤销。",
                "This permanently deletes \(selectedContacts.count) contacts."
            ))
        }
    }

    private var contactList: some View {
        VStack(spacing: 0) {
            ModemDeckPageHeader(
                title: controller.text("联系人", "Contacts"),
                actionIcon: "person.badge.plus",
                actionAccessibilityText: controller.text("新建联系人", "New contact"),
                actionDisabled: !controller.isOnline
            ) {
                editingContact = nil
                editorOpen = true
            }
            searchToolbar

            Group {
                if store.loading && store.contacts.isEmpty {
                    ProgressView(controller.text("正在载入联系人…", "Loading contacts…"))
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if !store.errorMessage.isEmpty && store.contacts.isEmpty {
                    ModemDeckLoadErrorState(
                        controller: controller,
                        detail: store.errorMessage
                    ) {
                        Task { await store.load() }
                    }
                } else if filteredContacts.isEmpty {
                    ScrollView {
                        ModemDeckStateView(
                            icon: query.isEmpty ? "person.crop.circle.badge.plus" : "magnifyingglass",
                            title: query.isEmpty
                                ? controller.text("还没有联系人", "No Contacts Yet")
                                : controller.text("没有匹配结果", "No Matches"),
                            detail: query.isEmpty
                                ? controller.text(
                                    "可在设置中从本机通讯录导入。",
                                    "Import contacts from this device in Settings."
                                )
                                : controller.text("请尝试其他姓名或号码。", "Try another name or number.")
                        )
                    }
                    .refreshable { await store.load() }
                } else {
                    List {
                        ForEach(filteredContacts) { contact in
                            contactListRow(contact)
                                .listRowInsets(EdgeInsets())
                                .listRowSeparator(.hidden)
                                .listRowBackground(Color.mdSurface)
                        }
                    }
                    .listStyle(.plain)
                    .scrollContentBackground(.hidden)
                    .environment(\.defaultMinListRowHeight, 0)
                    .background(Color.mdSurface)
                    .refreshable { await store.load() }
                }
            }
            .background(Color.mdSurface)
        }
        .background(Color.mdBackground)
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if selecting { contactBatchBar }
        }
    }

    private func contactListRow(_ contact: ModemDeckContact) -> some View {
        ModemDeckListRow(
            controller: controller, selecting: selecting,
            selected: selecting ? selectedIDs.contains(contact.id) : usesSplitWorkspace && selectedContactID == contact.id,
            enabled: !batchBusy, favorite: contact.favorite,
            accessibilityID: "contact-\(contact.id)",
            deleteMessage: controller.text("将永久删除此联系人。", "This permanently deletes this contact."),
            open: {
                if selecting {
                    if !selectedIDs.insert(contact.id).inserted { selectedIDs.remove(contact.id) }
                } else if usesSplitWorkspace {
                    selectedContactID = contact.id
                } else { navigate(.contact(contact.id)) }
            },
            toggleFavorite: {
                let saved = try await controller.api.updateContact(
                    id: contact.id,
                    draft: ModemDeckContactEditor.draft(from: contact, favorite: !contact.favorite)
                )
                store.upsert(saved)
            },
            delete: {
                try await controller.api.deleteContact(contact)
                store.remove(ids: [contact.id])
            }
        ) { actions in
            ModemDeckContactRow(contact: contact, controller: controller, contextActions: actions)
        }
    }

    @ViewBuilder
    private var contactDetail: some View {
        if let contact = selectedContact {
            ModemDeckContactDetailView(
                contact: contact,
                controller: controller,
                showsBackButton: false,
                onChanged: store.upsert,
                onDeleted: { id in
                    store.remove(ids: [id])
                    selectedContactID = nil
                }
            )
            .id(contact.id)
        } else {
            ModemDeckWorkspaceEmptyView(
                icon: "person.crop.circle",
                title: controller.text("选择联系人", "Select a contact"),
                detail: controller.text("联系人详情会显示在这里。", "Contact details appear here.")
            )
        }
    }

    private var searchToolbar: some View {
        HStack(spacing: ModemDeckLayout.toolbarGap) {
            ModemDeckToolbarButton(
                icon: selecting ? "xmark" : "checklist",
                active: selecting,
                accessibilityText: controller.text("选择联系人", "Select contacts")
            ) {
                selecting.toggle()
                if !selecting { selectedIDs.removeAll() }
            }
            ModemDeckSearchField(
                text: $query,
                prompt: controller.text("搜索联系人", "Search contacts"),
                clearAccessibilityText: controller.text("清除搜索", "Clear search")
            )
        }
        .modemDeckListToolbar(showsDivider: true)
    }

    private var contactBatchBar: some View {
        let contacts = selectedContacts
        let allFavorite = !contacts.isEmpty && contacts.allSatisfy(\.favorite)

        return ModemDeckBatchActionBar(
            selectedCount: contacts.count,
            totalCount: filteredContacts.count,
            busy: batchBusy,
            selectedText: controller.text("已选择 %d 项", "%d selected"),
            selectAllText: controller.text("全选", "Select All"),
            clearAllText: controller.text("清除", "Clear"),
            doneText: controller.text("完成", "Done"),
            selectAll: toggleAllContacts,
            done: endContactSelection
        ) {
            ModemDeckBatchActionButton(
                title: allFavorite
                    ? controller.text("取消收藏", "Unfavorite")
                    : controller.text("收藏", "Favorite"),
                icon: allFavorite ? "star.slash" : "star",
                disabled: contacts.isEmpty || batchBusy || !controller.isOnline
            ) {
                updateSelectedContactsFavorite(!allFavorite)
            }
            ModemDeckBatchActionButton(
                title: controller.text("删除", "Delete"),
                icon: "trash",
                destructive: true,
                disabled: contacts.isEmpty || batchBusy || !controller.isOnline
            ) {
                confirmBatchDelete = true
            }
        }
    }

    private func updateSelectedContactsFavorite(_ favorite: Bool) {
        let contacts = selectedContacts
        guard controller.isOnline, !contacts.isEmpty, !batchBusy else { return }
        batchBusy = true
        Task {
            do {
                for contact in contacts {
                    guard contact.favorite != favorite else { continue }
                    let saved = try await controller.api.updateContact(
                        id: contact.id,
                        draft: ModemDeckContactEditor.draft(from: contact, favorite: favorite)
                    )
                    store.upsert(saved)
                }
                store.errorMessage = ""
            } catch {
                store.errorMessage = error.localizedDescription
                await store.load()
            }
            batchBusy = false
        }
    }

    private func toggleAllContacts() {
        let visible = Set(filteredContacts.map(\.id))
        if !visible.isEmpty && visible.isSubset(of: selectedIDs) {
            selectedIDs.subtract(visible)
        } else {
            selectedIDs.formUnion(visible)
        }
    }

    private func endContactSelection() {
        selecting = false
        selectedIDs.removeAll()
    }

    private func deleteSelectedContacts() {
        let contacts = selectedContacts
        let deletedIDs = Set(contacts.map(\.id))
        guard controller.isOnline, !contacts.isEmpty, !batchBusy else { return }
        batchBusy = true
        Task {
            do {
                try await controller.api.deleteContacts(contacts)
                store.remove(ids: deletedIDs)
                if let selectedContactID, deletedIDs.contains(selectedContactID) {
                    self.selectedContactID = nil
                }
                endContactSelection()
                store.errorMessage = ""
            } catch {
                store.errorMessage = error.localizedDescription
            }
            batchBusy = false
        }
    }

}

struct ModemDeckContactRow: View {
    let contact: ModemDeckContact
    @ObservedObject var controller: ModemDeckSessionController
    var contextActions: [ModemDeckContextAction] = []

    var body: some View {
        HStack(spacing: 11) {
            ModemDeckAvatar(
                name: contact.displayName,
                avatarSource: contact.avatar,
                size: ModemDeckLayout.listAvatarSize
            )
            VStack(alignment: .leading, spacing: 4) {
                Text(contact.displayName)
                    .font(.subheadline.weight(.semibold))
                    .foregroundColor(.mdText)
                    .lineLimit(1)
                Text(contact.primaryPhone?.displayNumber ?? "")
                    .font(.footnote)
                    .foregroundColor(.mdMuted)
                    .lineLimit(1)
            }
            Spacer()
            if contact.favorite {
                Image(systemName: "star.fill")
                    .font(.system(size: 15))
                    .foregroundColor(.orange)
            }
        }
        .padding(.horizontal, ModemDeckLayout.listHorizontalPadding)
        .frame(minHeight: ModemDeckLayout.listRowMinHeight)
        .contentShape(Rectangle())
        .modemDeckCopyMenu([
            ModemDeckCopyItem(
                label: controller.text("复制姓名", "Copy Name"),
                value: contact.displayName
            ),
            ModemDeckCopyItem(
                label: controller.text("复制号码", "Copy Number"),
                value: contact.primaryPhone?.displayNumber ?? ""
            )
        ], actions: contextActions)
    }
}

enum ModemDeckAvatarFallback {
    case initials
    case person
    case service
    case unknown
    case call
    case message
    case recording
}

enum ModemDeckLucideAsset {
    static let house = "LucideHouse"
    static let usersRound = "LucideUsersRound"
    static let messageSquareText = "LucideMessageSquareText"
    static let phoneCall = "LucidePhoneCall"
    static let phone = "LucidePhone"
    static let audioLines = "LucideAudioLines"
    static let settings = "LucideSettings"
    static let userRound = "LucideUserRound"
    static let circleHelp = "LucideCircleHelp"
}

struct ModemDeckLucideIcon: View {
    let asset: String
    let size: CGFloat

    var body: some View {
        Image(asset)
            .renderingMode(.template)
            .resizable()
            .scaledToFit()
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}

enum ModemDeckCommunicationChannel {
    case call
    case message
    case recording

    var fallback: ModemDeckAvatarFallback {
        switch self {
        case .call: return .call
        case .message: return .message
        case .recording: return .recording
        }
    }

    var iconAsset: String {
        switch self {
        case .call: return ModemDeckLucideAsset.phone
        case .message: return ModemDeckLucideAsset.messageSquareText
        case .recording: return ModemDeckLucideAsset.audioLines
        }
    }

    var paletteSeed: String {
        switch self {
        case .call: return "call"
        case .message: return "message"
        case .recording: return "recording"
        }
    }

    var badgeForeground: Color {
        self == .call ? .mdBlue : .mdAccentStrong
    }

    var badgeBackground: Color {
        self == .call ? .mdBlueSoft : .mdAccentSoft
    }
}

private enum ModemDeckAvatarImageCache {
    static let images: NSCache<NSString, UIImage> = {
        let cache = NSCache<NSString, UIImage>()
        cache.countLimit = 128
        cache.totalCostLimit = 16 * 1_024 * 1_024
        return cache
    }()

    static func image(for source: String) -> UIImage? {
        let key = source as NSString
        if let cached = images.object(forKey: key) { return cached }
        guard source.hasPrefix("data:image/"),
              let separator = source.firstIndex(of: ","),
              source[..<separator].contains(";base64"),
              let data = Data(base64Encoded: String(source[source.index(after: separator)...])),
              let image = UIImage(data: data) else {
            return nil
        }
        images.setObject(image, forKey: key, cost: data.count)
        return image
    }
}

struct ModemDeckAvatar: View {
    let name: String
    var avatarSource: String? = nil
    var fallback = ModemDeckAvatarFallback.initials
    var paletteKey: String? = nil
    var size: CGFloat = 44

    private var initials: String {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let words = trimmed.split(whereSeparator: { $0.isWhitespace })
        if words.count > 1 {
            return "\(words.first?.first.map(String.init) ?? "")\(words.last?.first.map(String.init) ?? "")"
                .uppercased()
        }
        let value = String(trimmed.prefix(2))
        return value.isEmpty ? "#" : value.uppercased()
    }

    private var paletteIndex: Int {
        let source = paletteKey?.trimmingCharacters(in: .whitespacesAndNewlines)
        return (source?.isEmpty == false ? source! : name).unicodeScalars.reduce(0) {
            ($0 * 31 + Int($1.value)) % 8
        }
    }

    private var background: Color {
        [
            Color(red: 230 / 255, green: 238 / 255, blue: 252 / 255),
            Color(red: 229 / 255, green: 244 / 255, blue: 236 / 255),
            Color(red: 248 / 255, green: 234 / 255, blue: 223 / 255),
            Color(red: 241 / 255, green: 232 / 255, blue: 247 / 255),
            Color(red: 227 / 255, green: 242 / 255, blue: 244 / 255),
            Color(red: 247 / 255, green: 232 / 255, blue: 235 / 255),
            Color(red: 237 / 255, green: 240 / 255, blue: 223 / 255),
            Color(red: 238 / 255, green: 233 / 255, blue: 223 / 255)
        ][paletteIndex]
    }

    var body: some View {
        ZStack {
            Circle().fill(fallback == .unknown ? Color.mdSurfaceHover : background)
            avatarContent
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .overlay(Circle().stroke(Color.mdText.opacity(0.08), lineWidth: 1))
        .accessibilityHidden(true)
    }

    @ViewBuilder
    private var avatarContent: some View {
        let source = avatarSource?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if let image = ModemDeckAvatarImageCache.image(for: source) {
            Image(uiImage: image)
                .resizable()
                .scaledToFill()
        } else {
            fallbackContent
        }
    }

    @ViewBuilder
    private var fallbackContent: some View {
        if let iconAsset = fallbackIconAsset {
            ModemDeckLucideIcon(asset: iconAsset, size: fallbackIconSize)
                .foregroundColor(fallback == .unknown ? .mdMuted : .mdText)
        } else {
            Text(initials)
                .font(.system(size: size * 0.30, weight: .bold))
                .foregroundColor(.mdText)
        }
    }

    private var fallbackIconSize: CGFloat {
        if size >= 56 { return 30 }
        if size <= 34 { return 16 }
        return 20
    }

    private var fallbackIconAsset: String? {
        switch fallback {
        case .initials: return nil
        case .person: return ModemDeckLucideAsset.userRound
        case .service, .message: return ModemDeckLucideAsset.messageSquareText
        case .unknown: return ModemDeckLucideAsset.circleHelp
        case .call: return ModemDeckLucideAsset.phone
        case .recording: return ModemDeckLucideAsset.audioLines
        }
    }
}

struct ModemDeckCommunicationAvatar: View {
    let channel: ModemDeckCommunicationChannel
    let name: String
    let address: String
    var avatarSource: String? = nil
    var contactBound = false
    var muted = false
    var size: CGFloat = 44

    private var fallback: ModemDeckAvatarFallback {
        guard contactBound else { return channel.fallback }
        let nameKey = comparisonKey(name)
        let addressKey = comparisonKey(address)
        return !nameKey.isEmpty && nameKey != addressKey ? .initials : .person
    }

    private var paletteKey: String {
        fallback == .initials ? name.trimmingCharacters(in: .whitespacesAndNewlines) :
            (comparisonKey(address).isEmpty ? channel.paletteSeed : comparisonKey(address))
    }

    private var badgeSize: CGFloat { size >= 56 ? 24 : (size <= 34 ? 18 : 21) }
    private var badgeIconSize: CGFloat { size >= 56 ? 14 : (size <= 34 ? 10 : 12) }

    var body: some View {
        ZStack(alignment: .bottomTrailing) {
            ModemDeckAvatar(
                name: name.isEmpty ? address : name,
                avatarSource: avatarSource,
                fallback: fallback,
                paletteKey: paletteKey,
                size: size
            )
            .saturation(muted ? 0 : 1)
            .opacity(muted ? 0.7 : 1)

            if contactBound {
                ModemDeckLucideIcon(asset: channel.iconAsset, size: badgeIconSize)
                    .foregroundColor(muted ? .mdMuted : channel.badgeForeground)
                    .frame(width: badgeSize, height: badgeSize)
                    .background(muted ? Color.mdSurfaceHover : channel.badgeBackground)
                    .clipShape(Circle())
                    .overlay(Circle().stroke(Color.mdSurface, lineWidth: 2))
                    .shadow(color: Color.black.opacity(0.14), radius: 1.5, y: 1)
                    .offset(x: size >= 56 ? 0 : 4, y: size >= 56 ? 0 : 4)
                    .accessibilityHidden(true)
            }
        }
        .frame(width: size, height: size)
    }

    private func comparisonKey(_ value: String) -> String {
        let source = value.trimmingCharacters(in: .whitespacesAndNewlines)
            .precomposedStringWithCompatibilityMapping
            .lowercased()
        guard !source.isEmpty else { return "" }
        let allowed = CharacterSet(charactersIn: "+0123456789 ().-/")
        guard source.unicodeScalars.allSatisfy({ allowed.contains($0) }) else {
            return source.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        }
        var compact = source.filter { !" ().-/".contains($0) }
        if compact.hasPrefix("00"), compact.dropFirst(2).first?.isNumber == true {
            compact = "+" + compact.dropFirst(2)
        }
        return compact
    }
}

struct ModemDeckHeaderAction: Identifiable {
    let id: String
    let icon: String
    let title: String
    var color = Color.mdMuted
    var disabled = false
    let action: () -> Void
}

struct ModemDeckIdentityHeader: View {
    let title: String
    let subtitle: String
    var avatarSource: String? = nil
    var channel: ModemDeckCommunicationChannel? = nil
    var contactBound = false
    var showsBackButton = true
    let backTitle: String
    var actions: [ModemDeckHeaderAction] = []
    var copyItems: [ModemDeckCopyItem] = []
    let dismiss: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            if showsBackButton {
                Button(action: dismiss) {
                    Image(systemName: "chevron.left")
                        .font(.system(size: 19, weight: .semibold))
                        .foregroundColor(.mdText)
                        .frame(width: 32, height: 44)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(backTitle)
            }

            HStack(spacing: 10) {
                if let channel {
                    ModemDeckCommunicationAvatar(
                        channel: channel,
                        name: title,
                        address: subtitle,
                        avatarSource: avatarSource,
                        contactBound: contactBound,
                        size: 44
                    )
                } else {
                    ModemDeckAvatar(name: title, avatarSource: avatarSource, size: 44)
                }
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.system(size: 18, weight: .bold))
                        .foregroundColor(.mdText)
                        .lineLimit(1)
                    if !subtitle.isEmpty {
                        Text(subtitle)
                            .font(.system(size: 13))
                            .foregroundColor(.mdMuted)
                            .lineLimit(1)
                    }
                }
            }
            .contentShape(Rectangle())
            .modemDeckCopyMenu(copyItems)
            .layoutPriority(1)
            Spacer()
            if actions.count > 3 {
                ForEach(Array(actions.prefix(2))) { item in
                    headerButton(item)
                }
                Menu {
                    ForEach(Array(actions.dropFirst(2))) { item in
                        Button(action: item.action) {
                            Label(item.title, systemImage: item.icon)
                        }
                        .disabled(item.disabled)
                    }
                } label: {
                    Image(systemName: "ellipsis")
                        .font(.system(size: 17, weight: .semibold))
                        .foregroundColor(.mdMuted)
                        .frame(width: 38, height: 40)
                }
                .accessibilityLabel("More actions")
            } else {
                ForEach(actions) { item in
                    headerButton(item)
                }
            }
        }
        .padding(.horizontal, 10)
        .frame(height: 64)
        .background(Color.mdSurface)
        .overlay(alignment: .bottom) {
            Rectangle().fill(Color.mdBorder).frame(height: 1)
        }
    }

    private func headerButton(_ item: ModemDeckHeaderAction) -> some View {
        Button(action: item.action) {
            Image(systemName: item.icon)
                .font(.system(size: 17, weight: .semibold))
                .foregroundColor(item.color)
                .frame(width: 38, height: 40)
        }
        .buttonStyle(.plain)
        .disabled(item.disabled)
        .opacity(item.disabled ? 0.45 : 1)
        .accessibilityLabel(item.title)
    }
}

struct ModemDeckContactDetailView: View {
    let contact: ModemDeckContact
    @ObservedObject var controller: ModemDeckSessionController
    var showsBackButton = true
    var onChanged: (ModemDeckContact) -> Void = { _ in }
    var onDeleted: (String) -> Void = { _ in }
    @Environment(\.presentationMode) private var presentationMode
    @State private var displayedContact: ModemDeckContact
    @State private var editorOpen = false
    @State private var favoritePending = false
    @State private var deleting = false
    @State private var confirmDelete = false
    @State private var mutationError = ""

    init(
        contact: ModemDeckContact,
        controller: ModemDeckSessionController,
        showsBackButton: Bool = true,
        onChanged: @escaping (ModemDeckContact) -> Void = { _ in },
        onDeleted: @escaping (String) -> Void = { _ in }
    ) {
        self.contact = contact
        self.controller = controller
        self.showsBackButton = showsBackButton
        self.onChanged = onChanged
        self.onDeleted = onDeleted
        _displayedContact = State(initialValue: contact)
    }

    private var defaultLine: ModemDeckLine? {
        let id = displayedContact.preferredLineId ?? controller.bootstrap?.lineSettings.defaultLineId
        return controller.voiceDialLine(preferredID: id)
    }

    var body: some View {
        VStack(spacing: 0) {
            ModemDeckIdentityHeader(
                title: displayedContact.displayName,
                subtitle: displayedContact.primaryPhone?.displayNumber ?? "",
                avatarSource: displayedContact.avatar,
                showsBackButton: showsBackButton,
                backTitle: controller.text("返回联系人", "Back to contacts"),
                actions: contactHeaderActions,
                copyItems: [
                    ModemDeckCopyItem(
                        label: controller.text("复制姓名", "Copy Name"),
                        value: displayedContact.displayName
                    ),
                    ModemDeckCopyItem(
                        label: controller.text("复制号码", "Copy Number"),
                        value: displayedContact.primaryPhone?.displayNumber ?? ""
                    )
                ]
            ) {
                presentationMode.wrappedValue.dismiss()
            }

            ScrollView {
                VStack(spacing: 0) {
                    if !mutationError.isEmpty {
                        ModemDeckInlineError(message: mutationError)
                            .padding(16)
                        ModemDeckListDivider(leading: 16)
                    }

                    if let notes = displayedContact.notes, !notes.isEmpty {
                        Text(notes)
                            .font(.system(size: 14))
                            .foregroundColor(.mdMuted)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(16)
                        ModemDeckListDivider(leading: 16)
                    }

                    ForEach(Array(displayedContact.phones.enumerated()), id: \.offset) { _, phone in
                        VStack(alignment: .leading, spacing: 12) {
                            VStack(alignment: .leading, spacing: 3) {
                                Text(phone.displayNumber)
                                    .font(.system(size: 17, weight: .semibold))
                                    .foregroundColor(.mdText)
                                Text(phone.label)
                                    .font(.system(size: 12))
                                    .foregroundColor(.mdMuted)
                            }
                            .contentShape(Rectangle())
                            .modemDeckCopyMenu([
                                ModemDeckCopyItem(
                                    label: controller.text("复制号码", "Copy Number"),
                                    value: phone.displayNumber
                                )
                            ])
                            HStack(spacing: 10) {
                                Button {
                                    startCall(to: phone.displayNumber)
                                } label: {
                                    Label(controller.text("呼叫", "Call"), systemImage: "phone.fill")
                                        .frame(maxWidth: .infinity, minHeight: 42)
                                }
                                .buttonStyle(.borderedProminent)
                                .tint(.mdAccent)
                                .disabled(defaultLine == nil || !controller.isOnline)

                                NavigationLink {
                                    ModemDeckDirectMessageView(
                                        number: phone.displayNumber,
                                        displayName: displayedContact.displayName,
                                        avatarSource: displayedContact.avatar,
                                        contactBound: true,
                                        controller: controller,
                                        preferredLineID: displayedContact.preferredLineId
                                    )
                                } label: {
                                    Label(controller.text("消息", "Message"), systemImage: "message.fill")
                                        .frame(maxWidth: .infinity, minHeight: 42)
                                }
                                .buttonStyle(.bordered)
                                .tint(.mdAccent)
                                .disabled(!controller.isOnline)
                            }
                        }
                        .padding(16)
                        ModemDeckListDivider(leading: 16)
                    }
                }
                .background(Color.mdSurface)
            }
        }
        .background(Color.mdBackground)
        .navigationBarHidden(true)
        .modemDeckInteractiveBack(showsBackButton)
        .onChange(of: contact) { displayedContact = $0 }
        .sheet(isPresented: $editorOpen) {
            ModemDeckContactEditor(
                controller: controller,
                contact: displayedContact
            ) { saved in
                displayedContact = saved
                controller.contactsStore.upsert(saved)
                onChanged(saved)
            }
        }
        .alert(
            controller.text("删除联系人？", "Delete Contact?"),
            isPresented: $confirmDelete
        ) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {}
            Button(controller.text("删除", "Delete"), role: .destructive) { deleteContact() }
        } message: {
            Text(controller.text(
                "将永久删除 \(displayedContact.displayName)。",
                "This permanently deletes \(displayedContact.displayName)."
            ))
        }
    }

    private var contactHeaderActions: [ModemDeckHeaderAction] {
        [
            ModemDeckHeaderAction(
                id: "edit",
                icon: "pencil",
                title: controller.text("编辑联系人", "Edit contact"),
                disabled: deleting || !controller.isOnline
            ) { editorOpen = true },
            ModemDeckHeaderAction(
                id: "favorite",
                icon: displayedContact.favorite ? "star.fill" : "star",
                title: displayedContact.favorite
                    ? controller.text("取消收藏", "Unfavorite")
                    : controller.text("收藏", "Favorite"),
                color: displayedContact.favorite ? .orange : .mdMuted,
                disabled: favoritePending || deleting || !controller.isOnline
            ) { toggleFavorite() },
            ModemDeckHeaderAction(
                id: "delete",
                icon: "trash",
                title: controller.text("删除联系人", "Delete contact"),
                color: .mdDanger,
                disabled: deleting || !controller.isOnline
            ) { confirmDelete = true }
        ]
    }

    private func startCall(to number: String) {
        guard controller.isOnline, let line = defaultLine else { return }
        Task {
            await controller.callController.start(
                lineID: line.id,
                number: number,
                displayName: displayedContact.displayName,
                recording: nil
            )
        }
    }

    private func toggleFavorite() {
        guard controller.isOnline, !favoritePending else { return }
        favoritePending = true
        mutationError = ""
        let draft = ModemDeckContactEditor.draft(
            from: displayedContact,
            favorite: !displayedContact.favorite
        )
        Task {
            do {
                let saved = try await controller.api.updateContact(
                    id: displayedContact.id,
                    draft: draft
                )
                displayedContact = saved
                controller.contactsStore.upsert(saved)
                onChanged(saved)
            } catch {
                mutationError = error.localizedDescription
            }
            favoritePending = false
        }
    }

    private func deleteContact() {
        guard controller.isOnline, !deleting else { return }
        deleting = true
        mutationError = ""
        let target = displayedContact
        Task {
            do {
                try await controller.api.deleteContact(target)
                controller.contactsStore.remove(ids: [target.id])
                onDeleted(target.id)
                if showsBackButton { presentationMode.wrappedValue.dismiss() }
            } catch {
                mutationError = error.localizedDescription
            }
            deleting = false
        }
    }
}

private struct ModemDeckContactPhoneEditorDraft: Identifiable {
    let id = UUID()
    var serverID: String?
    var label: String
    var number: String
    var region: String
    var primary: Bool
}

struct ModemDeckContactEditor: View {
    @ObservedObject var controller: ModemDeckSessionController
    let contact: ModemDeckContact?
    let onSaved: (ModemDeckContact) -> Void
    @Environment(\.presentationMode) private var presentationMode

    @State private var name: String
    @State private var avatar: String
    @State private var favorite: Bool
    @State private var phones: [ModemDeckContactPhoneEditorDraft]
    @State private var preferredLineID: String
    @State private var notes: String
    @State private var saving = false
    @State private var errorMessage = ""

    init(
        controller: ModemDeckSessionController,
        contact: ModemDeckContact?,
        onSaved: @escaping (ModemDeckContact) -> Void
    ) {
        self.controller = controller
        self.contact = contact
        self.onSaved = onSaved
        _name = State(initialValue: contact?.displayName ?? "")
        _avatar = State(initialValue: contact?.avatar ?? "")
        _favorite = State(initialValue: contact?.favorite ?? false)
        _preferredLineID = State(initialValue: contact?.preferredLineId ?? "")
        _notes = State(initialValue: contact?.notes ?? "")
        _phones = State(initialValue: contact?.phones.map {
            ModemDeckContactPhoneEditorDraft(
                serverID: $0.id,
                label: $0.label,
                number: $0.displayNumber,
                region: $0.region ?? "",
                primary: $0.primary
            )
        } ?? [
            ModemDeckContactPhoneEditorDraft(
                serverID: nil,
                label: controller.text("手机", "Mobile"),
                number: "",
                region: "",
                primary: true
            )
        ])
    }

    private var valid: Bool {
        !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
            !phones.isEmpty &&
            phones.allSatisfy { !$0.number.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty } &&
            phones.filter(\.primary).count == 1
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                Text(contact == nil
                     ? controller.text("新建联系人", "New Contact")
                     : controller.text("编辑联系人", "Edit Contact"))
                    .font(.system(size: 19, weight: .bold))
                    .foregroundColor(.mdText)
                Spacer()
                Button {
                    presentationMode.wrappedValue.dismiss()
                } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 17, weight: .semibold))
                        .foregroundColor(.mdMuted)
                        .frame(width: 40, height: 40)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(controller.text("关闭", "Close"))
            }
            .padding(.horizontal, 18)
            .frame(height: 58)
            .background(Color.mdSurface)
            .overlay(alignment: .bottom) { Rectangle().fill(Color.mdBorder).frame(height: 1) }

            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    editorField(controller.text("姓名", "Name")) {
                        TextField(controller.text("联系人姓名", "Contact name"), text: $name)
                            .textContentType(.name)
                    }

                    Toggle(isOn: $favorite) {
                        Label(
                            controller.text("收藏联系人", "Favorite Contact"),
                            systemImage: favorite ? "star.fill" : "star"
                        )
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundColor(favorite ? .orange : .mdText)
                    }
                    .tint(.mdAccent)
                    .padding(.horizontal, 14)
                    .frame(minHeight: 54)
                    .background(Color.mdSurface)
                    .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                    .overlay(
                        RoundedRectangle(cornerRadius: 10, style: .continuous)
                            .stroke(Color.mdBorder, lineWidth: 1)
                    )

                    VStack(alignment: .leading, spacing: 10) {
                        Text(controller.text("电话号码", "Phone Numbers"))
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundColor(.mdMuted)
                        ForEach($phones) { $phone in
                            phoneEditor(phone: $phone)
                        }
                        Button { addPhone() } label: {
                            Label(controller.text("添加号码", "Add Number"), systemImage: "plus")
                                .font(.system(size: 14, weight: .semibold))
                                .foregroundColor(.mdAccent)
                                .frame(minHeight: 40)
                        }
                        .buttonStyle(.plain)
                    }

                    if !(controller.bootstrap?.lineCatalog ?? []).isEmpty {
                        editorField(controller.text("首选线路", "Preferred Line")) {
                            Picker("", selection: $preferredLineID) {
                                Text(controller.text("跟随默认线路", "Follow Default Line")).tag("")
                                ForEach(controller.bootstrap?.lineCatalog ?? []) { line in
                                    Text(line.displayName).tag(line.id)
                                }
                            }
                            .pickerStyle(.menu)
                            .tint(.mdAccent)
                        }
                    }

                    editorField(controller.text("头像 URL", "Avatar URL")) {
                        TextField(controller.text("可选", "Optional"), text: $avatar)
                            .keyboardType(.URL)
                            .textInputAutocapitalization(.never)
                            .disableAutocorrection(true)
                    }

                    VStack(alignment: .leading, spacing: 7) {
                        Text(controller.text("备注", "Notes"))
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundColor(.mdMuted)
                        TextEditor(text: $notes)
                            .font(.system(size: 15))
                            .foregroundColor(.mdText)
                            .frame(minHeight: 96)
                            .padding(8)
                            .background(Color.mdSurface)
                            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                            .overlay(
                                RoundedRectangle(cornerRadius: 10, style: .continuous)
                                    .stroke(Color.mdBorder, lineWidth: 1)
                            )
                    }

                    if !errorMessage.isEmpty {
                        ModemDeckInlineError(message: errorMessage)
                    }
                }
                .frame(maxWidth: 680)
                .padding(18)
                .frame(maxWidth: .infinity)
            }
            .background(Color.mdBackground)

            HStack(spacing: 10) {
                Button {
                    presentationMode.wrappedValue.dismiss()
                } label: {
                    Text(controller.text("取消", "Cancel"))
                        .frame(maxWidth: .infinity, minHeight: 42)
                }
                .buttonStyle(.bordered)
                .tint(.mdMuted)
                .frame(maxWidth: .infinity)

                Button {
                    save()
                } label: {
                    Group {
                        if saving {
                            ProgressView().tint(.white)
                        } else {
                            Text(controller.text("保存", "Save"))
                        }
                    }
                    .frame(maxWidth: .infinity, minHeight: 42)
                }
                .buttonStyle(.borderedProminent)
                .tint(.mdAccent)
                .frame(maxWidth: .infinity)
                .disabled(!valid || saving)
            }
            .padding(14)
            .background(Color.mdSurface)
            .overlay(alignment: .top) { Rectangle().fill(Color.mdBorder).frame(height: 1) }
        }
        .interactiveDismissDisabled(saving)
    }

    @ViewBuilder
    private func editorField<Content: View>(
        _ title: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(title)
                .font(.system(size: 13, weight: .semibold))
                .foregroundColor(.mdMuted)
            content()
                .font(.system(size: 15))
                .foregroundColor(.mdText)
                .padding(.horizontal, 12)
                .frame(minHeight: 44)
                .background(Color.mdSurface)
                .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .stroke(Color.mdBorder, lineWidth: 1)
                )
        }
    }

    private func phoneEditor(
        phone: Binding<ModemDeckContactPhoneEditorDraft>
    ) -> some View {
        VStack(spacing: 10) {
            HStack(spacing: 10) {
                TextField(controller.text("类型", "Label"), text: phone.label)
                    .font(.system(size: 14, weight: .medium))
                    .padding(.horizontal, 10)
                    .frame(width: 94, height: 42)
                    .background(Color.mdSurfaceHover)
                    .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                TextField(controller.text("电话号码", "Phone number"), text: phone.number)
                    .font(.system(size: 15))
                    .keyboardType(.phonePad)
                    .padding(.horizontal, 10)
                    .frame(height: 42)
                    .background(Color.mdSurfaceHover)
                    .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
            }
            HStack(spacing: 10) {
                TextField(controller.text("地区", "Region"), text: phone.region)
                    .font(.system(size: 13))
                    .textInputAutocapitalization(.characters)
                    .disableAutocorrection(true)
                    .padding(.horizontal, 10)
                    .frame(width: 94, height: 38)
                    .background(Color.mdSurfaceHover)
                    .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                Button {
                    setPrimary(phone.wrappedValue.id)
                } label: {
                    Label(
                        controller.text("主号码", "Primary"),
                        systemImage: phone.wrappedValue.primary ? "checkmark.circle.fill" : "circle"
                    )
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundColor(phone.wrappedValue.primary ? .mdAccent : .mdMuted)
                }
                .buttonStyle(.plain)
                Spacer()
                Button {
                    removePhone(phone.wrappedValue.id)
                } label: {
                    Image(systemName: "trash")
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundColor(.mdDanger)
                        .frame(width: 38, height: 38)
                }
                .buttonStyle(.plain)
                .disabled(phones.count == 1)
                .opacity(phones.count == 1 ? 0.4 : 1)
            }
        }
        .padding(12)
        .background(Color.mdSurface)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(Color.mdBorder, lineWidth: 1)
        )
    }

    private func addPhone() {
        phones.append(ModemDeckContactPhoneEditorDraft(
            serverID: nil,
            label: controller.text("其他", "Other"),
            number: "",
            region: "",
            primary: phones.isEmpty
        ))
    }

    private func removePhone(_ id: UUID) {
        guard phones.count > 1, let index = phones.firstIndex(where: { $0.id == id }) else { return }
        let wasPrimary = phones[index].primary
        phones.remove(at: index)
        if wasPrimary, !phones.isEmpty { phones[0].primary = true }
    }

    private func setPrimary(_ id: UUID) {
        for index in phones.indices {
            phones[index].primary = phones[index].id == id
        }
    }

    private func save() {
        guard valid, !saving else { return }
        saving = true
        errorMessage = ""
        let input = ModemDeckContactDraft(
            displayName: name.trimmingCharacters(in: .whitespacesAndNewlines),
            avatar: nilIfEmpty(avatar),
            favorite: favorite,
            phones: phones.map {
                ModemDeckContactDraft.Phone(
                    id: $0.serverID,
                    label: nilIfEmpty($0.label) ?? controller.text("电话", "Phone"),
                    number: $0.number.trimmingCharacters(in: .whitespacesAndNewlines),
                    region: nilIfEmpty($0.region)?.uppercased(),
                    primary: $0.primary
                )
            },
            notes: nilIfEmpty(notes),
            preferredLineId: nilIfEmpty(preferredLineID),
            revision: contact?.revision
        )
        Task {
            do {
                let saved: ModemDeckContact
                if let contact {
                    saved = try await controller.api.updateContact(id: contact.id, draft: input)
                } else {
                    saved = try await controller.api.createContact(input)
                }
                controller.contactsStore.upsert(saved)
                onSaved(saved)
                presentationMode.wrappedValue.dismiss()
            } catch {
                errorMessage = error.localizedDescription
            }
            saving = false
        }
    }

    static func draft(
        from contact: ModemDeckContact,
        favorite: Bool? = nil
    ) -> ModemDeckContactDraft {
        ModemDeckContactDraft(
            displayName: contact.displayName,
            avatar: contact.avatar,
            favorite: favorite ?? contact.favorite,
            phones: contact.phones.map {
                ModemDeckContactDraft.Phone(
                    id: $0.id,
                    label: $0.label,
                    number: $0.displayNumber,
                    region: $0.region,
                    primary: $0.primary
                )
            },
            notes: contact.notes,
            preferredLineId: contact.preferredLineId,
            revision: contact.revision
        )
    }

    private func nilIfEmpty(_ value: String) -> String? {
        let normalized = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return normalized.isEmpty ? nil : normalized
    }
}

struct ModemDeckDirectMessageView: View {
    let number: String
    let displayName: String
    var avatarSource: String? = nil
    var contactBound = false
    @ObservedObject var controller: ModemDeckSessionController
    var preferredLineID: String? = nil
    @Environment(\.presentationMode) private var presentationMode

    @State private var content = ""
    @State private var sending = false
    @State private var errorMessage = ""

    private var defaultLine: ModemDeckLine? {
        let id = preferredLineID ?? controller.bootstrap?.lineSettings.defaultLineId
        guard controller.bootstrap?.capabilities.message == true else { return nil }
        let lines = controller.bootstrap?.lines.filter { $0.capabilities?.message == true } ?? []
        return lines.first(where: { $0.id == id }) ?? lines.first
    }

    var body: some View {
        VStack(spacing: 0) {
            ModemDeckIdentityHeader(
                title: displayName,
                subtitle: number,
                avatarSource: avatarSource,
                channel: .message,
                contactBound: contactBound,
                backTitle: controller.text("返回", "Back"),
                copyItems: [
                    ModemDeckCopyItem(
                        label: controller.text("复制联系人", "Copy Contact"),
                        value: displayName
                    ),
                    ModemDeckCopyItem(
                        label: controller.text("复制号码", "Copy Number"),
                        value: number
                    )
                ]
            ) {
                presentationMode.wrappedValue.dismiss()
            }
            Spacer()
            ModemDeckInlineError(message: errorMessage)
                .padding(.horizontal, 16)
            composer
        }
        .background(Color.mdBackground)
        .navigationBarHidden(true)
        .modemDeckInteractiveBack()
    }

    private var composer: some View {
        HStack(alignment: .bottom, spacing: 10) {
            TextField(
                controller.text("短信内容", "Message"),
                text: $content,
                axis: .vertical
            )
                .lineLimit(1...5)
                .padding(.horizontal, 12)
                .frame(minHeight: 42)
                .background(Color.mdSurfaceHover)
                .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
            Button { send() } label: {
                Image(systemName: "arrow.up.circle.fill")
                    .font(.system(size: 32))
                    .foregroundColor(.mdAccent)
            }
            .disabled(
                content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                    sending || defaultLine == nil || !controller.isOnline
            )
        }
        .padding(12)
        .background(Color.mdSurface)
        .overlay(alignment: .top) { Rectangle().fill(Color.mdBorder).frame(height: 1) }
    }

    private func send() {
        guard controller.isOnline, let line = defaultLine else { return }
        let message = content.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !message.isEmpty else { return }
        sending = true
        Task {
            do {
                _ = try await controller.api.sendMessage(lineID: line.id, to: number, content: message)
                Task { await controller.messagesStore.load() }
                content = ""
                errorMessage = ""
            } catch {
                errorMessage = error.localizedDescription
            }
            sending = false
        }
    }
}

struct ModemDeckNewMessageView: View {
    @ObservedObject var controller: ModemDeckSessionController
    let onSent: () -> Void
    @Environment(\.presentationMode) private var presentationMode
    @State private var selectedLineID = ""
    @State private var recipient = ""
    @State private var content = ""
    @State private var sending = false
    @State private var errorMessage = ""

    private var lines: [ModemDeckLine] {
        guard controller.bootstrap?.capabilities.message == true else { return [] }
        return controller.bootstrap?.lines.filter { $0.capabilities?.message == true } ?? []
    }

    private var selectedLine: ModemDeckLine? {
        lines.first(where: { $0.id == selectedLineID }) ?? lines.first
    }

    private var valid: Bool {
        selectedLine != nil &&
            !recipient.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
            !content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text(controller.text("新建短信", "New Message"))
                    .font(.system(size: 19, weight: .bold))
                    .foregroundColor(.mdText)
                Spacer()
                Button {
                    presentationMode.wrappedValue.dismiss()
                } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 17, weight: .semibold))
                        .foregroundColor(.mdMuted)
                        .frame(width: 40, height: 40)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(controller.text("关闭", "Close"))
            }
            .padding(.horizontal, 18)
            .frame(height: 58)
            .background(Color.mdSurface)
            .overlay(alignment: .bottom) { Rectangle().fill(Color.mdBorder).frame(height: 1) }

            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    composeField(controller.text("发送线路", "Send From")) {
                        Picker("", selection: $selectedLineID) {
                            ForEach(lines) { line in
                                Text(line.displayName).tag(line.id)
                            }
                        }
                        .pickerStyle(.menu)
                        .tint(.mdAccent)
                    }

                    composeField(controller.text("收件人", "To")) {
                        TextField(controller.text("电话号码", "Phone number"), text: $recipient)
                            .keyboardType(.phonePad)
                            .textContentType(.telephoneNumber)
                    }

                    VStack(alignment: .leading, spacing: 7) {
                        Text(controller.text("短信内容", "Message"))
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundColor(.mdMuted)
                        TextEditor(text: $content)
                            .font(.system(size: 16))
                            .foregroundColor(.mdText)
                            .frame(minHeight: 150)
                            .padding(8)
                            .background(Color.mdSurface)
                            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                            .overlay(
                                RoundedRectangle(cornerRadius: 10, style: .continuous)
                                    .stroke(Color.mdBorder, lineWidth: 1)
                            )
                    }

                    if !errorMessage.isEmpty {
                        ModemDeckInlineError(message: errorMessage)
                    }
                }
                .frame(maxWidth: 680)
                .padding(18)
                .frame(maxWidth: .infinity)
            }
            .background(Color.mdBackground)

            Button {
                send()
            } label: {
                Group {
                    if sending {
                        ProgressView().tint(.white)
                    } else {
                        Label(controller.text("发送短信", "Send Message"), systemImage: "paperplane.fill")
                    }
                }
                .frame(maxWidth: .infinity, minHeight: 42)
            }
            .buttonStyle(.borderedProminent)
            .tint(.mdAccent)
            .frame(maxWidth: .infinity)
            .padding(14)
            .background(Color.mdSurface)
            .overlay(alignment: .top) { Rectangle().fill(Color.mdBorder).frame(height: 1) }
            .disabled(!valid || sending || !controller.isOnline)
        }
        .onAppear { chooseLine() }
        .onChange(of: lines.map(\.id)) { _ in chooseLine() }
        .interactiveDismissDisabled(sending)
    }

    @ViewBuilder
    private func composeField<Content: View>(
        _ title: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(title)
                .font(.system(size: 13, weight: .semibold))
                .foregroundColor(.mdMuted)
            content()
                .font(.system(size: 15))
                .foregroundColor(.mdText)
                .padding(.horizontal, 12)
                .frame(minHeight: 46)
                .background(Color.mdSurface)
                .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .stroke(Color.mdBorder, lineWidth: 1)
                )
        }
    }

    private func chooseLine() {
        guard selectedLineID.isEmpty || !lines.contains(where: { $0.id == selectedLineID }) else { return }
        let defaultID = controller.bootstrap?.lineSettings.defaultLineId ?? ""
        selectedLineID = lines.contains(where: { $0.id == defaultID }) ? defaultID : (lines.first?.id ?? "")
    }

    private func send() {
        guard controller.isOnline, let line = selectedLine, valid, !sending else { return }
        let number = recipient.trimmingCharacters(in: .whitespacesAndNewlines)
        let message = content.trimmingCharacters(in: .whitespacesAndNewlines)
        sending = true
        errorMessage = ""
        Task {
            do {
                _ = try await controller.api.sendMessage(lineID: line.id, to: number, content: message)
                Task { await controller.messagesStore.load() }
                onSent()
                presentationMode.wrappedValue.dismiss()
            } catch {
                errorMessage = error.localizedDescription
                sending = false
            }
        }
    }
}

struct ModemDeckMessagesView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @StateObject private var store: ModemDeckMessagesStore
    @Environment(\.modemDeckUsesSplitWorkspace) private var usesSplitWorkspace
    @Environment(\.modemDeckNavigate) private var navigate
    @State private var query = ""
    @State private var statusFilter = "all"
    @State private var lineFilter = ""
    @State private var favoriteOnly = false
    @State private var selecting = false
    @State private var selectedIDs = Set<String>()
    @State private var selectedThreadID: String?
    @State private var notificationThread: ModemDeckMessageThread?
    @State private var notificationLinkActive = false
    @State private var composeOpen = false
    @State private var batchBusy = false
    @State private var confirmBatchDelete = false

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        _store = StateObject(wrappedValue: controller.messagesStore)
    }

    private var filteredThreads: [ModemDeckMessageThread] {
        let normalized = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return store.threads.filter { thread in
            let unread = thread.unreadCount > 0 || thread.markedUnread
            let matchesQuery = normalized.isEmpty ||
                displayName(for: thread).localizedCaseInsensitiveContains(normalized) ||
                thread.peer.localizedCaseInsensitiveContains(normalized) ||
                (thread.lastContent?.localizedCaseInsensitiveContains(normalized) == true)
            let matchesStatus = statusFilter == "all" ||
                (statusFilter == "unread" && unread) ||
                (statusFilter == "read" && !unread)
            return matchesQuery && matchesStatus &&
                (lineFilter.isEmpty || thread.lineId == lineFilter) &&
                (!favoriteOnly || thread.favorite)
        }
    }

    private var selectedThread: ModemDeckMessageThread? {
        guard let id = selectedThreadID else { return nil }
        return store.threads.first(where: { $0.id == id })
    }

    private var selectedThreads: [ModemDeckMessageThread] {
        filteredThreads.filter { selectedIDs.contains($0.id) }
    }

    private func contact(for thread: ModemDeckMessageThread) -> ModemDeckContact? {
        store.contacts.modemDeckContact(id: thread.contactId, number: thread.peer)
    }

    private func displayName(for thread: ModemDeckMessageThread) -> String {
        contact(for: thread)?.displayName ?? thread.displayName
    }

    var body: some View {
        Group {
            if usesSplitWorkspace {
                HStack(spacing: 0) {
                    messageList
                        .frame(width: ModemDeckLayout.padListWidth)
                    Rectangle().fill(Color.mdBorder).frame(width: 1)
                    conversationDetail
                }
            } else {
                messageList
            }
        }
        .background(Color.mdBackground)
        .navigationBarHidden(true)
        .overlay(alignment: .bottom) {
            ModemDeckInlineError(message: store.errorMessage)
                .padding(.horizontal, 16)
        }
        .task {
            await store.load()
            applyRequestedThread()
        }
        .onChange(of: usesSplitWorkspace) { split in
            if !split, let selectedThreadID {
                navigate(.message(selectedThreadID))
                self.selectedThreadID = nil
            }
        }
        .onChange(of: controller.requestedMessageThreadKey) { _ in
            applyRequestedThread()
        }
        .onChange(of: store.threads.map(\.id)) { _ in
            applyRequestedThread()
        }
        .onChange(of: notificationLinkActive) { active in
            if !active { notificationThread = nil }
        }
        .onChange(of: filteredThreads.map(\.id)) { visibleIDs in
            guard selecting else { return }
            selectedIDs.formIntersection(Set(visibleIDs))
        }
        .navigationDestination(isPresented: $notificationLinkActive) {
            Group {
                if let thread = notificationThread {
                    ModemDeckConversationView(
                        thread: thread,
                        controller: controller,
                        contact: contact(for: thread),
                        onChanged: reloadThreads,
                        onDeleted: handleDeletedThread
                    )
                }
            }
        }
        .sheet(isPresented: $composeOpen) {
            ModemDeckNewMessageView(controller: controller) {
                Task { await store.load() }
            }
        }
        .alert(
            controller.text("删除所选会话？", "Delete Selected Conversations?"),
            isPresented: $confirmBatchDelete
        ) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {}
            Button(controller.text("删除", "Delete"), role: .destructive) {
                mutateSelectedThreads(.delete)
            }
        } message: {
            Text(controller.text(
                "将永久删除 \(selectedThreads.count) 个短信会话。",
                "This permanently deletes \(selectedThreads.count) conversations."
            ))
        }
    }

    private func applyRequestedThread() {
        guard let key = controller.requestedMessageThreadKey,
              let thread = store.threads.first(where: { $0.key == key }) else {
            return
        }
        if usesSplitWorkspace {
            selectedThreadID = thread.id
        } else {
            notificationThread = thread
            notificationLinkActive = true
        }
        controller.acceptRequestedMessage(threadKey: key)
    }

    private var messageList: some View {
        VStack(spacing: 0) {
            ModemDeckPageHeader(
                title: controller.text("消息", "Messages"),
                secondaryActionIcon: "envelope.open",
                secondaryActionAccessibilityText: controller.text("全部标为已读", "Mark All Read"),
                secondaryActionDisabled: batchBusy || !controller.isOnline ||
                    !filteredThreads.contains { $0.unreadCount > 0 || $0.markedUnread },
                secondaryAction: markAllMessagesRead,
                actionIcon: "square.and.pencil",
                actionAccessibilityText: controller.text("新建短信", "New message"),
                actionDisabled: !controller.isOnline
            ) { composeOpen = true }
            toolbar
            filterBar
            threadList
        }
        .background(Color.mdBackground)
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if selecting { messageBatchBar }
        }
    }

    @ViewBuilder
    private var conversationDetail: some View {
        if let thread = selectedThread {
            ModemDeckConversationView(
                thread: thread,
                controller: controller,
                contact: contact(for: thread),
                showsBackButton: false,
                onChanged: reloadThreads,
                onDeleted: handleDeletedThread
            )
            .id(thread.id)
        } else {
            ModemDeckWorkspaceEmptyView(
                icon: "tray",
                title: controller.text("选择会话", "Select a conversation"),
                detail: controller.text("短信会显示在这里。", "Messages appear here.")
            )
        }
    }

    private var toolbar: some View {
        HStack(spacing: ModemDeckLayout.toolbarGap) {
            ModemDeckToolbarButton(
                icon: selecting ? "xmark" : "checklist",
                active: selecting,
                accessibilityText: controller.text("选择消息", "Select messages")
            ) {
                selecting.toggle()
                if !selecting { selectedIDs.removeAll() }
            }
            ModemDeckSearchField(
                text: $query,
                prompt: controller.text("搜索", "Search"),
                clearAccessibilityText: controller.text("清除搜索", "Clear search")
            )
            ModemDeckLineFilterMenu(
                controller: controller,
                selection: $lineFilter,
                accessibilityText: controller.text("筛选线路", "Filter line")
            )
        }
        .modemDeckListToolbar()
    }

    private var filterBar: some View {
        HStack(spacing: ModemDeckLayout.toolbarGap) {
            ModemDeckSegmentPicker(
                options: [
                    .init(id: "all", title: controller.text("全部", "All")),
                    .init(id: "unread", title: controller.text("未读", "Unread")),
                    .init(id: "read", title: controller.text("已读", "Read"))
                ],
                selection: $statusFilter
            )
            ModemDeckToolbarButton(
                icon: favoriteOnly ? "star.fill" : "star",
                active: favoriteOnly,
                accessibilityText: controller.text("仅收藏", "Favorites only")
            ) {
                favoriteOnly.toggle()
            }
        }
        .modemDeckListFilterBar()
    }

    @ViewBuilder
    private var threadList: some View {
        if store.loading && store.threads.isEmpty {
            ProgressView(controller.text("正在载入消息…", "Loading messages…"))
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if !store.errorMessage.isEmpty && store.threads.isEmpty {
            ModemDeckLoadErrorState(
                controller: controller,
                detail: store.errorMessage
            ) {
                Task { await store.load() }
            }
        } else if filteredThreads.isEmpty {
            ScrollView {
                ModemDeckStateView(
                    icon: query.isEmpty ? "message" : "magnifyingglass",
                    title: store.threads.isEmpty
                        ? controller.text("还没有消息", "No Messages Yet")
                        : controller.text("没有匹配结果", "No Matches"),
                    detail: store.threads.isEmpty
                        ? controller.text("收到或发出短信后会显示在这里。", "Sent and received messages appear here.")
                        : controller.text("请调整搜索或筛选条件。", "Adjust the search or filters.")
                )
            }
            .refreshable { await store.load() }
        } else {
            List {
                ForEach(filteredThreads) { thread in
                    threadListRow(thread)
                        .listRowInsets(EdgeInsets())
                        .listRowSeparator(.hidden)
                        .listRowBackground(Color.mdSurface)
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .environment(\.defaultMinListRowHeight, 0)
            .background(Color.mdSurface)
            .refreshable { await store.load() }
        }
    }

    private func threadListRow(_ thread: ModemDeckMessageThread) -> some View {
        let unread = thread.unreadCount > 0 || thread.markedUnread
        return ModemDeckListRow(
            controller: controller, selecting: selecting,
            selected: selecting ? selectedIDs.contains(thread.id) : usesSplitWorkspace && selectedThreadID == thread.id,
            enabled: !batchBusy, unread: unread, favorite: thread.favorite,
            accessibilityID: "message-\(thread.id)",
            deleteMessage: controller.text("该会话中的全部短信将被永久删除。", "All messages in this conversation will be permanently deleted."),
            open: {
                if selecting {
                    if !selectedIDs.insert(thread.id).inserted { selectedIDs.remove(thread.id) }
                } else if usesSplitWorkspace {
                    selectedThreadID = thread.id
                } else { navigate(.message(thread.id)) }
            },
            toggleRead: { try await store.mutate(unread ? .read : .unread, threads: [thread]) },
            toggleFavorite: { try await store.mutate(thread.favorite ? .unfavorite : .favorite, threads: [thread]) },
            delete: { try await store.mutate(.delete, threads: [thread]) }
        ) { actions in
            ModemDeckMessageThreadRow(
                thread: thread, contact: contact(for: thread), controller: controller,
                contextActions: actions
            )
        }
    }

    private var messageBatchBar: some View {
        let selected = selectedThreads
        let hasUnread = selected.contains {
            $0.unreadCount > 0 || $0.markedUnread
        }
        let allFavorite = !selected.isEmpty && selected.allSatisfy(\.favorite)

        return ModemDeckBatchActionBar(
            selectedCount: selected.count,
            totalCount: filteredThreads.count,
            busy: batchBusy,
            selectedText: controller.text("已选择 %d 项", "%d selected"),
            selectAllText: controller.text("全选", "Select All"),
            clearAllText: controller.text("清除", "Clear"),
            doneText: controller.text("完成", "Done"),
            selectAll: toggleAllThreads,
            done: endMessageSelection
        ) {
            ModemDeckBatchActionButton(
                title: hasUnread
                    ? controller.text("标为已读", "Mark Read")
                    : controller.text("标为未读", "Mark Unread"),
                icon: hasUnread ? "envelope.open" : "envelope.badge",
                disabled: selected.isEmpty || batchBusy || !controller.isOnline
            ) { mutateSelectedThreads(hasUnread ? .read : .unread) }
            ModemDeckBatchActionButton(
                title: allFavorite
                    ? controller.text("取消收藏", "Unfavorite")
                    : controller.text("收藏", "Favorite"),
                icon: allFavorite ? "star.slash" : "star",
                disabled: selected.isEmpty || batchBusy || !controller.isOnline
            ) { mutateSelectedThreads(allFavorite ? .unfavorite : .favorite) }
            ModemDeckBatchActionButton(
                title: controller.text("删除", "Delete"),
                icon: "trash",
                destructive: true,
                disabled: selected.isEmpty || batchBusy || !controller.isOnline
            ) { confirmBatchDelete = true }
        }
    }

    private func toggleAllThreads() {
        let visible = Set(filteredThreads.map(\.id))
        if !visible.isEmpty && visible.isSubset(of: selectedIDs) {
            selectedIDs.subtract(visible)
        } else {
            selectedIDs.formUnion(visible)
        }
    }

    private func endMessageSelection() {
        selecting = false
        selectedIDs.removeAll()
    }

    private func mutateSelectedThreads(_ action: ModemDeckMessageThreadAction) {
        let threads = selectedThreads
        guard controller.isOnline, !threads.isEmpty, !batchBusy else { return }
        batchBusy = true
        Task {
            do {
                try await store.mutate(action, threads: threads)
                if action == .delete {
                    if let id = selectedThreadID, selectedIDs.contains(id) {
                        selectedThreadID = nil
                    }
                    endMessageSelection()
                }
                await store.load()
                store.errorMessage = ""
            } catch {
                store.errorMessage = error.localizedDescription
            }
            batchBusy = false
        }
    }

    private func markAllMessagesRead() {
        guard controller.isOnline, !batchBusy else { return }
        batchBusy = true
        Task {
            do {
                try await store.markAllRead(lineID: lineFilter)
                await store.load()
                store.errorMessage = ""
            } catch {
                store.errorMessage = error.localizedDescription
            }
            batchBusy = false
        }
    }

    private func reloadThreads() {
        Task { await store.load() }
    }

    private func handleDeletedThread(_ id: String) {
        if selectedThreadID == id { selectedThreadID = nil }
        selectedIDs.remove(id)
        reloadThreads()
    }
}

struct ModemDeckLineFilterMenu: View {
    @ObservedObject var controller: ModemDeckSessionController
    @Binding var selection: String
    let accessibilityText: String

    var body: some View {
        Menu {
            Button(controller.text("全部线路", "All lines")) { selection = "" }
            ForEach(controller.bootstrap?.lineCatalog ?? []) { line in
                Button(line.displayName) { selection = line.id }
            }
        } label: {
            ZStack {
                RoundedRectangle(cornerRadius: 9, style: .continuous)
                    .fill(selection.isEmpty ? Color.mdSurface : Color.mdAccentSoft)
                    .overlay(
                        RoundedRectangle(cornerRadius: 9, style: .continuous)
                            .stroke(
                                selection.isEmpty ? Color.mdBorder : Color.mdAccent.opacity(0.45),
                                lineWidth: 1
                            )
                    )
                Image(systemName: selection.isEmpty
                      ? "line.3.horizontal.decrease"
                      : "line.3.horizontal.decrease.circle.fill")
                    .font(.system(size: 17, weight: .medium))
                    .foregroundColor(selection.isEmpty ? .mdMuted : .mdAccent)
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
        .accessibilityLabel(accessibilityText)
        .accessibilityAddTraits(selection.isEmpty ? [] : .isSelected)
    }
}

struct ModemDeckMessageThreadRow: View {
    let thread: ModemDeckMessageThread
    var contact: ModemDeckContact? = nil
    @ObservedObject var controller: ModemDeckSessionController
    var contextActions: [ModemDeckContextAction] = []

    private var line: ModemDeckLine? {
        controller.bootstrap?.lineCatalog.first(where: { $0.id == thread.lineId })
    }

    private var displayName: String {
        contact?.displayName ?? thread.displayName
    }

    private var contactBound: Bool {
        contact != nil || !(thread.contactId?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ?? true)
    }

    private var unread: Bool {
        thread.unreadCount > 0 || thread.markedUnread
    }

    var body: some View {
        HStack(alignment: .center, spacing: 10) {
            Circle()
                .fill(Color.mdAccent)
                .frame(width: 8, height: 8)
                .opacity(unread ? 1 : 0)
                .accessibilityHidden(true)
            ModemDeckCommunicationAvatar(
                channel: .message,
                name: displayName,
                address: thread.peer,
                avatarSource: contact?.avatar,
                contactBound: contactBound,
                size: ModemDeckLayout.listAvatarSize
            )
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) {
                    Text(displayName)
                        .font(.subheadline.weight(unread ? .semibold : .regular))
                        .foregroundColor(.mdText)
                        .lineLimit(1)
                    Spacer(minLength: 6)
                    Text(controller.compactDateText(thread.lastTimestamp))
                        .font(.caption)
                        .foregroundColor(.mdFaint)
                        .monospacedDigit()
                }
                HStack(spacing: 7) {
                    if let line, (controller.bootstrap?.lineCatalog.count ?? 0) > 1 {
                        ModemDeckLineTag(line: line)
                    }
                    Text(thread.lastContent ?? "")
                        .font(.footnote.weight(unread ? .medium : .regular))
                        .foregroundColor(.mdMuted)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    if thread.favorite {
                        Image(systemName: "star.fill")
                            .font(.system(size: 12))
                            .foregroundColor(.orange)
                    }
                }
            }
        }
        .padding(.horizontal, ModemDeckLayout.listHorizontalPadding)
        .frame(minHeight: ModemDeckLayout.listRowMinHeight)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityValue(unread
            ? controller.text("未读", "Unread")
            : controller.text("已读", "Read"))
        .modemDeckCopyMenu([
            ModemDeckCopyItem(
                label: controller.text("复制联系人", "Copy Contact"),
                value: displayName
            ),
            ModemDeckCopyItem(
                label: controller.text("复制号码", "Copy Number"),
                value: thread.peer
            ),
            ModemDeckCopyItem(
                label: controller.text("复制短信", "Copy Message"),
                value: thread.lastContent ?? ""
            )
        ], actions: contextActions)
    }
}

struct ModemDeckConversationView: View {
    let thread: ModemDeckMessageThread
    @ObservedObject var controller: ModemDeckSessionController
    let contact: ModemDeckContact?
    let showsBackButton: Bool
    let onChanged: () -> Void
    let onDeleted: (String) -> Void
    @StateObject private var store: ModemDeckConversationStore
    @Environment(\.presentationMode) private var presentationMode
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @GestureState private var timestampDrag: CGFloat = 0
    @StateObject private var draft: ModemDeckMessageDraft
    @State private var favorite: Bool
    @State private var unread: Bool
    @State private var mutationBusy = false
    @State private var mutationError = ""
    @State private var confirmDelete = false
    @State private var initialPositionApplied = false
    @State private var bottomVisible = false
    @State private var readAcknowledgementBusy = false
    @State private var scrollToBottomRequest = 0
    @State private var hasNewMessagesBelow = false
    @State private var acknowledgedThroughID: Int64 = 0
    @State private var unreadDividerMessageID: Int64?

    init(
        thread: ModemDeckMessageThread,
        controller: ModemDeckSessionController,
        contact: ModemDeckContact? = nil,
        showsBackButton: Bool = true,
        onChanged: @escaping () -> Void = {},
        onDeleted: @escaping (String) -> Void = { _ in }
    ) {
        self.thread = thread
        self.controller = controller
        self.contact = contact
        self.showsBackButton = showsBackButton
        self.onChanged = onChanged
        self.onDeleted = onDeleted
        _draft = StateObject(wrappedValue: controller.messagesStore.draft(for: thread.id))
        _store = StateObject(
            wrappedValue: ModemDeckConversationStore(api: controller.api, thread: thread, messagesStore: controller.messagesStore)
        )
        _favorite = State(initialValue: thread.favorite)
        _unread = State(initialValue: thread.unreadCount > 0 || thread.markedUnread)
        _unreadDividerMessageID = State(initialValue: thread.firstUnreadMessageId)
    }

    private var dialLine: ModemDeckLine? {
        controller.voiceDialLine(preferredID: thread.lineId)
    }

    private var displayName: String {
        contact?.displayName ?? thread.displayName
    }

    private var contactBound: Bool {
        contact != nil || !(thread.contactId?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ?? true)
    }

    private var oneWaySender: Bool {
        let source = thread.peer.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !source.isEmpty, source.count <= 32 else { return false }
        let allowed = CharacterSet.letters
            .union(.decimalDigits)
            .union(CharacterSet(charactersIn: " ._-"))
        return source.unicodeScalars.contains { CharacterSet.letters.contains($0) } &&
            source.unicodeScalars.allSatisfy { allowed.contains($0) }
    }

    var body: some View {
        VStack(spacing: 0) {
            ModemDeckIdentityHeader(
                title: displayName,
                subtitle: thread.peer,
                avatarSource: contact?.avatar,
                channel: .message,
                contactBound: contactBound,
                showsBackButton: showsBackButton,
                backTitle: controller.text("返回消息", "Back to messages"),
                actions: threadHeaderActions,
                copyItems: [
                    ModemDeckCopyItem(
                        label: controller.text("复制联系人", "Copy Contact"),
                        value: displayName
                    ),
                    ModemDeckCopyItem(
                        label: controller.text("复制号码", "Copy Number"),
                        value: thread.peer
                    )
                ]
            ) {
                presentationMode.wrappedValue.dismiss()
            }

            if !mutationError.isEmpty {
                ModemDeckInlineError(message: mutationError)
                    .padding(.horizontal, 16)
                    .padding(.top, 8)
            }

            if store.loading && store.messages.isEmpty {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(spacing: 8) {
                            if store.hasMore {
                                Button {
                                    loadOlder(using: proxy)
                                } label: {
                                    if store.loadingOlder {
                                        ProgressView()
                                    } else {
                                        Text(controller.text("载入更早消息", "Load Earlier Messages"))
                                    }
                                }
                                .font(.footnote.weight(.medium))
                                .foregroundColor(.mdAccent)
                                .frame(minHeight: 38)
                                .disabled(store.loadingOlder)
                            }
                            if store.messages.isEmpty {
                                ModemDeckStateView(
                                    icon: "message",
                                    title: controller.text("没有消息", "No Messages"),
                                    detail: controller.text("发送第一条短信。", "Send the first message.")
                                )
                            }
                            ForEach(Array(store.messages.enumerated()), id: \.element.id) { index, message in
                                VStack(spacing: 8) {
                                    if showsDateDivider(at: index) {
                                        dateDivider(for: message)
                                    }
                                    if message.id == initialUnreadMessageID {
                                        unreadDivider
                                    }
                                    ModemDeckMessageBubble(
                                        message: message,
                                        controller: controller,
                                        timestampReveal: timestampReveal,
                                        showsDeliveryStatus: message.id == lastOutgoingMessageID ||
                                            message.deliveryStatus?.lowercased() == "failed"
                                    )
                                }
                                .id(message.id)
                            }
                            Color.clear
                                .frame(height: 1)
                                .id(conversationBottomID)
                                .onAppear {
                                    bottomVisible = true
                                    if initialPositionApplied {
                                        acknowledgeReadIfNeeded()
                                    }
                                }
                                .onDisappear { bottomVisible = false }
                        }
                        .padding(14)
                    }
                    .background(Color.mdBackground)
                    .simultaneousGesture(timestampRevealGesture)
                    .onAppear {
                        guard store.authoritativeLoadCompleted, !store.messages.isEmpty else { return }
                        applyInitialPosition(using: proxy)
                    }
                    .onChange(of: store.authoritativeLoadCompleted) { completed in
                        guard completed else { return }
                        applyInitialPosition(using: proxy)
                    }
                    .onChange(of: scrollToBottomRequest) { _ in
                        scrollToBottom(using: proxy, animated: true)
                    }
                    .overlay(alignment: .bottomTrailing) {
                        if hasNewMessagesBelow {
                            Button {
                                hasNewMessagesBelow = false
                                scrollToBottomRequest += 1
                                acknowledgeReadIfNeeded(through: latestMessageID, force: true)
                            } label: {
                                Label(
                                    controller.text("新消息", "New Messages"),
                                    systemImage: "arrow.down"
                                )
                                .font(.footnote.weight(.semibold))
                                .foregroundColor(.white)
                                .padding(.horizontal, 12)
                                .frame(minHeight: 36)
                                .background(Color.mdAccent)
                                .clipShape(Capsule())
                                .shadow(color: .black.opacity(0.12), radius: 8, y: 3)
                            }
                            .buttonStyle(.plain)
                            .padding(14)
                        }
                    }
                }
            }

            ModemDeckInlineError(message: store.errorMessage)
                .padding(.horizontal, 16)

            if oneWaySender {
                Label(
                    controller.text("此发送方不接受回复", "This sender does not accept replies"),
                    systemImage: "lock"
                )
                .font(.system(size: 14, weight: .medium))
                .foregroundColor(.mdMuted)
                .frame(maxWidth: .infinity, minHeight: 52)
                .background(Color.mdSurface)
                .overlay(alignment: .top) { Rectangle().fill(Color.mdBorder).frame(height: 1) }
            } else {
                HStack(alignment: .bottom, spacing: 10) {
                    TextField(
                        controller.text("短信内容", "Message"),
                        text: $draft.text,
                        axis: .vertical
                    )
                        .lineLimit(1...5)
                        .padding(.horizontal, 12)
                        .frame(minHeight: 42)
                        .background(Color.mdSurfaceHover)
                        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                    Button {
                        let value = draft.text
                        let revision = draft.revision
                        Task {
                            if await store.send(value) {
                                draft.clear(ifRevision: revision)
                                scrollToBottomRequest += 1
                            }
                        }
                    } label: {
                        Image(systemName: "arrow.up.circle.fill")
                            .font(.system(size: 32))
                            .foregroundColor(.mdAccent)
                    }
                    .disabled(
                        draft.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                            store.sending || !controller.isOnline
                    )
                }
                .padding(12)
                .background(Color.mdSurface)
                .overlay(alignment: .top) { Rectangle().fill(Color.mdBorder).frame(height: 1) }
            }
        }
        .navigationBarHidden(true)
        .modemDeckInteractiveBack(showsBackButton)
        .task {
            await store.load()
        }
        .onChange(of: thread) { value in
            favorite = value.favorite
            unread = value.unreadCount > 0 || value.markedUnread
        }
        .onReceive(NotificationCenter.default.publisher(for: .modemDeckRemoteNotification)) { _ in
            Task {
                let shouldFollowLatest = bottomVisible
                let previousIDs = Set(store.messages.map(\.id))
                await store.load()
                let receivedIncoming = store.messages.contains {
                    $0.incoming && !previousIDs.contains($0.id)
                }
                guard receivedIncoming else { return }
                unread = true
                if shouldFollowLatest {
                    scrollToBottomRequest += 1
                    acknowledgeReadIfNeeded(through: latestMessageID, force: true)
                } else {
                    hasNewMessagesBelow = true
                }
            }
        }
        .alert(
            controller.text("删除短信会话？", "Delete Conversation?"),
            isPresented: $confirmDelete
        ) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {}
            Button(controller.text("删除", "Delete"), role: .destructive) { deleteThread() }
        } message: {
            Text(controller.text("该会话中的短信将被永久删除。", "Messages in this conversation will be permanently deleted."))
        }
    }

    private var conversationBottomID: String {
        "conversation-bottom-\(thread.id)"
    }

    private var initialUnreadMessageID: Int64? {
        guard let id = unreadDividerMessageID, id > 0,
              store.messages.contains(where: { $0.id == id }) else { return nil }
        return id
    }

    private var latestMessageID: Int64? {
        store.messages.last?.id
    }

    private var latestIncomingMessageID: Int64? {
        store.messages.last(where: \.incoming)?.id
    }

    private var lastOutgoingMessageID: Int64? {
        store.messages.last(where: { !$0.incoming })?.id
    }

    private var timestampReveal: CGFloat {
        min(max(-timestampDrag, 0), 72)
    }

    private var timestampRevealGesture: some Gesture {
        DragGesture(minimumDistance: 14)
            .updating($timestampDrag) { value, state, _ in
                let horizontal = value.translation.width
                let vertical = value.translation.height
                guard horizontal < 0, abs(horizontal) > abs(vertical) * 1.2 else { return }
                state = max(horizontal, -72)
            }
    }

    private func showsDateDivider(at index: Int) -> Bool {
        guard store.messages.indices.contains(index) else { return false }
        guard index > 0 else { return true }
        return !ModemDeckDateText.sameDay(
            store.messages[index - 1].timestamp,
            store.messages[index].timestamp
        )
    }

    private func dateDivider(for message: ModemDeckMessage) -> some View {
        Text(controller.messageDayText(message.timestamp))
            .font(.caption.weight(.semibold))
            .foregroundColor(.mdMuted)
            .padding(.horizontal, 10)
            .frame(minHeight: 26)
            .background(Color.mdSurfaceHover.opacity(0.86))
            .clipShape(Capsule())
            .padding(.vertical, 7)
    }

    private var unreadDivider: some View {
        HStack(spacing: 10) {
            Rectangle().fill(Color.mdAccent.opacity(0.28)).frame(height: 1)
            Text(controller.text("未读消息", "Unread Messages"))
                .font(.system(size: 11, weight: .semibold))
                .foregroundColor(.mdAccentStrong)
                .fixedSize()
            Rectangle().fill(Color.mdAccent.opacity(0.28)).frame(height: 1)
        }
        .padding(.vertical, 2)
    }

    private func applyInitialPosition(using proxy: ScrollViewProxy) {
        guard !initialPositionApplied, store.authoritativeLoadCompleted else { return }
        initialPositionApplied = true
        let unreadID = initialUnreadMessageID
        let snapshotID = latestMessageID
        Task { @MainActor in
            await Task.yield()
            if let unreadID {
                proxy.scrollTo(unreadID, anchor: .top)
            } else {
                proxy.scrollTo(conversationBottomID, anchor: .bottom)
            }
            if unread {
                try? await Task.sleep(nanoseconds: 180_000_000)
                acknowledgeReadIfNeeded(through: snapshotID, force: true)
            }
        }
    }

    private func scrollToBottom(using proxy: ScrollViewProxy, animated: Bool) {
        Task { @MainActor in
            await Task.yield()
            if animated {
                withAnimation(reduceMotion ? nil : .easeOut(duration: 0.22)) {
                    proxy.scrollTo(conversationBottomID, anchor: .bottom)
                }
            } else {
                proxy.scrollTo(conversationBottomID, anchor: .bottom)
            }
        }
    }

    private func loadOlder(using proxy: ScrollViewProxy) {
        Task {
            guard let anchor = await store.loadOlder() else { return }
            await Task.yield()
            proxy.scrollTo(anchor, anchor: .top)
        }
    }

    private func acknowledgeReadIfNeeded(through messageID: Int64? = nil, force: Bool = false) {
        guard let target = messageID ?? latestMessageID, target > acknowledgedThroughID,
              controller.isOnline, (force || unread || hasNewMessagesBelow),
              !readAcknowledgementBusy else { return }
        readAcknowledgementBusy = true
        Task {
            let succeeded = await store.markRead(throughMessageID: target)
            if succeeded {
                acknowledgedThroughID = max(acknowledgedThroughID, target)
                let newerIncoming = (latestIncomingMessageID ?? 0) > target
                unread = newerIncoming
                if !newerIncoming { hasNewMessagesBelow = false }
                onChanged()
            }
            readAcknowledgementBusy = false
            if succeeded, bottomVisible, let latest = latestIncomingMessageID, latest > target {
                acknowledgeReadIfNeeded(through: latestMessageID, force: true)
            }
        }
    }

    private var threadHeaderActions: [ModemDeckHeaderAction] {
        [
            ModemDeckHeaderAction(
                id: "call",
                icon: "phone.fill",
                title: controller.text("呼叫", "Call"),
                color: .mdAccent,
                disabled: dialLine == nil || controller.callController.busy || !controller.isOnline
            ) { startCall() },
            ModemDeckHeaderAction(
                id: "favorite",
                icon: favorite ? "star.fill" : "star",
                title: favorite
                    ? controller.text("取消收藏", "Unfavorite")
                    : controller.text("收藏", "Favorite"),
                color: favorite ? .orange : .mdMuted,
                disabled: mutationBusy || !controller.isOnline
            ) { mutateThread(favorite ? .unfavorite : .favorite) },
            ModemDeckHeaderAction(
                id: "delete",
                icon: "trash",
                title: controller.text("删除会话", "Delete conversation"),
                color: .mdDanger,
                disabled: mutationBusy || !controller.isOnline
            ) { confirmDelete = true }
        ]
    }

    private func startCall() {
        guard controller.isOnline, let line = dialLine else { return }
        Task {
            await controller.callController.start(
                lineID: line.id,
                number: thread.peer,
                displayName: displayName,
                recording: nil
            )
        }
    }

    private func mutateThread(_ action: ModemDeckMessageThreadAction) {
        guard controller.isOnline, !mutationBusy else { return }
        mutationBusy = true
        mutationError = ""
        Task {
            do {
                try await controller.messagesStore.mutate(action, threads: [thread])
                switch action {
                case .read: unread = false
                case .unread: unread = true
                case .favorite: favorite = true
                case .unfavorite: favorite = false
                case .delete: break
                }
                onChanged()
            } catch {
                mutationError = error.localizedDescription
            }
            mutationBusy = false
        }
    }

    private func deleteThread() {
        guard controller.isOnline, !mutationBusy else { return }
        mutationBusy = true
        mutationError = ""
        Task {
            do {
                try await controller.messagesStore.mutate(.delete, threads: [thread])
                onDeleted(thread.id)
                if showsBackButton { presentationMode.wrappedValue.dismiss() }
            } catch {
                mutationError = error.localizedDescription
                mutationBusy = false
            }
        }
    }
}

private struct ModemDeckMessageBubble: View {
    let message: ModemDeckMessage
    @ObservedObject var controller: ModemDeckSessionController
    let timestampReveal: CGFloat
    let showsDeliveryStatus: Bool

    private var deliveryText: String? {
        guard !message.incoming, showsDeliveryStatus else { return nil }
        switch message.deliveryStatus?.lowercased() {
        case "delivered":
            return controller.text("已送达", "Delivered")
        case "submitted":
            return controller.text("已发送", "Sent")
        case "failed":
            return controller.text("发送失败", "Not Delivered")
        default:
            return message.failureCode?.isEmpty == false
                ? controller.text("发送失败", "Not Delivered")
                : nil
        }
    }

    private var deliveryFailed: Bool {
        message.deliveryStatus?.lowercased() == "failed" || message.failureCode?.isEmpty == false
    }

    var body: some View {
        ZStack(alignment: .trailing) {
            Text(controller.messageTimeText(message.timestamp))
                .font(.caption2.monospacedDigit())
                .foregroundColor(.mdFaint)
                .frame(width: 66, alignment: .trailing)
                .opacity(min(timestampReveal / 24, 1))
                .accessibilityHidden(true)

            HStack {
                if !message.incoming { Spacer(minLength: 48) }
                VStack(alignment: message.incoming ? .leading : .trailing, spacing: 4) {
                    Text(message.content)
                        .font(.body)
                        .foregroundColor(message.incoming ? .mdText : .white)
                        .textSelection(.enabled)
                        .padding(.horizontal, 13)
                        .padding(.vertical, 9)
                        .background(message.incoming ? Color.mdSurface : Color.mdAccent)
                        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
                    if let deliveryText {
                        Label(deliveryText, systemImage: deliveryFailed ? "exclamationmark.circle" : "checkmark")
                            .font(.caption2)
                            .foregroundColor(deliveryFailed ? .mdDanger : .mdFaint)
                    }
                }
                if message.incoming { Spacer(minLength: 48) }
            }
            .frame(maxWidth: .infinity)
            .background(Color.mdBackground)
            .offset(x: -timestampReveal)
        }
        .frame(maxWidth: .infinity)
        .clipped()
        .modemDeckCopyMenu([
            ModemDeckCopyItem(
                label: controller.text("复制短信", "Copy Message"),
                value: message.content
            )
        ])
        .accessibilityElement(children: .combine)
        .accessibilityLabel([
            message.incoming
                ? controller.text("收到", "Received")
                : controller.text("发出", "Sent"),
            message.content,
            controller.dateText(message.timestamp),
            deliveryText
        ].compactMap { $0 }.joined(separator: ", "))
    }
}
