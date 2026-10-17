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

private struct ModemDeckActivitySource: Equatable {
    let threads: [ModemDeckMessageThread]
    let calls: [ModemDeckCallRecord]
}

private struct ModemDeckActivityDay: Identifiable {
    let id: Date
    var items: [ModemDeckActivityItem]
}

private struct ModemDeckActivitySnapshot {
    var days: [ModemDeckActivityDay] = []
    var routes: Set<ModemDeckRoute> = []

    static func build(_ source: ModemDeckActivitySource) -> Self {
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let plain = ISO8601DateFormatter()
        let calendar = Calendar.current
        // Parse each timestamp once; sorting and grouping must never run inside
        // a List row builder or use the main actor's shared date formatters.
        let items = source.threads.map(ModemDeckActivityItem.message) + source.calls.map(ModemDeckActivityItem.call)
        var dated: [(item: ModemDeckActivityItem, date: Date)] = items.map { item in
            let date = fractional.date(from: item.timestamp) ?? plain.date(from: item.timestamp) ?? Date.distantPast
            return (item: item, date: date)
        }
        dated.sort { $0.date == $1.date ? $0.item.id < $1.item.id : $0.date > $1.date }
        var snapshot = Self()
        for entry in dated {
            let day = calendar.startOfDay(for: entry.date)
            if snapshot.days.last?.id != day { snapshot.days.append(.init(id: day, items: [])) }
            snapshot.days[snapshot.days.count - 1].items.append(entry.item)
            snapshot.routes.insert(entry.item.route)
        }
        return snapshot
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
    @State private var activity = ModemDeckActivitySnapshot()
    @State private var preparingActivity = true

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        messages = controller.messagesStore
        calls = controller.callsStore
        contacts = controller.contactsStore
    }

    private var activitySource: ModemDeckActivitySource {
        ModemDeckActivitySource(threads: messages.threads, calls: calls.calls)
    }

    private func dayTitle(_ day: Date) -> String {
        if Calendar.current.isDateInToday(day) { return controller.text("今天", "Today") }
        if Calendar.current.isDateInYesterday(day) { return controller.text("昨天", "Yesterday") }
        if day == Calendar.current.startOfDay(for: .distantPast) { return controller.text("更早", "Earlier") }
        return day.formatted(Date.FormatStyle().month().day().locale(Locale(identifier: controller.usesChinese ? "zh_CN" : "en_US")))
    }

    private var loadError: String {
        [messages.errorMessage, calls.errorMessage].filter { !$0.isEmpty }.joined(separator: "\n")
    }

    var body: some View {
        Group {
            if usesSplitWorkspace {
                HStack(spacing: 0) {
                    activityColumn.frame(width: ModemDeckLayout.padListWidth)
                    Rectangle().fill(Color.mdBorder).frame(width: 1)
                    detail
                }
            } else { activityColumn }
        }
        .background(Color.mdBackground)
        .navigationBarHidden(true)
        .task(id: activitySource) {
            let source = activitySource
            preparingActivity = true
            let snapshot = await Task.detached(priority: .userInitiated) {
                ModemDeckActivitySnapshot.build(source)
            }.value
            guard !Task.isCancelled else { return }
            activity = snapshot
            preparingActivity = false
            if let selectedRoute, !snapshot.routes.contains(selectedRoute) { self.selectedRoute = nil }
        }
    }

    private var activityColumn: some View {
        VStack(spacing: 0) {
            ModemDeckPageHeader(title: controller.text("最近", "Recents"))
            activityList
        }
    }

    private var shortcuts: some View {
        HStack(spacing: 8) {
            shortcut(controller.text("通话记录", "Calls"), icon: "phone", count: calls.calls.count, filter: "all")
            shortcut(controller.text("未接来电", "Missed"), icon: "phone.arrow.down.left", count: calls.calls.filter(\.missed).count,
                     filter: "missed", attention: calls.calls.contains { $0.missed && !$0.read })
            shortcut(controller.text("通话录音", "Recordings"), icon: "waveform", count: Set(calls.recordings.map { $0.call.id }).count, filter: "recorded")
        }
        .padding(.horizontal, 20)
        .padding(.bottom, 16)
    }

    /// Counter card: the number leads; it only turns red while unread missed calls are waiting.
    private func shortcut(_ title: String, icon: String, count: Int, filter: String, attention: Bool = false) -> some View {
        Button { controller.openCalls(filter: filter) } label: {
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Image(systemName: icon)
                        .symbolVariant(.fill)
                        .font(.subheadline.weight(.semibold))
                        .foregroundColor(attention ? .mdDanger : .mdAccent)
                    Spacer(minLength: 0)
                }
                Text(count.formatted())
                    .font(.title2.weight(.semibold).monospacedDigit())
                    .foregroundColor(attention ? .mdDanger : .mdText)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                Text(title).font(.footnote).foregroundColor(.mdMuted).lineLimit(1)
            }
            .padding(12)
            .frame(maxWidth: .infinity, minHeight: 92, alignment: .leading)
            .background(attention ? Color.mdDangerSoft : Color.mdSurface)
            .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.large))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .hoverEffect(.highlight)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("recent-calls-" + filter)
    }

    private var activityList: some View {
        List {
            VStack(alignment: .leading, spacing: 12) {
                Text(controller.text("我的线路", "My Lines"))
                    .font(.footnote.weight(.semibold)).foregroundColor(.mdMuted)
                    .accessibilityAddTraits(.isHeader)
                ModemDeckHomeLineGrid(controller: controller, lines: controller.bootstrap?.lines ?? [])
            }
            .padding(.horizontal, 20).padding(.top, 6).padding(.bottom, 16)
            .listRowInsets(EdgeInsets()).listRowSeparator(.hidden).listRowBackground(Color.mdBackground)
            shortcuts.listRowInsets(EdgeInsets()).listRowSeparator(.hidden).listRowBackground(Color.mdBackground)
            if activity.days.isEmpty && (preparingActivity || messages.loading || calls.loading) {
                ProgressView(controller.text("正在载入…", "Loading…"))
                    .frame(maxWidth: .infinity, minHeight: 140)
                    .listRowSeparator(.hidden)
            } else if activity.days.isEmpty && !loadError.isEmpty {
                ModemDeckLoadErrorState(controller: controller, detail: loadError) {
                    Task { await controller.refresh() }
                }
                .listRowSeparator(.hidden)
            } else if activity.days.isEmpty {
                ModemDeckStateView(
                    icon: "tray", title: controller.text("暂无活动", "No Activity Yet"),
                    detail: controller.text("短信与通话活动会显示在这里。", "Messages and calls appear here.")
                )
                .listRowSeparator(.hidden)
            } else {
                ForEach(activity.days) { day in
                    Section {
                        ForEach(day.items) { item in
                            activityRow(item)
                                .listRowInsets(EdgeInsets())
                                .listRowSeparator(.hidden)
                                .listRowBackground(Color.mdBackground)
                        }
                    } header: {
                        Text(dayTitle(day.id)).font(.caption.weight(.semibold)).foregroundColor(.mdMuted)
                            .textCase(nil).padding(.vertical, 4)
                    }
                }
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .environment(\.defaultMinListRowHeight, 0)
        .background(Color.mdBackground)
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
