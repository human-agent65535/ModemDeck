import SwiftUI

private enum ModemDeckActivityItem: Identifiable {
    case message(ModemDeckMessageThread)
    case call(ModemDeckCallRecord)

    var id: String {
        switch self {
        case .message(let thread): return "message-\(thread.id)"
        case .call(let call): return "call-\(call.id)"
        }
    }
    var route: ModemDeckRoute {
        switch self {
        case .message(let thread): return .message(thread.id)
        case .call(let call): return .call(call.id)
        }
    }
    var timestamp: String {
        switch self {
        case .message(let thread): return thread.lastTimestamp
        case .call(let call): return call.startedAt
        }
    }
    var unread: Bool? {
        switch self {
        case .message(let thread): return thread.unreadCount > 0 || thread.markedUnread
        case .call(let call): return call.missed ? !call.read : nil
        }
    }
    var favorite: Bool {
        switch self {
        case .message(let thread): return thread.favorite
        case .call(let call): return call.favorite
        }
    }
}

/// Home is a quick view of the shared communication stores. Search, filters
/// and bulk management belong to the dedicated pages; row actions stay shared.
struct ModemDeckHomeView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @ObservedObject private var messages: ModemDeckMessagesStore
    @ObservedObject private var calls: ModemDeckCallsStore
    @ObservedObject private var contacts: ModemDeckContactsStore
    @Environment(\.modemDeckUsesSplitWorkspace) private var usesSplitWorkspace
    @Environment(\.modemDeckNavigate) private var navigate
    @State private var selectedRoute: ModemDeckRoute?

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        messages = controller.messagesStore
        calls = controller.callsStore
        contacts = controller.contactsStore
    }

    private var items: [ModemDeckActivityItem] {
        (messages.threads.map(ModemDeckActivityItem.message) + calls.calls.map(ModemDeckActivityItem.call))
            .sorted {
                let lhs = ModemDeckDateText.date($0.timestamp) ?? .distantPast
                let rhs = ModemDeckDateText.date($1.timestamp) ?? .distantPast
                return lhs == rhs ? $0.id < $1.id : lhs > rhs
            }
    }
    private var loadError: String {
        [messages.errorMessage, calls.errorMessage].filter { !$0.isEmpty }.joined(separator: "\n")
    }

    var body: some View {
        Group {
            if usesSplitWorkspace {
                HStack(spacing: 0) {
                    activityList.frame(width: ModemDeckLayout.padListWidth)
                    Rectangle().fill(Color.mdBorder).frame(width: 1)
                    detail
                }
            } else { activityList }
        }
        .background(Color.mdBackground)
        .navigationBarHidden(true)
        .task { await controller.refreshCollections() }
        .onChange(of: items.map(\.route)) { routes in
            if let selectedRoute, !routes.contains(selectedRoute) { self.selectedRoute = nil }
        }
    }

    private var activityList: some View {
        List {
            if !(controller.bootstrap?.lines.isEmpty ?? true) {
                ModemDeckHomeLineGrid(controller: controller, lines: controller.bootstrap?.lines ?? [])
                    .padding(10)
                    .listRowInsets(EdgeInsets())
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color.mdBackground)
            }
            if items.isEmpty && (messages.loading || calls.loading) {
                ProgressView(controller.text("正在载入…", "Loading…"))
                    .frame(maxWidth: .infinity, minHeight: 140)
                    .listRowSeparator(.hidden)
            } else if items.isEmpty && !loadError.isEmpty {
                ModemDeckLoadErrorState(controller: controller, detail: loadError) {
                    Task { await controller.refresh() }
                }
                .listRowSeparator(.hidden)
            } else if items.isEmpty {
                ModemDeckStateView(
                    icon: "tray", title: controller.text("暂无活动", "No Activity Yet"),
                    detail: controller.text("短信与通话活动会显示在这里。", "Messages and calls appear here.")
                )
                .listRowSeparator(.hidden)
            } else {
                ForEach(items) { item in
                    activityRow(item)
                        .listRowInsets(EdgeInsets())
                        .listRowSeparator(.hidden)
                        .listRowBackground(Color.mdSurface)
                }
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .environment(\.defaultMinListRowHeight, 0)
        .background(Color.mdSurface)
        .refreshable { await controller.refresh() }
    }

    @ViewBuilder private var detail: some View {
        if let selectedRoute {
            ModemDeckRouteContent(route: selectedRoute, controller: controller, showsBackButton: false)
                .id(selectedRoute)
        } else {
            ModemDeckWorkspaceEmptyView(
                icon: "tray", title: controller.text("选择短信或通话", "Select a message or call"),
                detail: controller.text("详情会显示在这里。", "Details appear here.")
            )
        }
    }

    private func contact(for item: ModemDeckActivityItem) -> ModemDeckContact? {
        switch item {
        case .message(let thread): return contacts.contacts.modemDeckContact(id: thread.contactId, number: thread.peer)
        case .call(let call): return contacts.contacts.modemDeckContact(id: call.contactId, number: call.remoteNumber)
        }
    }

    private func activityRow(_ item: ModemDeckActivityItem) -> some View {
        ModemDeckListRow(
            controller: controller,
            selected: usesSplitWorkspace && selectedRoute == item.route,
            unread: item.unread, favorite: item.favorite,
            accessibilityID: "activity-\(item.id)",
            deleteMessage: {
                switch item {
                case .message: return controller.text("该会话中的全部短信将被永久删除。", "All messages in this conversation will be permanently deleted.")
                case .call: return controller.text("将永久删除此通话记录及其录音。", "This permanently deletes the call record and its recordings.")
                }
            }(),
            open: {
                if usesSplitWorkspace { selectedRoute = item.route }
                else { navigate(item.route) }
            },
            toggleRead: item.unread != nil ? { try await mutate(item, action: item.unread == true ? .read : .unread) } : nil,
            toggleFavorite: { try await mutate(item, action: item.favorite ? .unfavorite : .favorite) },
            delete: { try await mutate(item, action: .delete) }
        ) { actions in
            switch item {
            case .message(let thread):
                ModemDeckMessageThreadRow(thread: thread, contact: contact(for: item), controller: controller, contextActions: actions)
            case .call(let call):
                ModemDeckCallRecordRow(
                    call: call, contact: contact(for: item), controller: controller,
                    recorded: calls.recordings.contains { $0.call.id == call.id && $0.playable },
                    contextActions: actions
                )
            }
        }
    }

    private func mutate(_ item: ModemDeckActivityItem, action: ModemDeckMessageThreadAction) async throws {
        switch item {
        case .message(let thread): try await messages.mutate(action, threads: [thread])
        case .call(let call):
            if let action = ModemDeckCallBatchAction(rawValue: action.rawValue) {
                try await calls.mutate(action, calls: [call])
            }
        }
    }

}
