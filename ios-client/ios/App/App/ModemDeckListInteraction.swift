import SwiftUI

enum ModemDeckRoute: Hashable {
    case contact(String)
    case message(String)
    case call(String)
    case recording(String)
}

private struct ModemDeckNavigateKey: EnvironmentKey {
    static let defaultValue: (ModemDeckRoute) -> Void = { _ in }
}

extension EnvironmentValues {
    var modemDeckNavigate: (ModemDeckRoute) -> Void {
        get { self[ModemDeckNavigateKey.self] }
        set { self[ModemDeckNavigateKey.self] = newValue }
    }
}

/// Value navigation is registered once outside lazy lists. Rows remain native
/// buttons without an extra disclosure accessory or hidden NavigationLink.
struct ModemDeckRouteContent: View {
    let route: ModemDeckRoute
    @ObservedObject var controller: ModemDeckSessionController
    var showsBackButton = true
    @Environment(\.dismiss) private var dismiss
    @ObservedObject private var contacts: ModemDeckContactsStore
    @ObservedObject private var messages: ModemDeckMessagesStore
    @ObservedObject private var calls: ModemDeckCallsStore

    init(route: ModemDeckRoute, controller: ModemDeckSessionController, showsBackButton: Bool = true) {
        self.route = route
        self.controller = controller
        self.showsBackButton = showsBackButton
        contacts = controller.contactsStore
        messages = controller.messagesStore
        calls = controller.callsStore
    }

    @ViewBuilder var body: some View {
        switch route {
        case .contact(let id):
            if let contact = contacts.contacts.first(where: { $0.id == id }) {
                ModemDeckContactDetailView(
                    contact: contact, controller: controller, showsBackButton: showsBackButton,
                    onChanged: contacts.upsert, onDeleted: { contacts.remove(ids: [$0]) }
                )
            } else { missingDetail }
        case .message(let id):
            if let thread = messages.threads.first(where: { $0.id == id }) {
                ModemDeckConversationView(
                    thread: thread, controller: controller,
                    contact: contacts.contacts.modemDeckContact(id: thread.contactId, number: thread.peer),
                    showsBackButton: showsBackButton
                )
            } else { missingDetail }
        case .call(let id):
            if let call = calls.calls.first(where: { $0.id == id }) {
                ModemDeckCallDetailView(
                    call: call, recordings: calls.recordings.filter { $0.call.id == id },
                    controller: controller,
                    contact: contacts.contacts.modemDeckContact(id: call.contactId, number: call.remoteNumber),
                    showsBackButton: showsBackButton
                )
            } else { missingDetail }
        case .recording(let id):
            if let recording = calls.recordings.first(where: { $0.id == id }) {
                ModemDeckRecordingDetailView(
                    recording: recording, controller: controller,
                    contact: contacts.contacts.modemDeckContact(id: recording.call.contactId, number: recording.call.remoteNumber),
                    showsBackButton: showsBackButton
                )
            } else { missingDetail }
        }
    }

    private var missingDetail: some View {
        VStack(spacing: 0) {
            if showsBackButton {
                Button { dismiss() } label: {
                    Label(controller.text("返回", "Back"), systemImage: "chevron.left")
                        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .padding(.horizontal, 14)
                .background(Color.mdSurface)
            }
            ModemDeckStateView(
                icon: "doc", title: controller.text("条目已不可用", "Item Unavailable"),
                detail: controller.text("该条目可能已在另一台设备上删除。", "This item may have been deleted on another device.")
            )
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .background(Color.mdBackground)
        .navigationBarHidden(true)
        .modemDeckInteractiveBack(showsBackButton)
    }
}

struct ModemDeckContextAction: Identifiable {
    let title: String
    let icon: String
    var destructive = false
    var disabled = false
    let perform: () -> Void
    var id: String { icon }
}

/// Shared row interaction for Home and every communication collection. The
/// content owns its copy values; this owner supplies identical contextual and
/// swipe actions, selection feedback, and destructive confirmation.
struct ModemDeckListRow<Content: View>: View {
    @ObservedObject var controller: ModemDeckSessionController
    var selecting = false
    var selected = false
    var enabled = true
    var unread: Bool? = nil
    var favorite = false
    var accessibilityID = ""
    let deleteMessage: String
    let open: () -> Void
    var toggleRead: (() async throws -> Void)? = nil
    let toggleFavorite: () async throws -> Void
    let delete: () async throws -> Void
    @ViewBuilder let content: ([ModemDeckContextAction]) -> Content
    @State private var busy = false
    @State private var confirmingDelete = false
    @State private var errorMessage = ""

    private var unavailable: Bool { !controller.isOnline || !enabled || busy }
    private var readTitle: String {
        unread == true ? controller.text("标为已读", "Mark Read") : controller.text("标为未读", "Mark Unread")
    }
    private var readIcon: String { unread == true ? "envelope.open" : "envelope.badge" }

    private var menuActions: [ModemDeckContextAction] {
        guard !selecting else { return [] }
        var actions: [ModemDeckContextAction] = []
        if let toggleRead {
            actions.append(.init(title: readTitle, icon: readIcon, disabled: unavailable) { perform(toggleRead) })
        }
        actions.append(.init(
            title: favorite ? controller.text("取消收藏", "Unfavorite") : controller.text("收藏", "Favorite"),
            icon: favorite ? "star.slash" : "star", disabled: unavailable
        ) { perform(toggleFavorite) })
        actions.append(.init(
            title: controller.text("删除", "Delete"), icon: "trash", destructive: true, disabled: unavailable
        ) { confirmingDelete = true })
        return actions
    }

    var body: some View {
        VStack(spacing: 0) {
            Button(action: open) {
                HStack(spacing: 0) {
                    if selecting {
                        ModemDeckSelectionMark(selected: selected)
                            .padding(.leading, ModemDeckLayout.listHorizontalPadding)
                    }
                    content(menuActions)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(!enabled || busy)
            .accessibilityIdentifier(accessibilityID)
            .accessibilityAddTraits(selected ? .isSelected : [])
            .accessibilityValue([
                unread.map { $0 ? controller.text("未读", "Unread") : controller.text("已读", "Read") },
                favorite ? controller.text("已收藏", "Favorite") : nil
            ].compactMap { $0 }.joined(separator: ", "))
            ModemDeckListDivider()
        }
        .background(selected ? Color.mdSelected : Color.mdSurface)
        .swipeActions(edge: .leading, allowsFullSwipe: true) {
            if !selecting, let toggleRead {
                Button { perform(toggleRead) } label: { Label(readTitle, systemImage: readIcon) }
                    .tint(.mdAccent)
                    .disabled(unavailable)
            }
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if !selecting {
                // No destructive role here: native rows must not disappear
                // until the user confirms and the server accepts deletion.
                Button { confirmingDelete = true } label: {
                    Label(controller.text("删除", "Delete"), systemImage: "trash")
                }
                .tint(.red)
                .disabled(unavailable)
            }
        }
        .alert(controller.text("确认删除？", "Confirm Delete?"), isPresented: $confirmingDelete) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {}
            Button(controller.text("删除", "Delete"), role: .destructive) { perform(delete) }
        } message: { Text(deleteMessage) }
        .alert(controller.text("操作失败", "Action Failed"), isPresented: Binding(
            get: { !errorMessage.isEmpty }, set: { if !$0 { errorMessage = "" } }
        )) {
            Button(controller.text("好", "OK"), role: .cancel) { errorMessage = "" }
        } message: { Text(errorMessage) }
    }

    private func perform(_ action: @escaping () async throws -> Void) {
        guard !unavailable else { return }
        busy = true
        Task {
            defer { busy = false }
            do { try await action() } catch { errorMessage = error.localizedDescription }
        }
    }
}
