import SwiftUI
import UIKit
import AVFAudio

private enum ModemDeckDialTargetError: Equatable {
    case none
    case required
    case tooLong
    case invalidCharacter
    case invalidLength
}

private struct ModemDeckDialTarget {
    let original: String
    let normalized: String
    let error: ModemDeckDialTargetError
}

@MainActor
private final class ModemDeckDTMFTonePlayer {
    static let shared = ModemDeckDTMFTonePlayer()

    private let engine = AVAudioEngine()
    private let player = AVAudioPlayerNode()
    private var prepared = false
    private var stopWorkItem: DispatchWorkItem?

    private let frequencies: [String: (Double, Double)] = [
        "1": (697, 1209), "2": (697, 1336), "3": (697, 1477),
        "4": (770, 1209), "5": (770, 1336), "6": (770, 1477),
        "7": (852, 1209), "8": (852, 1336), "9": (852, 1477),
        "*": (941, 1209), "0": (941, 1336), "#": (941, 1477)
    ]

    func play(_ digit: String) {
        guard let pair = frequencies[digit] else { return }
        let sampleRate = 44_100.0
        let duration = 0.12
        let frameCount = AVAudioFrameCount(sampleRate * duration)
        guard let format = AVAudioFormat(
            standardFormatWithSampleRate: sampleRate,
            channels: 1
        ), let buffer = AVAudioPCMBuffer(
            pcmFormat: format,
            frameCapacity: frameCount
        ), let samples = buffer.floatChannelData?[0] else {
            return
        }
        buffer.frameLength = frameCount

        for frame in 0..<Int(frameCount) {
            let time = Double(frame) / sampleRate
            let envelope: Double
            if time < 0.008 {
                envelope = time / 0.008
            } else if time > 0.085 {
                envelope = max(0, (duration - time) / (duration - 0.085))
            } else {
                envelope = 1
            }
            let mixed = sin(2 * .pi * pair.0 * time) + sin(2 * .pi * pair.1 * time)
            samples[frame] = Float(mixed * 0.055 * envelope)
        }

        do {
            if !prepared {
                engine.attach(player)
                engine.connect(player, to: engine.mainMixerNode, format: format)
                prepared = true
            }
            if !engine.isRunning {
                try engine.start()
            }
            player.stop()
            player.scheduleBuffer(buffer, at: nil, options: .interrupts)
            player.play()
            stopWorkItem?.cancel()
            let stopWorkItem = DispatchWorkItem { [weak self] in
                guard let self else { return }
                self.player.stop()
                self.engine.stop()
                self.stopWorkItem = nil
            }
            self.stopWorkItem = stopWorkItem
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.25, execute: stopWorkItem)
        } catch {
            // Dialing remains available when local key feedback cannot play.
        }
    }
}

private struct ModemDeckDialKeyStyle: ButtonStyle {
    let size: CGFloat
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .frame(maxWidth: .infinity, minHeight: size)
            .background(configuration.isPressed ? Color.mdBorder : Color.mdSurfaceHover)
            .clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
            .scaleEffect(configuration.isPressed ? 0.96 : 1)
            .animation(
                reduceMotion ? nil : .easeOut(duration: 0.08),
                value: configuration.isPressed
            )
    }
}

private struct ModemDeckDialKeyButton: View {
    let digit: String
    let letters: String
    let size: CGFloat
    let action: (String) -> Void
    @State private var zeroLongPressTriggered = false

    var body: some View {
        Button {
            if digit == "0", zeroLongPressTriggered {
                zeroLongPressTriggered = false
            } else {
                action(digit)
            }
        } label: {
            VStack(spacing: digit == "0" ? 1 : 3) {
                Text(digit)
                    .font(.system(size: 30, weight: .regular))
                    .lineLimit(1)
                if !letters.isEmpty {
                    Text(letters)
                        .font(.system(size: 9, weight: .medium))
                        .tracking(digit == "0" ? 0 : 1.1)
                        .lineLimit(1)
                }
            }
            .foregroundColor(.mdText)
        }
        .buttonStyle(ModemDeckDialKeyStyle(size: size))
        .simultaneousGesture(
            LongPressGesture(minimumDuration: 0.5).onEnded { _ in
                guard digit == "0" else { return }
                zeroLongPressTriggered = true
                action("+")
            }
        )
        .accessibilityLabel(letters.isEmpty ? digit : "\(digit) \(letters)")
        .accessibilityHint(
            digit == "0" ? "Long press to enter plus" : ""
        )
    }
}

private func normalizeModemDeckDialTarget(_ value: String) -> ModemDeckDialTarget {
    let original = value.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !original.isEmpty else {
        return ModemDeckDialTarget(original: "", normalized: "", error: .required)
    }
    let characters = Array(original)
    guard characters.count <= 64 else {
        return ModemDeckDialTarget(original: original, normalized: "", error: .tooLong)
    }

    var normalized = ""
    var digitCount = 0
    for (index, character) in characters.enumerated() {
        if character.unicodeScalars.count == 1,
           let scalar = character.unicodeScalars.first,
           (48...57).contains(scalar.value) {
            normalized.append(character)
            digitCount += 1
            continue
        }
        if character == "+", index == 0 {
            normalized.append(character)
            continue
        }
        let isControl = character.unicodeScalars.contains {
            $0.value <= 0x1f || (0x7f...0x9f).contains($0.value)
        }
        let isSeparator = character.isWhitespace || "().-/".contains(character)
        if isControl || !isSeparator {
            return ModemDeckDialTarget(
                original: original,
                normalized: "",
                error: .invalidCharacter
            )
        }
    }
    guard digitCount > 0, digitCount <= 32 else {
        return ModemDeckDialTarget(
            original: original,
            normalized: "",
            error: .invalidLength
        )
    }
    return ModemDeckDialTarget(original: original, normalized: normalized, error: .none)
}

/// The session owns drafts; dismissing a presentation never destroys an edit.
@MainActor
final class ModemDeckDialDraft: ObservableObject {
    @Published var number = ""
    @Published var selectedLineID = ""
    @Published var recording = false
    var recordingInitialized = false
    var lineSelectionOverridden = false

    func clear() {
        number = ""
        selectedLineID = ""
        recording = false
        recordingInitialized = false
        lineSelectionOverridden = false
    }
}

/// Uses native text selection and paste, with the app keypad as its input surface.
private struct ModemDeckDialNumberField: UIViewRepresentable {
    @Binding var text: String
    let placeholder: String
    let accessibilityLabel: String

    final class Coordinator: NSObject, UITextFieldDelegate {
        var parent: ModemDeckDialNumberField
        init(_ parent: ModemDeckDialNumberField) { self.parent = parent }
        @objc func changed(_ field: UITextField) { parent.text = String((field.text ?? "").prefix(64)) }
    }
    func makeCoordinator() -> Coordinator { Coordinator(self) }
    func makeUIView(context: Context) -> UITextField {
        let field = UITextField()
        field.delegate = context.coordinator
        field.addTarget(context.coordinator, action: #selector(Coordinator.changed(_:)), for: .editingChanged)
        field.inputView = UIView(frame: .zero)
        field.textAlignment = .center
        field.font = UIFontMetrics(forTextStyle: .largeTitle).scaledFont(for: .systemFont(ofSize: 34))
        field.adjustsFontForContentSizeCategory = true
        field.adjustsFontSizeToFitWidth = true
        field.minimumFontSize = 18
        field.textContentType = .telephoneNumber
        field.keyboardType = .phonePad
        field.autocorrectionType = .no
        field.accessibilityIdentifier = "dial-number"
        field.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        return field
    }
    func updateUIView(_ field: UITextField, context: Context) {
        context.coordinator.parent = self
        if field.text != text { field.text = text }
        field.textColor = UIColor(Color.mdText)
        field.tintColor = UIColor(Color.mdAccent)
        field.attributedPlaceholder = NSAttributedString(string: placeholder, attributes: [
            .foregroundColor: UIColor(Color.mdMuted), .font: UIFont.preferredFont(forTextStyle: .title2)
        ])
        field.accessibilityLabel = accessibilityLabel
    }
}

struct ModemDeckDialerPanel: View {
    @ObservedObject var controller: ModemDeckSessionController
    let close: () -> Void
    var floating = false

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text(controller.text("拨号", "Dial")).font(.headline).foregroundColor(.mdText)
                Spacer()
                Button(action: close) {
                    Image(systemName: "chevron.down")
                        .foregroundColor(.mdMuted)
                        .frame(width: 44, height: 44)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(controller.text("收起拨号盘", "Collapse dialer"))
                .accessibilityIdentifier("dialer-collapse")
            }
            .padding(.horizontal, 24)
            .padding(.top, 14)
            ModemDeckDialerView(controller: controller)
        }
        .background(Color.mdSurface.ignoresSafeArea())
        .accessibilityAction(.escape, close)
    }
}

struct ModemDeckDialerPage: View {
    @ObservedObject var controller: ModemDeckSessionController
    var body: some View {
        VStack(spacing: 0) {
            ModemDeckPageHeader(title: controller.text("拨号", "Dial"))
            ModemDeckDialerView(controller: controller)
        }
        .background(Color.mdBackground)
        .navigationBarHidden(true)
    }
}

struct ModemDeckDialerView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @ObservedObject private var callController: ModemDeckCallController
    @ObservedObject private var contactsStore: ModemDeckContactsStore
    @ObservedObject private var draft: ModemDeckDialDraft
    @State private var showingContacts = false
    @State private var contactQuery = ""
    @State private var recordingError = ""
    @State private var validationAttempted = false

    private let keys = [
        ("1", ""), ("2", "ABC"), ("3", "DEF"),
        ("4", "GHI"), ("5", "JKL"), ("6", "MNO"),
        ("7", "PQRS"), ("8", "TUV"), ("9", "WXYZ"),
        ("*", ""), ("0", "+"), ("#", "")
    ]

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        _callController = ObservedObject(wrappedValue: controller.callController)
        _contactsStore = ObservedObject(wrappedValue: controller.contactsStore)
        _draft = ObservedObject(wrappedValue: controller.dialDraft)
    }

    private var lines: [ModemDeckLine] { controller.voiceDialLines }
    private var selectedLine: ModemDeckLine? { lines.first { $0.id == draft.selectedLineID } }
    private var dialTarget: ModemDeckDialTarget { normalizeModemDeckDialTarget(draft.number) }
    private var matchedContact: ModemDeckContact? {
        contactsStore.contacts.modemDeckContact(id: nil, number: draft.number)
    }
    private var canDial: Bool {
        dialTarget.error == .none && selectedLine != nil && controller.isOnline &&
        !callController.busy && callController.call == nil
    }

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                VStack(spacing: 3) {
                    ModemDeckDialNumberField(text: $draft.number,
                        placeholder: controller.text("输入号码", "Enter number"),
                        accessibilityLabel: controller.text("电话号码", "Phone number"))
                        .frame(height: 54)
                    Text(matchedContact?.displayName ?? controller.text("选择联系人，或直接输入号码", "Choose a contact or enter a number"))
                        .font(.footnote).foregroundColor(.mdMuted)
                        .lineLimit(1).frame(minHeight: 24)
                }
                HStack(spacing: 16) {
                    Menu {
                        ForEach(lines) { line in
                            Button {
                                draft.selectedLineID = line.id
                                draft.lineSelectionOverridden = true
                            } label: {
                                if line.id == draft.selectedLineID {
                                    Label(line.displayName, systemImage: "checkmark")
                                } else { Text(line.displayName) }
                            }
                        }
                    } label: {
                        HStack(spacing: 6) {
                            Text(selectedLine?.displayName ?? controller.text("无可用线路", "No available line"))
                                .lineLimit(1)
                            Image(systemName: "chevron.down").font(.caption2)
                        }
                        .font(.subheadline).foregroundColor(.mdText)
                        .padding(.horizontal, 12).frame(minHeight: 44)
                        .background(Color.mdSurfaceHover)
                        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                    .disabled(lines.isEmpty)
                    .accessibilityLabel(controller.text("通话线路", "Calling line"))
                    .accessibilityValue(selectedLine?.displayName ?? "")
                    Toggle(controller.text("本次录音", "Record call"), isOn: $draft.recording)
                        .font(.footnote).tint(.mdAccent).fixedSize()
                        .disabled(!draft.recordingInitialized)
                        .accessibilityIdentifier("dial-recording")
                }
                .frame(minHeight: 44)

                LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 14), count: 3), spacing: 10) {
                    ForEach(Array(keys.enumerated()), id: \.offset) { _, key in
                        ModemDeckDialKeyButton(digit: key.0, letters: key.1, size: 62, action: appendDigit)
                            .accessibilityIdentifier("dial-key-\(key.0)")
                    }
                }
                HStack(spacing: 14) {
                    Button { showingContacts = true } label: {
                        Image(systemName: "person.crop.circle.badge.plus")
                            .font(.title3).frame(width: 44, height: 56)
                    }
                    .accessibilityLabel(controller.text("选择联系人", "Choose contact"))
                    Button(action: startCall) {
                        HStack(spacing: 10) {
                            if callController.busy { ProgressView().tint(.mdOnAccent) }
                            else { Image(systemName: "phone.fill") }
                            Text(controller.text("拨打", "Call"))
                        }
                        .font(.headline).foregroundColor(.mdOnAccent)
                        .frame(maxWidth: .infinity, minHeight: 56)
                        .background(Color.mdAccent.opacity(canDial ? 1 : 0.4))
                        .clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
                    }
                    .disabled(!canDial)
                    .accessibilityIdentifier("dial-call")
                    Button(action: removeDigit) {
                        Image(systemName: "delete.left")
                            .font(.title3).frame(width: 44, height: 56)
                    }
                    .disabled(draft.number.isEmpty)
                    .accessibilityLabel(controller.text("删除一位", "Delete digit"))
                    .accessibilityIdentifier("dial-delete")
                }
                .buttonStyle(.plain).foregroundColor(.mdText)
                if lines.isEmpty || !controller.isOnline {
                    Text(controller.text("无可用线路，请检查线路连接。", "No available line. Check your connection."))
                        .font(.footnote).foregroundColor(.mdMuted)
                }
                if !draft.number.isEmpty && dialTarget.error != .none {
                    Text(controller.text("请输入有效的电话号码", "Enter a valid phone number"))
                        .font(.footnote).foregroundColor(.mdDanger)
                }
                ModemDeckInlineError(message: recordingError)
                ModemDeckInlineError(message: callController.errorMessage)
            }
            .frame(maxWidth: 340)
            .padding(.horizontal, 20).padding(.bottom, 20)
            .frame(maxWidth: .infinity)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(Color.mdSurface)
        .onAppear { chooseDefaultLine() }
        .onChange(of: lines.map(\.id)) { _ in chooseDefaultLine() }
        .onChange(of: draft.number) { _ in
            validationAttempted = false
            if !draft.lineSelectionOverridden {
                draft.selectedLineID = controller.voiceDialLine(preferredID: matchedContact?.preferredLineId)?.id ?? ""
            }
        }
        .task {
            await contactsStore.load()
            await loadRecordingPreference()
        }
        .sheet(isPresented: $showingContacts) { contactPicker }
    }

    private var contactPicker: some View {
        NavigationStack {
            List {
                ForEach(contactsStore.contacts.filter { contact in
                    contactQuery.isEmpty || contact.displayName.localizedCaseInsensitiveContains(contactQuery) ||
                    contact.phones.contains { $0.displayNumber.localizedCaseInsensitiveContains(contactQuery) }
                }) { contact in
                    Section(contact.displayName) {
                        ForEach(Array(contact.phones.enumerated()), id: \.offset) { _, phone in
                            Button {
                                draft.number = phone.displayNumber
                                if !draft.lineSelectionOverridden {
                                    draft.selectedLineID = controller.voiceDialLine(preferredID: contact.preferredLineId)?.id ?? ""
                                }
                                showingContacts = false
                            } label: {
                                VStack(alignment: .leading, spacing: 4) {
                                    Text(phone.displayNumber).foregroundColor(.mdText)
                                    Text(phone.label).font(.footnote).foregroundColor(.mdMuted)
                                }
                                .frame(minHeight: 44)
                            }
                        }
                    }
                }
            }
            .searchable(text: $contactQuery, prompt: controller.text("搜索姓名或号码", "Search name or number"))
            .navigationTitle(controller.text("选择联系人", "Choose contact"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(controller.text("取消", "Cancel")) { showingContacts = false }
                }
            }
        }
    }

    private func chooseDefaultLine() {
        guard !lines.contains(where: { $0.id == draft.selectedLineID }) else { return }
        draft.selectedLineID = controller.voiceDialLine(preferredID: nil)?.id ?? ""
    }
    private func appendDigit(_ digit: String) {
        guard draft.number.count < 64 else { return }
        draft.number.append(digit)
        validationAttempted = false
        UIImpactFeedbackGenerator(style: .light).impactOccurred(intensity: 0.65)
        ModemDeckDTMFTonePlayer.shared.play(digit)
    }
    private func removeDigit() {
        guard !draft.number.isEmpty else { return }
        draft.number.removeLast()
        validationAttempted = false
        UISelectionFeedbackGenerator().selectionChanged()
    }
    private func loadRecordingPreference() async {
        guard !draft.recordingInitialized else { return }
        do {
            let settings = try await controller.api.recordingSettings()
            if !draft.recordingInitialized { draft.recording = settings.defaultEnabled }
            draft.recordingInitialized = true
            recordingError = ""
        } catch { recordingError = error.localizedDescription }
    }
    private func startCall() {
        validationAttempted = true
        guard canDial, let line = selectedLine else { return }
        let target = dialTarget
        let displayName = matchedContact?.displayName ?? target.original
        Task {
            await callController.start(lineID: line.id, number: target.original, displayName: displayName,
                                       recording: draft.recordingInitialized ? draft.recording : nil)
            if callController.call != nil && callController.errorMessage.isEmpty { draft.clear() }
        }
    }
}

struct ModemDeckCallsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @StateObject private var store: ModemDeckCallsStore
    @Environment(\.modemDeckUsesSplitWorkspace) private var usesSplitWorkspace
    @Environment(\.modemDeckNavigate) private var navigate
    @State private var query = ""
    @State private var lineFilter = ""
    @State private var favoriteOnly = false
    @State private var directionFilter = "all"
    @State private var selecting = false
    @State private var selectedIDs = Set<String>()
    @State private var selectedCallID: String?
    @State private var batchBusy = false
    @State private var confirmBatchDelete = false

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        _store = StateObject(wrappedValue: controller.callsStore)
    }

    private var filteredCalls: [ModemDeckCallRecord] {
        let normalized = query.trimmingCharacters(in: .whitespacesAndNewlines)
        let callsWithRecordings = Set(store.recordings.map { $0.call.id })
        return store.calls.filter { call in
            let matchesStatus = controller.callsFilter == "all" ||
                (controller.callsFilter == "missed" && call.missed) ||
                (controller.callsFilter == "recorded" && callsWithRecordings.contains(call.id))
            let matchesQuery = normalized.isEmpty ||
                displayName(for: call).localizedCaseInsensitiveContains(normalized) ||
                call.remoteNumber.localizedCaseInsensitiveContains(normalized)
            return matchesStatus && matchesQuery &&
                (directionFilter == "all" || call.direction == directionFilter) &&
                (lineFilter.isEmpty || call.lineId == lineFilter) &&
                (!favoriteOnly || call.favorite)
        }
    }

    private var recordedCallIDs: Set<String> {
        Set(store.recordings.map { $0.call.id })
    }

    private var selectedCall: ModemDeckCallRecord? {
        guard let id = selectedCallID else { return nil }
        return store.calls.first(where: { $0.id == id })
    }

    private var selectedCalls: [ModemDeckCallRecord] {
        filteredCalls.filter { selectedIDs.contains($0.id) }
    }

    private func contact(for call: ModemDeckCallRecord) -> ModemDeckContact? {
        store.contacts.modemDeckContact(id: call.contactId, number: call.remoteNumber)
    }

    private func displayName(for call: ModemDeckCallRecord) -> String {
        contact(for: call)?.displayName ?? call.displayName
    }

    var body: some View {
        Group {
            if usesSplitWorkspace {
                HStack(spacing: 0) {
                    callListColumn
                        .frame(width: ModemDeckLayout.padListWidth)
                    Rectangle().fill(Color.mdBorder).frame(width: 1)
                    callDetail
                }
            } else {
                callListColumn
            }
        }
        .background(Color.mdBackground)
        .navigationBarHidden(true)
        .overlay(alignment: .bottom) {
            ModemDeckInlineError(message: store.errorMessage)
                .padding(.horizontal, 16)
        }
        .task { await store.load() }
        .onChange(of: filteredCalls.map(\.id)) { visibleIDs in
            if let selectedCallID, !visibleIDs.contains(selectedCallID) { self.selectedCallID = nil }
            if selecting { selectedIDs.formIntersection(Set(visibleIDs)) }
        }
        .alert(
            controller.text("删除所选通话？", "Delete Selected Calls?"),
            isPresented: $confirmBatchDelete
        ) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {}
            Button(controller.text("删除", "Delete"), role: .destructive) {
                mutateSelectedCalls(.delete)
            }
        } message: {
            Text(controller.text(
                "将永久删除 \(selectedCalls.count) 条通话记录。",
                "This permanently deletes \(selectedCalls.count) call records."
            ))
        }
    }

    private var callListColumn: some View {
        VStack(spacing: 0) {
            ModemDeckCollectionHeader(controller: controller, title: controller.text("通话", "Calls"), query: $query) {}
            filterBar
            callList
        }
        .background(Color.mdBackground)
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if selecting { callBatchBar }
        }
    }

    @ViewBuilder
    private var callDetail: some View {
        if let call = selectedCall {
            ModemDeckCallDetailView(
                call: call,
                recordings: store.recordings.filter { $0.call.id == call.id },
                controller: controller,
                contact: contact(for: call),
                showsBackButton: false,
                onChanged: reloadCalls,
                onDeleted: handleDeletedCall
            )
            .id(call.id)
        } else {
            ModemDeckWorkspaceEmptyView(
                icon: "tray",
                title: controller.text("选择通话", "Select a call"),
                detail: controller.text("通话详情会显示在这里。", "Call details appear here.")
            )
        }
    }

    private var filterBar: some View {
        HStack(spacing: ModemDeckLayout.toolbarGap) {
            ModemDeckSegmentPicker(
                options: [
                    .init(id: "all", title: controller.text("全部", "All")),
                    .init(id: "missed", title: controller.text("未接", "Missed")),
                    .init(id: "recorded", title: controller.text("有录音", "Recorded"))
                ],
                selection: $controller.callsFilter, compact: true
            )
            ModemDeckCollectionFilters(controller: controller, line: $lineFilter, favoriteOnly: $favoriteOnly,
                extraCount: directionFilter == "all" ? 0 : 1, clearExtra: { directionFilter = "all" }) {
                Picker(controller.text("通话方向", "Direction"), selection: $directionFilter) {
                    Text(controller.text("所有方向", "All directions")).tag("all")
                    Text(controller.text("呼入", "Incoming")).tag("incoming")
                    Text(controller.text("呼出", "Outgoing")).tag("outgoing")
                }
            }
            ModemDeckCollectionMenu(controller: controller, selecting: $selecting, busy: batchBusy,
                selectTitle: controller.text("选择通话", "Select calls"), clearSelection: { selectedIDs.removeAll() }) {
                Button { navigate(.recordings) } label: {
                    Label(controller.text("录音管理", "Manage recordings"), systemImage: "waveform")
                }
            }
        }
        .modemDeckListFilterBar()
    }

    @ViewBuilder
    private var callList: some View {
        if store.loading && store.calls.isEmpty {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if !store.errorMessage.isEmpty && store.calls.isEmpty {
            ModemDeckLoadErrorState(
                controller: controller,
                detail: store.errorMessage
            ) {
                Task { await store.load() }
            }
        } else if filteredCalls.isEmpty {
            ScrollView {
                ModemDeckStateView(
                    icon: "phone",
                    title: store.calls.isEmpty
                        ? controller.text("还没有通话记录", "No Call History")
                        : controller.text("没有匹配结果", "No Matches"),
                    detail: store.calls.isEmpty
                        ? controller.text("拨打或接听电话后会显示在这里。", "Placed and received calls appear here.")
                        : controller.text("请调整搜索或筛选条件。", "Adjust the search or filters.")
                )
            }
            .refreshable { await store.load() }
        } else {
            List {
                ForEach(filteredCalls) { call in
                    callListRow(call)
                        .listRowInsets(EdgeInsets())
                        .listRowSeparator(.hidden)
                        .listRowBackground(Color.mdBackground)
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .environment(\.defaultMinListRowHeight, 0)
            .background(Color.mdBackground)
            .refreshable { await store.load() }
        }
    }

    private func callListRow(_ call: ModemDeckCallRecord) -> some View {
        ModemDeckListRow(
            controller: controller, selecting: selecting,
            selected: selecting ? selectedIDs.contains(call.id) : usesSplitWorkspace && selectedCallID == call.id,
            enabled: !batchBusy, unread: call.missed ? !call.read : nil, favorite: call.favorite,
            accessibilityID: "call-\(call.id)",
            deleteMessage: controller.text("将永久删除此通话记录及其录音。", "This permanently deletes the call record and its recordings."),
            open: {
                if selecting {
                    if !selectedIDs.insert(call.id).inserted { selectedIDs.remove(call.id) }
                } else if usesSplitWorkspace {
                    selectedCallID = call.id
                } else { navigate(.call(call.id)) }
            },
            beginSelection: { selecting = true; selectedIDs = [call.id] },
            toggleRead: call.missed ? { try await store.mutate(call.read ? .unread : .read, calls: [call]) } : nil,
            toggleFavorite: { try await store.mutate(call.favorite ? .unfavorite : .favorite, calls: [call]) },
            delete: { try await store.mutate(.delete, calls: [call]) }
        ) { actions in
            ModemDeckCallRecordRow(
                call: call, contact: contact(for: call), controller: controller,
                recorded: recordedCallIDs.contains(call.id), contextActions: actions
            )
        }
    }

    private var callBatchBar: some View {
        let selected = selectedCalls
        let missed = selected.filter(\.missed)
        let allMissedUnread = !missed.isEmpty && missed.allSatisfy { !$0.read }
        let allFavorite = !selected.isEmpty && selected.allSatisfy(\.favorite)

        return ModemDeckBatchActionBar(
            selectedCount: selected.count,
            totalCount: filteredCalls.count,
            busy: batchBusy,
            selectedText: controller.text("已选择 %d 项", "%d selected"),
            selectAllText: controller.text("全选", "Select All"),
            clearAllText: controller.text("清除", "Clear"),
            doneText: controller.text("完成", "Done"),
            selectAll: toggleAllCalls,
            done: endCallSelection
        ) {
            if selected.count == 1, let call = selected.first {
                ModemDeckBatchCopyMenu(controller: controller, items: [
                    .init(label: controller.text("复制联系人", "Copy Contact"), value: displayName(for: call)),
                    .init(label: controller.text("复制号码", "Copy Number"), value: call.remoteNumber)
                ])
            }
            if !missed.isEmpty {
                ModemDeckBatchActionButton(
                    title: allMissedUnread
                        ? controller.text("标为已读", "Mark Read")
                        : controller.text("标为未读", "Mark Unread"),
                    icon: allMissedUnread ? "phone.badge.checkmark" : "phone.badge.waveform",
                    disabled: batchBusy || !controller.isOnline
                ) { mutateSelectedCalls(allMissedUnread ? .read : .unread, calls: missed) }
            }
            ModemDeckBatchActionButton(
                title: allFavorite
                    ? controller.text("取消收藏", "Unfavorite")
                    : controller.text("收藏", "Favorite"),
                icon: allFavorite ? "star.slash" : "star",
                disabled: selected.isEmpty || batchBusy || !controller.isOnline
            ) { mutateSelectedCalls(allFavorite ? .unfavorite : .favorite) }
            ModemDeckBatchActionButton(
                title: controller.text("删除", "Delete"),
                icon: "trash",
                destructive: true,
                disabled: selected.isEmpty || batchBusy || !controller.isOnline
            ) { confirmBatchDelete = true }
        }
    }

    private func toggleAllCalls() {
        let visible = Set(filteredCalls.map(\.id))
        if !visible.isEmpty && visible.isSubset(of: selectedIDs) {
            selectedIDs.subtract(visible)
        } else {
            selectedIDs.formUnion(visible)
        }
    }

    private func endCallSelection() {
        selecting = false
        selectedIDs.removeAll()
    }

    private func mutateSelectedCalls(
        _ action: ModemDeckCallBatchAction,
        calls: [ModemDeckCallRecord]? = nil
    ) {
        let targets = calls ?? selectedCalls
        guard controller.isOnline, !targets.isEmpty, !batchBusy else { return }
        batchBusy = true
        Task {
            do {
                try await store.mutate(action, calls: targets)
                if action == .delete {
                    if let id = selectedCallID, selectedIDs.contains(id) { selectedCallID = nil }
                    endCallSelection()
                }
                await store.load()
                store.errorMessage = ""
            } catch {
                store.errorMessage = error.localizedDescription
            }
            batchBusy = false
        }
    }

    private func reloadCalls() {
        Task { await store.load() }
    }

    private func handleDeletedCall(_ id: String) {
        if selectedCallID == id { selectedCallID = nil }
        selectedIDs.remove(id)
        reloadCalls()
    }
}

struct ModemDeckCallRecordRow: View {
    let call: ModemDeckCallRecord
    var contact: ModemDeckContact? = nil
    @ObservedObject var controller: ModemDeckSessionController
    var recorded = false
    var contextActions: [ModemDeckContextAction] = []

    private var line: ModemDeckLine? {
        controller.bootstrap?.lineCatalog.first(where: { $0.id == call.lineId })
    }

    private var displayName: String {
        contact?.displayName ?? call.displayName
    }

    private var contactBound: Bool {
        contact != nil || !(call.contactId?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ?? true)
    }

    var body: some View {
        HStack(alignment: .center, spacing: ModemDeckLayout.listAvatarTextSpacing) {
            ModemDeckCommunicationAvatar(
                channel: .call,
                name: displayName,
                address: call.remoteNumber,
                avatarSource: contact?.avatar,
                contactBound: contactBound,
                size: ModemDeckLayout.listAvatarSize
            )
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) {
                    Text(displayName)
                        .font(.subheadline.weight(.semibold))
                        .foregroundColor(call.missed ? .mdDanger : .mdText)
                        .lineLimit(1)
                    Spacer(minLength: 6)
                    Text(controller.compactDateText(call.startedAt))
                        .font(.caption)
                        .foregroundColor(.mdFaint)
                }
                HStack(spacing: 7) {
                    if let line { ModemDeckLineTag(line: line) }
                    Image(systemName: directionIcon)
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundColor(call.missed ? .mdDanger : .mdMuted)
                    Text(call.remoteNumber)
                        .font(.footnote)
                        .foregroundColor(.mdMuted)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    if recorded {
                        Image(systemName: "waveform")
                            .font(.system(size: 12))
                            .foregroundColor(.mdBlue)
                    }
                    if call.favorite {
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
        .modemDeckCopyMenu([
            ModemDeckCopyItem(
                label: controller.text("复制联系人", "Copy Contact"),
                value: displayName
            ),
            ModemDeckCopyItem(
                label: controller.text("复制号码", "Copy Number"),
                value: call.remoteNumber
            )
        ], actions: contextActions)
    }

    private var directionIcon: String {
        if call.missed { return "phone.down.fill" }
        return call.direction == "incoming" ? "arrow.down.left" : "arrow.up.right"
    }
}

struct ModemDeckRecordingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    var showsBackButton = false
    @Environment(\.dismiss) private var dismiss
    @StateObject private var store: ModemDeckCallsStore
    @Environment(\.modemDeckUsesSplitWorkspace) private var usesSplitWorkspace
    @Environment(\.modemDeckNavigate) private var navigate
    @State private var query = ""
    @State private var lineFilter = ""
    @State private var favoriteOnly = false
    @State private var selecting = false
    @State private var selectedIDs = Set<String>()
    @State private var selectedRecordingID: String?
    @State private var batchBusy = false
    @State private var confirmBatchDelete = false

    init(controller: ModemDeckSessionController, showsBackButton: Bool = false) {
        self.controller = controller
        self.showsBackButton = showsBackButton
        _store = StateObject(wrappedValue: controller.callsStore)
    }

    private var filteredRecordings: [ModemDeckRecording] {
        let normalized = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return store.recordings.filter { recording in
            let matchesQuery = normalized.isEmpty ||
                displayName(for: recording).localizedCaseInsensitiveContains(normalized) ||
                recording.call.remoteNumber.localizedCaseInsensitiveContains(normalized)
            return matchesQuery &&
                (lineFilter.isEmpty || recording.call.lineId == lineFilter) &&
                (!favoriteOnly || recording.favorite)
        }
    }

    private var selectedRecording: ModemDeckRecording? {
        guard let id = selectedRecordingID else { return nil }
        return store.recordings.first(where: { $0.id == id })
    }

    private var selectedRecordings: [ModemDeckRecording] {
        filteredRecordings.filter { selectedIDs.contains($0.id) }
    }

    private func contact(for recording: ModemDeckRecording) -> ModemDeckContact? {
        store.contacts.modemDeckContact(
            id: recording.call.contactId,
            number: recording.call.remoteNumber
        )
    }

    private func displayName(for recording: ModemDeckRecording) -> String {
        contact(for: recording)?.displayName ?? recording.call.displayName
    }

    var body: some View {
        Group {
            if usesSplitWorkspace {
                HStack(spacing: 0) {
                    recordingListColumn
                        .frame(width: ModemDeckLayout.padListWidth)
                    Rectangle().fill(Color.mdBorder).frame(width: 1)
                    recordingDetail
                }
            } else {
                recordingListColumn
            }
        }
        .background(Color.mdBackground)
        .navigationBarHidden(true)
        .modemDeckInteractiveBack(showsBackButton)
        .overlay(alignment: .bottom) {
            ModemDeckInlineError(message: store.errorMessage)
                .padding(.horizontal, 16)
        }
        .task { await store.load() }
        .onChange(of: filteredRecordings.map(\.id)) { visibleIDs in
            guard selecting else { return }
            selectedIDs.formIntersection(Set(visibleIDs))
        }
        .alert(
            controller.text("删除所选录音？", "Delete Selected Recordings?"),
            isPresented: $confirmBatchDelete
        ) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {}
            Button(controller.text("删除", "Delete"), role: .destructive) {
                mutateSelectedRecordings(.delete)
            }
        } message: {
            Text(controller.text(
                "将永久删除 \(selectedRecordings.count) 条录音。",
                "This permanently deletes \(selectedRecordings.count) recordings."
            ))
        }
    }

    private var recordingListColumn: some View {
        VStack(spacing: 0) {
            ModemDeckCollectionHeader(controller: controller, title: controller.text("录音管理", "Recordings"), query: $query,
                backAction: showsBackButton ? { dismiss() } : nil,
                backAccessibilityText: controller.text("返回通话", "Back to Calls")) {}
            HStack(spacing: ModemDeckLayout.toolbarGap) {
                Text(controller.text("\(filteredRecordings.count) 条录音", "\(filteredRecordings.count) recordings"))
                    .font(.subheadline).foregroundColor(.mdMuted)
                Spacer(minLength: 0)
                ModemDeckCollectionFilters(controller: controller, line: $lineFilter, favoriteOnly: $favoriteOnly) {}
                ModemDeckCollectionMenu(controller: controller, selecting: $selecting, busy: batchBusy,
                    selectTitle: controller.text("选择录音", "Select recordings"), clearSelection: { selectedIDs.removeAll() }) {}
            }
            .modemDeckListFilterBar()

            recordingList
        }
        .background(Color.mdBackground)
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if selecting { recordingBatchBar }
        }
    }

    @ViewBuilder
    private var recordingDetail: some View {
        if let recording = selectedRecording {
            ModemDeckRecordingDetailView(
                recording: recording,
                controller: controller,
                contact: contact(for: recording),
                showsBackButton: false,
                onChanged: reloadRecordings,
                onDeleted: handleDeletedRecording
            )
            .id(recording.id)
        } else {
            ModemDeckWorkspaceEmptyView(
                icon: "tray",
                title: controller.text("选择录音", "Select a recording")
            )
        }
    }

    @ViewBuilder
    private var recordingList: some View {
        if store.loading && store.recordings.isEmpty {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if !store.errorMessage.isEmpty && store.recordings.isEmpty {
            ModemDeckLoadErrorState(
                controller: controller,
                detail: store.errorMessage
            ) {
                Task { await store.load() }
            }
        } else if filteredRecordings.isEmpty {
            ScrollView {
                ModemDeckStateView(
                    icon: "waveform",
                    title: store.recordings.isEmpty
                        ? controller.text("还没有录音", "No Recordings")
                        : controller.text("没有匹配结果", "No Matches"),
                    detail: store.recordings.isEmpty
                        ? controller.text("启用通话录音后会显示在这里。", "Recorded calls appear here.")
                        : controller.text("请调整搜索或筛选条件。", "Adjust the search or filters.")
                )
            }
            .refreshable { await store.load() }
        } else {
            List {
                ForEach(filteredRecordings) { recording in
                    recordingListRow(recording)
                        .listRowInsets(EdgeInsets())
                        .listRowSeparator(.hidden)
                        .listRowBackground(Color.mdBackground)
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .environment(\.defaultMinListRowHeight, 0)
            .background(Color.mdBackground)
            .refreshable { await store.load() }
        }
    }

    private func recordingListRow(_ recording: ModemDeckRecording) -> some View {
        ModemDeckListRow(
            controller: controller, selecting: selecting,
            selected: selecting ? selectedIDs.contains(recording.id) : usesSplitWorkspace && selectedRecordingID == recording.id,
            enabled: !batchBusy, favorite: recording.favorite,
            accessibilityID: "recording-\(recording.id)",
            deleteMessage: controller.text("将永久删除此录音。", "This permanently deletes this recording."),
            open: {
                if selecting {
                    if !selectedIDs.insert(recording.id).inserted { selectedIDs.remove(recording.id) }
                } else if usesSplitWorkspace {
                    selectedRecordingID = recording.id
                } else { navigate(.recording(recording.id)) }
            },
            beginSelection: { selecting = true; selectedIDs = [recording.id] },
            toggleFavorite: { try await store.mutate(recording.favorite ? .unfavorite : .favorite, recordings: [recording]) },
            delete: { try await store.mutate(.delete, recordings: [recording]) }
        ) { actions in
            ModemDeckRecordingRow(
                recording: recording, contact: contact(for: recording), controller: controller,
                contextActions: actions
            )
        }
    }

    private var recordingBatchBar: some View {
        let selected = selectedRecordings
        let allFavorite = !selected.isEmpty && selected.allSatisfy(\.favorite)

        return ModemDeckBatchActionBar(
            selectedCount: selected.count,
            totalCount: filteredRecordings.count,
            busy: batchBusy,
            selectedText: controller.text("已选择 %d 项", "%d selected"),
            selectAllText: controller.text("全选", "Select All"),
            clearAllText: controller.text("清除", "Clear"),
            doneText: controller.text("完成", "Done"),
            selectAll: toggleAllRecordings,
            done: endRecordingSelection
        ) {
            if selected.count == 1, let recording = selected.first {
                ModemDeckBatchCopyMenu(controller: controller, items: [
                    .init(label: controller.text("复制联系人", "Copy Contact"), value: displayName(for: recording)),
                    .init(label: controller.text("复制号码", "Copy Number"), value: recording.call.remoteNumber)
                ])
            }
            ModemDeckBatchActionButton(
                title: allFavorite
                    ? controller.text("取消收藏", "Unfavorite")
                    : controller.text("收藏", "Favorite"),
                icon: allFavorite ? "star.slash" : "star",
                disabled: selected.isEmpty || batchBusy || !controller.isOnline
            ) { mutateSelectedRecordings(allFavorite ? .unfavorite : .favorite) }
            ModemDeckBatchActionButton(
                title: controller.text("删除", "Delete"),
                icon: "trash",
                destructive: true,
                disabled: selected.isEmpty || batchBusy || !controller.isOnline
            ) { confirmBatchDelete = true }
        }
    }

    private func toggleAllRecordings() {
        let visible = Set(filteredRecordings.map(\.id))
        if !visible.isEmpty && visible.isSubset(of: selectedIDs) {
            selectedIDs.subtract(visible)
        } else {
            selectedIDs.formUnion(visible)
        }
    }

    private func endRecordingSelection() {
        selecting = false
        selectedIDs.removeAll()
    }

    private func mutateSelectedRecordings(_ action: ModemDeckRecordingBatchAction) {
        let recordings = selectedRecordings
        guard controller.isOnline, !recordings.isEmpty, !batchBusy else { return }
        batchBusy = true
        Task {
            do {
                try await store.mutate(action, recordings: recordings)
                if action == .delete {
                    if let id = selectedRecordingID, selectedIDs.contains(id) {
                        selectedRecordingID = nil
                    }
                    endRecordingSelection()
                }
                await store.load()
                store.errorMessage = ""
            } catch {
                store.errorMessage = error.localizedDescription
            }
            batchBusy = false
        }
    }

    private func reloadRecordings() {
        Task { await store.load() }
    }

    private func handleDeletedRecording(_ id: String) {
        if selectedRecordingID == id { selectedRecordingID = nil }
        selectedIDs.remove(id)
        reloadRecordings()
    }
}

struct ModemDeckRecordingRow: View {
    let recording: ModemDeckRecording
    var contact: ModemDeckContact? = nil
    @ObservedObject var controller: ModemDeckSessionController
    var contextActions: [ModemDeckContextAction] = []

    private var line: ModemDeckLine? {
        controller.bootstrap?.lineCatalog.first(where: { $0.id == recording.call.lineId })
    }

    private var displayName: String {
        contact?.displayName ?? recording.call.displayName
    }

    private var contactBound: Bool {
        contact != nil || !(recording.call.contactId?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ?? true)
    }

    var body: some View {
        HStack(alignment: .center, spacing: ModemDeckLayout.listAvatarTextSpacing) {
            ModemDeckCommunicationAvatar(
                channel: .recording,
                name: displayName,
                address: recording.call.remoteNumber,
                avatarSource: contact?.avatar,
                contactBound: contactBound,
                muted: !recording.playable,
                size: ModemDeckLayout.listAvatarSize
            )
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) {
                    Text(displayName)
                        .font(.subheadline.weight(.semibold))
                        .foregroundColor(.mdText)
                        .lineLimit(1)
                    Spacer(minLength: 6)
                    Text(controller.compactDateText(recording.segment.createdAt))
                        .font(.caption)
                        .foregroundColor(.mdFaint)
                }
                HStack(spacing: 7) {
                    if let line { ModemDeckLineTag(line: line) }
                    Image(systemName: "waveform")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundColor(recording.playable ? .mdBlue : .mdFaint)
                    Text(ModemDeckDateText.duration(recording.segment.durationMs / 1_000))
                        .font(.footnote)
                        .foregroundColor(.mdMuted)
                    Spacer(minLength: 0)
                    if recording.favorite {
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
        .modemDeckCopyMenu([
            ModemDeckCopyItem(
                label: controller.text("复制联系人", "Copy Contact"),
                value: displayName
            ),
            ModemDeckCopyItem(
                label: controller.text("复制号码", "Copy Number"),
                value: recording.call.remoteNumber
            )
        ], actions: contextActions)
    }
}

private struct ModemDeckDetailCard<Content: View>: View {
    let title: String
    let content: Content

    init(title: String, @ViewBuilder content: () -> Content) {
        self.title = title
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(title.uppercased())
                .font(.system(size: 11, weight: .bold))
                .tracking(0.6)
                .foregroundColor(.mdFaint)
                .padding(.horizontal, 4)
                .padding(.bottom, 7)
            VStack(spacing: 0) { content }
                .background(Color.mdSurface)
                .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .stroke(Color.mdBorder, lineWidth: 1)
                )
        }
    }
}

private struct ModemDeckDetailValueRow: View {
    let title: String
    let value: String
    var icon: String? = nil
    var valueColor = Color.mdMuted

    var body: some View {
        HStack(spacing: 11) {
            if let icon {
                Image(systemName: icon)
                    .font(.system(size: 15, weight: .medium))
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

private struct ModemDeckDetailDivider: View {
    var body: some View {
        Rectangle()
            .fill(Color.mdBorder)
            .frame(height: 1)
            .padding(.leading, 48)
    }
}

private struct ModemDeckDetailAction: View {
    let title: String
    let icon: String
    var prominent = false

    var body: some View {
        Label(title, systemImage: icon)
            .font(.system(size: 14, weight: .semibold))
            .foregroundColor(prominent ? .mdOnAccent : .mdAccent)
            .frame(maxWidth: .infinity, minHeight: 44)
            .background(prominent ? Color.mdAccent : Color.mdSurface)
            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .stroke(prominent ? Color.clear : Color.mdBorder, lineWidth: 1)
            )
    }
}

struct ModemDeckCallDetailView: View {
    let call: ModemDeckCallRecord
    let recordings: [ModemDeckRecording]
    @ObservedObject var controller: ModemDeckSessionController
    let contact: ModemDeckContact?
    let showsBackButton: Bool
    let onChanged: () -> Void
    let onDeleted: (String) -> Void
    @Environment(\.presentationMode) private var presentationMode
    @State private var favorite: Bool
    @State private var isRead: Bool
    @State private var mutationBusy = false
    @State private var mutationError = ""
    @State private var confirmDelete = false

    init(
        call: ModemDeckCallRecord,
        recordings: [ModemDeckRecording],
        controller: ModemDeckSessionController,
        contact: ModemDeckContact? = nil,
        showsBackButton: Bool = true,
        onChanged: @escaping () -> Void = {},
        onDeleted: @escaping (String) -> Void = { _ in }
    ) {
        self.call = call
        self.recordings = recordings
        self.controller = controller
        self.contact = contact
        self.showsBackButton = showsBackButton
        self.onChanged = onChanged
        self.onDeleted = onDeleted
        _favorite = State(initialValue: call.favorite)
        _isRead = State(initialValue: call.read)
    }

    private var line: ModemDeckLine? {
        controller.bootstrap?.lineCatalog.first(where: { $0.id == call.lineId })
    }

    private var dialLine: ModemDeckLine? {
        controller.voiceDialLine(preferredID: call.lineId)
    }

    private var displayName: String {
        contact?.displayName ?? call.displayName
    }

    private var contactBound: Bool {
        contact != nil || !(call.contactId?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ?? true)
    }

    var body: some View {
        VStack(spacing: 0) {
            ModemDeckIdentityHeader(
                title: displayName,
                subtitle: call.remoteNumber,
                avatarSource: contact?.avatar,
                channel: .call,
                contactBound: contactBound,
                showsBackButton: showsBackButton,
                backTitle: controller.text("返回通话", "Back to calls"),
                actions: callHeaderActions,
                copyItems: [
                    ModemDeckCopyItem(
                        label: controller.text("复制联系人", "Copy Contact"),
                        value: displayName
                    ),
                    ModemDeckCopyItem(
                        label: controller.text("复制号码", "Copy Number"),
                        value: call.remoteNumber
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

            ScrollView {
                VStack(spacing: 18) {
                    HStack(spacing: 10) {
                        Button { startCall() } label: {
                            ModemDeckDetailAction(
                                title: controller.text("呼叫", "Call"),
                                icon: "phone.fill",
                                prominent: true
                            )
                        }
                        .buttonStyle(.plain)
                        .disabled(dialLine == nil || controller.callController.busy || !controller.isOnline)

                        NavigationLink {
                            ModemDeckDirectMessageView(
                                number: call.remoteNumber,
                                displayName: displayName,
                                avatarSource: contact?.avatar,
                                contactBound: contactBound,
                                controller: controller,
                                preferredLineID: call.lineId
                            )
                        } label: {
                            ModemDeckDetailAction(
                                title: controller.text("消息", "Message"),
                                icon: "message.fill"
                            )
                        }
                        .buttonStyle(.plain)
                        .disabled(!controller.isOnline)
                    }

                    ModemDeckDetailCard(title: controller.text("通话详情", "Call Details")) {
                        ModemDeckDetailValueRow(
                            title: controller.text("方向", "Direction"),
                            value: directionText,
                            icon: directionIcon,
                            valueColor: call.missed ? .mdDanger : .mdMuted
                        )
                        ModemDeckDetailDivider()
                        ModemDeckDetailValueRow(
                            title: controller.text("时间", "Time"),
                            value: controller.dateText(call.startedAt),
                            icon: "clock"
                        )
                        ModemDeckDetailDivider()
                        ModemDeckDetailValueRow(
                            title: controller.text("时长", "Duration"),
                            value: ModemDeckDateText.duration(call.durationSeconds),
                            icon: "timer"
                        )
                        ModemDeckDetailDivider()
                        ModemDeckDetailValueRow(
                            title: controller.text("线路", "Line"),
                            value: line?.displayName ?? call.lineId,
                            icon: "simcard"
                        )
                        if let failure = call.failureCode, !failure.isEmpty {
                            ModemDeckDetailDivider()
                            ModemDeckDetailValueRow(
                                title: controller.text("失败原因", "Failure"),
                                value: failure,
                                icon: "exclamationmark.triangle",
                                valueColor: .mdDanger
                            )
                        }
                    }

                    if !recordings.isEmpty {
                        ModemDeckDetailCard(title: controller.text("录音", "Recordings")) {
                            ForEach(Array(recordings.enumerated()), id: \.element.id) { index, recording in
                                if index > 0 { ModemDeckDetailDivider() }
                                NavigationLink {
                                    ModemDeckRecordingDetailView(
                                        recording: recording,
                                        controller: controller,
                                        contact: contact,
                                        onChanged: onChanged,
                                        onDeleted: { _ in onChanged() }
                                    )
                                } label: {
                                    HStack(spacing: 12) {
                                        Image(systemName: recording.playable ? "waveform.circle.fill" : "waveform.circle")
                                            .font(.system(size: 22))
                                            .foregroundColor(recording.playable ? .mdBlue : .mdFaint)
                                            .frame(width: 28)
                                        VStack(alignment: .leading, spacing: 3) {
                                            Text(controller.text(
                                                "录音片段 \(recording.segment.segmentIndex)",
                                                "Segment \(recording.segment.segmentIndex)"
                                            ))
                                            .font(.system(size: 15, weight: .semibold))
                                            .foregroundColor(.mdText)
                                            Text(ModemDeckDateText.duration(recording.segment.durationMs / 1_000))
                                                .font(.system(size: 13))
                                                .foregroundColor(.mdMuted)
                                        }
                                        Spacer()
                                    }
                                    .padding(.horizontal, 14)
                                    .frame(minHeight: 58)
                                    .contentShape(Rectangle())
                                }
                                .buttonStyle(.plain)
                            }
                        }
                    }
                }
                .frame(maxWidth: 720)
                .padding(16)
                .frame(maxWidth: .infinity)
            }
            .background(Color.mdBackground)
        }
        .navigationBarHidden(true)
        .modemDeckInteractiveBack(showsBackButton)
        .onChange(of: call) { value in
            favorite = value.favorite
            isRead = value.read
        }
        .alert(
            controller.text("删除通话记录？", "Delete Call Record?"),
            isPresented: $confirmDelete
        ) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {}
            Button(controller.text("删除", "Delete"), role: .destructive) { deleteCall() }
        } message: {
            Text(controller.text("该通话记录将被永久删除。", "This call record will be permanently deleted."))
        }
    }

    private var callHeaderActions: [ModemDeckHeaderAction] {
        var items: [ModemDeckHeaderAction] = []
        if call.missed {
            items.append(ModemDeckHeaderAction(
                id: "read",
                icon: isRead ? "envelope.badge" : "envelope.open",
                title: isRead
                    ? controller.text("标为未读", "Mark unread")
                    : controller.text("标为已读", "Mark read"),
                disabled: mutationBusy || !controller.isOnline
            ) { mutateCall(isRead ? .unread : .read) })
        }
        items.append(ModemDeckHeaderAction(
            id: "favorite",
            icon: favorite ? "star.fill" : "star",
            title: favorite
                ? controller.text("取消收藏", "Unfavorite")
                : controller.text("收藏", "Favorite"),
            color: favorite ? .orange : .mdMuted,
            disabled: mutationBusy || !controller.isOnline
        ) { mutateCall(favorite ? .unfavorite : .favorite) })
        items.append(ModemDeckHeaderAction(
            id: "delete",
            icon: "trash",
            title: controller.text("删除通话", "Delete call"),
            color: .mdDanger,
            disabled: mutationBusy || !controller.isOnline
        ) { confirmDelete = true })
        return items
    }

    private var directionText: String {
        if call.missed { return controller.text("未接来电", "Missed") }
        return call.direction == "incoming"
            ? controller.text("呼入", "Incoming")
            : controller.text("呼出", "Outgoing")
    }

    private var directionIcon: String {
        if call.missed { return "phone.down.fill" }
        return call.direction == "incoming" ? "arrow.down.left" : "arrow.up.right"
    }

    private func startCall() {
        guard controller.isOnline, let dialLine else { return }
        Task {
            await controller.callController.start(
                lineID: dialLine.id,
                number: call.remoteNumber,
                displayName: displayName,
                recording: nil
            )
        }
    }

    private func mutateCall(_ action: ModemDeckCallBatchAction) {
        guard controller.isOnline, !mutationBusy else { return }
        mutationBusy = true
        mutationError = ""
        Task {
            do {
                try await controller.callsStore.mutate(action, calls: [call])
                switch action {
                case .read: isRead = true
                case .unread: isRead = false
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

    private func deleteCall() {
        guard controller.isOnline, !mutationBusy else { return }
        mutationBusy = true
        mutationError = ""
        Task {
            do {
                try await controller.callsStore.mutate(.delete, calls: [call])
                onDeleted(call.id)
                if showsBackButton { presentationMode.wrappedValue.dismiss() }
            } catch {
                mutationError = error.localizedDescription
                mutationBusy = false
            }
        }
    }
}

@MainActor
private final class ModemDeckRecordingPlayer: NSObject, ObservableObject, AVAudioPlayerDelegate {
    @Published private(set) var loading = false
    @Published private(set) var playing = false
    @Published var errorMessage = ""

    private let api: ModemDeckAPIClient
    private let callID: String
    private let segmentID: String
    private var player: AVAudioPlayer?

    init(api: ModemDeckAPIClient, callID: String, segmentID: String) {
        self.api = api
        self.callID = callID
        self.segmentID = segmentID
    }

    func toggle() {
        if let player {
            if player.isPlaying {
                player.pause()
                playing = false
            } else {
                player.play()
                playing = true
            }
            return
        }
        guard !loading else { return }
        loading = true
        errorMessage = ""
        Task {
            do {
                let payload = try await api.recordingAudio(callID: callID, segmentID: segmentID)
                let player = try AVAudioPlayer(data: payload)
                player.delegate = self
                player.prepareToPlay()
                self.player = player
                playing = player.play()
            } catch {
                ModemDeckDiagnostics.shared.record(.app, "operation_failed", error: error)
                errorMessage = error.localizedDescription
            }
            loading = false
        }
    }

    nonisolated func audioPlayerDidFinishPlaying(_ player: AVAudioPlayer, successfully flag: Bool) {
        Task { @MainActor [weak self] in
            self?.playing = false
        }
    }
}

struct ModemDeckRecordingDetailView: View {
    let recording: ModemDeckRecording
    @ObservedObject var controller: ModemDeckSessionController
    let contact: ModemDeckContact?
    let showsBackButton: Bool
    let onChanged: () -> Void
    let onDeleted: (String) -> Void
    @StateObject private var player: ModemDeckRecordingPlayer
    @Environment(\.presentationMode) private var presentationMode
    @State private var favorite: Bool
    @State private var mutationBusy = false
    @State private var mutationError = ""
    @State private var confirmDelete = false

    init(
        recording: ModemDeckRecording,
        controller: ModemDeckSessionController,
        contact: ModemDeckContact? = nil,
        showsBackButton: Bool = true,
        onChanged: @escaping () -> Void = {},
        onDeleted: @escaping (String) -> Void = { _ in }
    ) {
        self.recording = recording
        self.controller = controller
        self.contact = contact
        self.showsBackButton = showsBackButton
        self.onChanged = onChanged
        self.onDeleted = onDeleted
        _player = StateObject(wrappedValue: ModemDeckRecordingPlayer(
            api: controller.api,
            callID: recording.call.id,
            segmentID: recording.segment.id
        ))
        _favorite = State(initialValue: recording.favorite)
    }

    private var line: ModemDeckLine? {
        controller.bootstrap?.lineCatalog.first(where: { $0.id == recording.call.lineId })
    }

    private var dialLine: ModemDeckLine? {
        controller.voiceDialLine(preferredID: recording.call.lineId)
    }

    private var displayName: String {
        contact?.displayName ?? recording.call.displayName
    }

    private var contactBound: Bool {
        contact != nil || !(recording.call.contactId?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ?? true)
    }

    var body: some View {
        VStack(spacing: 0) {
            ModemDeckIdentityHeader(
                title: displayName,
                subtitle: recording.call.remoteNumber,
                avatarSource: contact?.avatar,
                channel: .recording,
                contactBound: contactBound,
                showsBackButton: showsBackButton,
                backTitle: controller.text("返回录音", "Back to recordings"),
                actions: recordingHeaderActions,
                copyItems: [
                    ModemDeckCopyItem(
                        label: controller.text("复制联系人", "Copy Contact"),
                        value: displayName
                    ),
                    ModemDeckCopyItem(
                        label: controller.text("复制号码", "Copy Number"),
                        value: recording.call.remoteNumber
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

            ScrollView {
                VStack(spacing: 18) {
                    ModemDeckDetailCard(title: controller.text("播放", "Playback")) {
                        Button { player.toggle() } label: {
                            HStack(spacing: 13) {
                                ZStack {
                                    Circle()
                                        .fill(recording.playable ? Color.mdAccent : Color.mdSurfaceHover)
                                        .frame(width: 50, height: 50)
                                    if player.loading {
                                        ProgressView().tint(.mdOnAccent)
                                    } else {
                                        Image(systemName: player.playing ? "pause.fill" : "play.fill")
                                            .font(.system(size: 19, weight: .bold))
                                            .foregroundColor(recording.playable ? .mdOnAccent : .mdFaint)
                                            .offset(x: player.playing ? 0 : 1)
                                    }
                                }
                                VStack(alignment: .leading, spacing: 3) {
                                    Text(player.playing
                                         ? controller.text("暂停", "Pause")
                                         : controller.text("播放录音", "Play Recording"))
                                        .font(.system(size: 16, weight: .semibold))
                                        .foregroundColor(.mdText)
                                    Text(ModemDeckDateText.duration(recording.segment.durationMs / 1_000))
                                        .font(.system(size: 13))
                                        .foregroundColor(.mdMuted)
                                }
                                Spacer()
                                Image(systemName: "waveform")
                                    .font(.system(size: 23))
                                    .foregroundColor(recording.playable ? .mdBlue : .mdFaint)
                            }
                            .padding(.horizontal, 14)
                            .frame(minHeight: 74)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .disabled(!recording.playable || player.loading)
                    }

                    if !player.errorMessage.isEmpty {
                        ModemDeckInlineError(message: player.errorMessage)
                    }

                    HStack(spacing: 10) {
                        Button { startCall() } label: {
                            ModemDeckDetailAction(
                                title: controller.text("呼叫", "Call"),
                                icon: "phone.fill",
                                prominent: true
                            )
                        }
                        .buttonStyle(.plain)
                        .disabled(dialLine == nil || controller.callController.busy || !controller.isOnline)

                        NavigationLink {
                            ModemDeckDirectMessageView(
                                number: recording.call.remoteNumber,
                                displayName: displayName,
                                avatarSource: contact?.avatar,
                                contactBound: contactBound,
                                controller: controller,
                                preferredLineID: recording.call.lineId
                            )
                        } label: {
                            ModemDeckDetailAction(
                                title: controller.text("消息", "Message"),
                                icon: "message.fill"
                            )
                        }
                        .buttonStyle(.plain)
                        .disabled(!controller.isOnline)
                    }

                    ModemDeckDetailCard(title: controller.text("录音详情", "Recording Details")) {
                        ModemDeckDetailValueRow(
                            title: controller.text("录制时间", "Recorded"),
                            value: controller.dateText(recording.segment.createdAt),
                            icon: "clock"
                        )
                        ModemDeckDetailDivider()
                        ModemDeckDetailValueRow(
                            title: controller.text("时长", "Duration"),
                            value: ModemDeckDateText.duration(recording.segment.durationMs / 1_000),
                            icon: "timer"
                        )
                        ModemDeckDetailDivider()
                        ModemDeckDetailValueRow(
                            title: controller.text("文件大小", "File Size"),
                            value: ByteCountFormatter.string(
                                fromByteCount: Int64(recording.segment.sizeBytes),
                                countStyle: .file
                            ),
                            icon: "doc"
                        )
                        ModemDeckDetailDivider()
                        ModemDeckDetailValueRow(
                            title: controller.text("线路", "Line"),
                            value: line?.displayName ?? recording.call.lineId,
                            icon: "simcard"
                        )
                        ModemDeckDetailDivider()
                        ModemDeckDetailValueRow(
                            title: controller.text("状态", "Status"),
                            value: recording.playable
                                ? controller.text("可播放", "Playable")
                                : recording.segment.status,
                            icon: recording.playable ? "checkmark.circle" : "exclamationmark.circle",
                            valueColor: recording.playable ? .mdAccent : .mdDanger
                        )
                    }
                }
                .frame(maxWidth: 720)
                .padding(16)
                .frame(maxWidth: .infinity)
            }
            .background(Color.mdBackground)
        }
        .navigationBarHidden(true)
        .modemDeckInteractiveBack(showsBackButton)
        .onChange(of: recording) { favorite = $0.favorite }
        .alert(
            controller.text("删除录音？", "Delete Recording?"),
            isPresented: $confirmDelete
        ) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {}
            Button(controller.text("删除", "Delete"), role: .destructive) { deleteRecording() }
        } message: {
            Text(controller.text("该录音文件将被永久删除。", "This recording will be permanently deleted."))
        }
    }

    private var recordingHeaderActions: [ModemDeckHeaderAction] {
        [
            ModemDeckHeaderAction(
                id: "favorite",
                icon: favorite ? "star.fill" : "star",
                title: favorite
                    ? controller.text("取消收藏", "Unfavorite")
                    : controller.text("收藏", "Favorite"),
                color: favorite ? .orange : .mdMuted,
                disabled: mutationBusy || !controller.isOnline
            ) { toggleFavorite() },
            ModemDeckHeaderAction(
                id: "delete",
                icon: "trash",
                title: controller.text("删除录音", "Delete recording"),
                color: .mdDanger,
                disabled: mutationBusy || !controller.isOnline
            ) { confirmDelete = true }
        ]
    }

    private func startCall() {
        guard controller.isOnline, let dialLine else { return }
        Task {
            await controller.callController.start(
                lineID: dialLine.id,
                number: recording.call.remoteNumber,
                displayName: displayName,
                recording: nil
            )
        }
    }

    private func toggleFavorite() {
        guard controller.isOnline, !mutationBusy else { return }
        mutationBusy = true
        mutationError = ""
        Task {
            do {
                try await controller.callsStore.mutate(
                    favorite ? .unfavorite : .favorite,
                    recordings: [recording]
                )
                favorite.toggle()
                onChanged()
            } catch {
                mutationError = error.localizedDescription
            }
            mutationBusy = false
        }
    }

    private func deleteRecording() {
        guard controller.isOnline, !mutationBusy else { return }
        mutationBusy = true
        mutationError = ""
        Task {
            do {
                try await controller.callsStore.mutate(.delete, recordings: [recording])
                onDeleted(recording.id)
                if showsBackButton { presentationMode.wrappedValue.dismiss() }
            } catch {
                mutationError = error.localizedDescription
                mutationBusy = false
            }
        }
    }
}

struct ModemDeckMiniCallBar: View {
    let call: ModemDeckPresentedCall
    @ObservedObject var controller: ModemDeckSessionController
    @ObservedObject var callController: ModemDeckCallController
    let restore: () -> Void

    private var canEnd: Bool {
        if call.testCall || controller.bootstrap == nil { return true }
        let line = controller.bootstrap?.lines.first { $0.id == call.lineID }
        return call.state == "ringing" && call.direction == "incoming"
            ? line?.capabilities?.rejectCall == true : line?.capabilities?.hangupCall == true
    }
    var body: some View {
        HStack(spacing: 4) {
            Button(action: restore) {
                HStack(spacing: 12) {
                    Image(systemName: "phone.fill")
                    VStack(alignment: .leading, spacing: 3) {
                        Text(call.displayName.isEmpty ? call.remoteNumber : call.displayName)
                            .font(.subheadline.weight(.semibold)).lineLimit(1)
                        if let value = call.activeAt, let date = ModemDeckDateText.date(value) {
                            Text(date, style: .timer).font(.caption).monospacedDigit()
                        } else {
                            Text(controller.text("通话进行中", "Call in progress")).font(.caption)
                        }
                    }
                    Spacer(minLength: 4)
                }
                .padding(.horizontal, 14).frame(minHeight: 56)
                .contentShape(Rectangle())
            }
            .accessibilityLabel(controller.text("恢复通话", "Return to call"))
            .accessibilityValue(call.displayName.isEmpty ? call.remoteNumber : call.displayName)
            .accessibilityIdentifier("call-restore")
            Button { Task { await callController.end() } } label: {
                Image(systemName: "phone.down.fill")
                    .foregroundColor(.white)
                    .frame(width: 44, height: 44)
                    .background(Color(red: 0.78, green: 0.18, blue: 0.24))
                    .clipShape(Circle())
            }
            .disabled(!canEnd || callController.ending)
            .accessibilityLabel(controller.text("挂断", "End call"))
            .padding(.trailing, 8)
        }
        .buttonStyle(.plain).foregroundColor(.mdOnAccent)
        .background(Color.mdAccent)
        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
    }
}

struct ModemDeckActiveCallView: View {
    let call: ModemDeckPresentedCall
    @ObservedObject var controller: ModemDeckSessionController
    @ObservedObject var callController: ModemDeckCallController
    @ObservedObject private var audioRoute = ModemDeckAudioRoute.shared

    var collapse: () -> Void = {}
    @GestureState private var collapseOffset: CGFloat = 0

    @State private var showingKeypad = false
    @State private var dtmfDigits = ""
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var isRinging: Bool { call.state == "ringing" }
    private var isActive: Bool { call.state == "active" }

    private var callLine: ModemDeckLine? {
        guard let bootstrap = controller.bootstrap else { return nil }
        return bootstrap.lines.first(where: { $0.id == call.lineID }) ??
            (call.lineID.isEmpty && bootstrap.lines.count == 1 ? bootstrap.lines.first : nil)
    }

    private var canAnswer: Bool {
        if call.testCall { return true }
        guard let bootstrap = controller.bootstrap else { return true }
        return bootstrap.capabilities.webrtcAudio &&
            callLine?.capabilities?.answerCall == true &&
            callLine?.capabilities?.media == true
    }

    private var canReject: Bool {
        call.testCall || controller.bootstrap == nil || callLine?.capabilities?.rejectCall == true
    }

    private var canHangup: Bool {
        call.testCall || controller.bootstrap == nil || callLine?.capabilities?.hangupCall == true
    }

    private var canSendDTMF: Bool {
        !call.testCall && callLine?.capabilities?.sendDtmf != false
    }

    var body: some View {
        GeometryReader { geometry in
            let presentsAsPadPanel = ModemDeckLayout.isPad
            let panelWidth = min(420, max(0, geometry.size.width - 24))
            let panelHeight = min(720, max(0, geometry.size.height - 24))

            ZStack {
                if presentsAsPadPanel {
                    Color.black.opacity(0.38)
                        .ignoresSafeArea()

                    callStage
                        .frame(width: panelWidth, height: panelHeight)
                        .background(callBackground)
                        .clipShape(RoundedRectangle(cornerRadius: 28, style: .continuous))
                        .overlay(
                            RoundedRectangle(cornerRadius: 28, style: .continuous)
                                .stroke(Color.mdBorder, lineWidth: 0.5)
                        )
                        .shadow(color: Color.black.opacity(0.34), radius: 28, y: 12)
                } else {
                    callStage
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        .background(callBackground.ignoresSafeArea())
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .onAppear {
            #if DEBUG
            if ProcessInfo.processInfo.environment["MODEMDECK_UAT_CALL_KEYPAD"] == "1",
               isActive,
               canSendDTMF {
                showingKeypad = true
            }
            #endif
        }
        .onChange(of: canSendDTMF) { available in
            if !available { showingKeypad = false }
        }
    }

    private var callBackground: some View { Color.mdSurface }

    private var collapseHeader: some View {
        HStack {
            Text(controller.text("通话", "Call")).font(.headline)
            Spacer()
            Button(action: collapse) {
                Image(systemName: "chevron.down").frame(width: 44, height: 44).contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(controller.text("收起通话", "Collapse call"))
            .accessibilityIdentifier("call-collapse")
        }
        .foregroundColor(.mdText)
        .padding(.top, 14)
        .overlay(alignment: .top) {
            Capsule().fill(Color.mdBorder).frame(width: 34, height: 4).padding(.top, 4)
        }
        .contentShape(Rectangle())
        .gesture(DragGesture(minimumDistance: 8)
            .updating($collapseOffset) { value, offset, _ in
                if abs(value.translation.height) > abs(value.translation.width) {
                    offset = max(0, value.translation.height)
                }
            }
            .onEnded { value in
                guard value.translation.height > abs(value.translation.width) else { return }
                if value.translation.height > 100 || value.predictedEndTranslation.height > 220 { collapse() }
            })
    }

    private var isIncomingRinging: Bool { isRinging && call.direction == "incoming" }

    private var callStage: some View {
        VStack(spacing: 0) {
            collapseHeader
            GeometryReader { available in
                ScrollView {
                    VStack(spacing: 24) {
                        callIdentity
                        if !isIncomingRinging {
                            if call.testAudio && isActive { testAudioFeedback }
                            auxiliaryControls
                            if showingKeypad && isActive && canSendDTMF {
                                ModemDeckInCallKeypad(pressedDigits: dtmfDigits, action: sendDTMF)
                            }
                        }
                        ModemDeckInlineError(message: callController.errorMessage)
                        ModemDeckInlineError(message: audioRoute.errorMessage)
                    }
                    .foregroundColor(.mdText)
                    .padding(.vertical, 16)
                    .frame(maxWidth: .infinity, minHeight: available.size.height)
                }
            }
            callFooter
                .padding(.top, 20)
                .padding(.bottom, 24)
        }
        .frame(maxWidth: 640)
        .padding(.horizontal, 24)
        .offset(y: collapseOffset)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.18), value: collapseOffset == 0)
        .accessibilityAction(.escape, collapse)
    }

    private var callIdentity: some View {
        VStack(spacing: 10) {
            if !showingKeypad {
                Image(systemName: call.testCall ? "checkmark.shield.fill" : "person.crop.circle.fill")
                    .font(.system(size: 76))
                    .accessibilityHidden(true)
            }
            Text(call.displayName.isEmpty ? call.remoteNumber : call.displayName)
                .font(showingKeypad ? .title3.weight(.semibold) : .largeTitle.weight(.semibold))
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            if !showingKeypad, call.displayName != call.remoteNumber, !call.remoteNumber.isEmpty {
                Text(call.remoteNumber).font(.title3).foregroundColor(.mdMuted)
                    .multilineTextAlignment(.center)
            }
            Text(stateText).font(.subheadline.weight(.medium)).foregroundColor(.mdMuted)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var auxiliaryControls: some View {
        LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 20) {
            if isActive {
                ModemDeckCallControl(
                    title: call.muted ? controller.text("取消静音", "Unmute") : controller.text("静音", "Mute"),
                    icon: call.muted ? "mic.slash.fill" : "mic.fill",
                    selected: call.muted,
                    disabled: callController.muteBusy
                ) {
                    Task { await callController.setMuted(!call.muted) }
                }
                .accessibilityIdentifier("call-mute")
                audioRouteControl
            }
            if !call.testCall {
                ModemDeckCallControl(
                    title: recordingTitle,
                    icon: callController.recordingEnabled ? "stop.fill" : "circle.fill",
                    selected: callController.recordingEnabled,
                    selectedColor: .red,
                    disabled: !callController.recordingReady || callController.recordingBusy || callController.busy
                ) {
                    Task { await callController.toggleRecording() }
                }
                .accessibilityIdentifier("call-recording")
                if isActive {
                    ModemDeckCallControl(
                        title: showingKeypad ? controller.text("隐藏键盘", "Hide Keypad") : controller.text("键盘", "Keypad"),
                        icon: "circle.grid.3x3.fill",
                        selected: showingKeypad,
                        disabled: !canSendDTMF
                    ) {
                        if reduceMotion { showingKeypad.toggle() }
                        else { withAnimation(.easeOut(duration: 0.18)) { showingKeypad.toggle() } }
                    }
                    .accessibilityIdentifier("call-keypad")
                }
            }
        }
        .frame(maxWidth: 280)
    }

    private var callFooter: some View {
        // The incoming targets sit outside the centered end-call target, so a
        // second tap at Answer's position cannot immediately end the call.
        HStack(alignment: .top, spacing: 84) {
            if isIncomingRinging {
                ModemDeckCallPrimaryAction(
                    title: controller.text("拒绝", "Decline"), icon: "phone.down.fill", color: .red,
                    disabled: callController.ending || !canReject
                ) { Task { await callController.end() } }
                .accessibilityIdentifier("call-decline")
                ModemDeckCallPrimaryAction(
                    title: controller.text("接听", "Answer"), icon: "phone.fill", color: .green,
                    disabled: callController.busy || callController.ending || !canAnswer
                ) { Task { await callController.answer() } }
                .accessibilityIdentifier("call-answer")
            } else {
                ModemDeckCallPrimaryAction(
                    title: controller.text("挂断", "End"), icon: "phone.down.fill", color: .red,
                    disabled: callController.ending || !canHangup
                ) { Task { await callController.end() } }
                .accessibilityIdentifier("call-end")
            }
        }
        .frame(maxWidth: .infinity, minHeight: 98, alignment: .top)
    }

    private func sendDTMF(_ digit: String) {
        guard callController.enqueueDTMF(digit) else { return }
        dtmfDigits = String((dtmfDigits + digit).suffix(64))
        UIImpactFeedbackGenerator(style: .light).impactOccurred(intensity: 0.65)
        ModemDeckDTMFTonePlayer.shared.play(digit)
    }

    private var recordingTitle: String {
        if callController.recordingBusy {
            return controller.text("正在更新", "Updating")
        }
        switch callController.recordingStatus {
        case "recording":
            return controller.text("录音中", "Recording")
        case "pending":
            return controller.text("待录音", "Record On")
        case "failed":
            return controller.text("录音失败", "Record Failed")
        default:
            return controller.text("录音", "Record")
        }
    }

    private var stateText: String {
        if call.mediaState == "reconnecting" { return controller.text("正在恢复音频连接…", "Reconnecting audio…") }
        if call.mediaState == "waiting_for_audio" { return controller.text("等待系统启用音频…", "Waiting for system audio…") }
        if call.testCall {
            if call.state == "connecting" { return controller.text("正在连接测试音频…", "Connecting test audio…") }
            if isActive && call.testAudio {
                switch call.testPhase {
                case "guide": return controller.text("请听语音提示", "Listen to the instructions")
                case "tone": return controller.text("音频已连接 · 正在播放提示音", "Audio connected · Playing test tone")
                case "speak": return controller.text("请说几句话", "Say a few words") + countdown
                case "playback_prompt": return controller.text("准备回放你的声音", "Preparing your playback")
                case "playback": return controller.text("正在回放你的声音", "Playing back your voice") + countdown
                case "pause": return controller.text("即将再次测试", "The test will repeat shortly")
                default: return controller.text("测试音频已连接", "Test audio connected")
                }
            }
            return isActive
                ? controller.text("测试通话已接通", "Test call connected")
                : controller.text("ModemDeck 测试通话", "ModemDeck Test Call")
        }
        switch call.state {
        case "ringing": return controller.text("来电", "Incoming Call")
        case "connecting": return controller.text("正在连接…", "Connecting…")
        case "active": return controller.text("通话中", "Connected")
        default: return call.state
        }
    }

    private var countdown: String {
        call.testRemainingMS > 0 ? " · \((call.testRemainingMS + 999) / 1000)s" : ""
    }

    @ViewBuilder private var audioRouteControl: some View {
        if audioRoute.externalInputs.isEmpty {
            ModemDeckCallControl(title: controller.text("扬声器", "Speaker"), icon: "speaker.wave.2.fill",
                selected: audioRoute.output == "speaker", disabled: !audioRoute.active || !audioRoute.hasReceiver) {
                    audioRoute.selectSpeaker(audioRoute.output != "speaker")
                }
                .accessibilityIdentifier("call-speaker")
                .accessibilityValue(audioRoute.output == "speaker" ? controller.text("已开启", "On") : controller.text("已关闭", "Off"))
        } else {
            Menu {
                Button(controller.text("扬声器", "Speaker")) { audioRoute.selectSpeaker(true) }
                if audioRoute.hasReceiver {
                    Button(controller.text("iPhone 听筒", "iPhone receiver")) { audioRoute.selectSpeaker(false) }
                }
                ForEach(audioRoute.externalInputs, id: \.uid) { input in
                    Button(input.portName) { audioRoute.selectInput(input) }
                }
            } label: {
                VStack(spacing: 8) {
                    Image(systemName: audioRoute.output == "speaker" ? "speaker.wave.2.fill" : "headphones")
                        .font(.title2).frame(width: 62, height: 62)
                        .background(Color.mdSurfaceHover).clipShape(Circle())
                    Text(controller.text("音频", "Audio")).font(.caption)
                }.foregroundColor(.mdText)
            }
            .disabled(!audioRoute.active)
            .accessibilityIdentifier("call-audio-route")
        }
    }

    private var testAudioFeedback: some View {
        VStack(spacing: 8) {
            Text(controller.text("麦克风", "Microphone"))
                .font(.caption).foregroundColor(.mdMuted)
            ProgressView(value: Double(max(0, min(60, (call.microphoneDBFS ?? -96) + 60))) / 60)
                .tint(.mdAccent)
                .animation(reduceMotion ? nil : .linear(duration: 0.1), value: call.microphoneDBFS)
                .accessibilityLabel(controller.text("收音电平", "Microphone level"))
            Text(testAudioHint).font(.caption).foregroundColor(.mdMuted)
                .multilineTextAlignment(.center).fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: 250)
        .accessibilityIdentifier("call-test-audio-feedback")
    }

    private var testAudioHint: String {
        if call.muted { return controller.text("麦克风已静音", "Microphone is muted") }
        if ["playback", "playback_prompt", "pause"].contains(call.testPhase) {
            if call.receivedPackets == 0 {
                if call.localCapturedFrames == 0 { return controller.text("等待麦克风开始收音", "Waiting for microphone capture") }
                if call.localSentPackets == 0 { return controller.text("正在发送麦克风音频…", "Sending microphone audio…") }
                return controller.text("等待服务端确认收音…", "Waiting for the server audio report…")
            }
            if call.capturedPeakDBFS <= -80 { return controller.text("未检测到声音，请检查麦克风", "No sound detected. Check your microphone") }
            if call.capturedPeakDBFS < -40 { return controller.text("收音偏小，请靠近麦克风再试", "Audio is quiet. Move closer to the microphone") }
            return controller.text("请确认能否听清自己的回放", "Check whether you can hear your voice clearly")
        }
        return controller.text("提示音结束后说话，随后会回放", "Speak after the beep, then listen to the playback")
    }
}

private struct ModemDeckCallControl: View {
    let title: String
    let icon: String
    var selected = false
    var selectedColor = Color.modemDeckTint
    var disabled = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: 8) {
                Image(systemName: icon)
                    .font(.title2)
                    .foregroundColor(.mdText)
                    .frame(width: 62, height: 62)
                    .background(selected ? selectedColor.opacity(0.18) : Color.mdSurfaceHover)
                    .clipShape(Circle())
                Text(title)
                    .font(.caption)
                    .foregroundColor(.mdText)
            }
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .opacity(disabled ? 0.5 : 1)
    }
}

private struct ModemDeckCallFooterStyle: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.92 : 1)
            .opacity(configuration.isPressed ? 0.82 : 1)
            .animation(
                reduceMotion ? nil : .easeOut(duration: 0.08),
                value: configuration.isPressed
            )
    }
}

private struct ModemDeckCallPrimaryAction: View {
    let title: String
    let icon: String
    let color: Color
    var disabled = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: 8) {
                Image(systemName: icon)
                    .font(.system(size: 23, weight: .bold))
                    .foregroundColor(.white)
                    .frame(width: 62, height: 62)
                    .background(color)
                    .clipShape(Circle())
                Text(title).font(.caption.weight(.medium)).foregroundColor(.mdMuted)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(width: 76)
            .frame(minHeight: 88, alignment: .top)
            .contentShape(Rectangle())
        }
        .buttonStyle(ModemDeckCallFooterStyle())
        .disabled(disabled)
        .opacity(disabled ? 0.5 : 1)
    }
}

private struct ModemDeckInCallKeypad: View {
    let pressedDigits: String
    let action: (String) -> Void
    private let keys = [
        ("1", ""), ("2", "ABC"), ("3", "DEF"),
        ("4", "GHI"), ("5", "JKL"), ("6", "MNO"),
        ("7", "PQRS"), ("8", "TUV"), ("9", "WXYZ"),
        ("*", ""), ("0", "+"), ("#", "")
    ]

    var body: some View {
        VStack(spacing: 10) {
            Text(pressedDigits)
                .font(.system(size: 24, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .foregroundColor(.mdText)
                .lineLimit(1)
                .frame(width: 238, height: 34)
                .accessibilityLabel("Pressed keys")

            LazyVGrid(
                columns: Array(repeating: GridItem(.fixed(62), spacing: 26), count: 3),
                spacing: 9
            ) {
                ForEach(Array(keys.enumerated()), id: \.offset) { _, key in
                    Button {
                        action(key.0)
                    } label: {
                        VStack(spacing: key.0 == "0" ? 1 : 3) {
                            Text(key.0)
                                .font(.system(size: 25, weight: .medium, design: .rounded))
                            if !key.1.isEmpty {
                                Text(key.1)
                                    .font(.system(size: key.0 == "0" ? 14 : 9, weight: .bold))
                                    .tracking(key.0 == "0" ? 0 : 1.1)
                            }
                        }
                        .foregroundColor(.mdText)
                    }
                    .buttonStyle(ModemDeckInCallKeyStyle())
                    .accessibilityLabel(key.1.isEmpty ? key.0 : "\(key.0) \(key.1)")
                }
            }
            .frame(width: 238)
        }
    }
}

private struct ModemDeckInCallKeyStyle: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .frame(width: 62, height: 62)
            .background(
                configuration.isPressed
                    ? Color.mdSelected
                    : Color.mdSurfaceHover
            )
            .clipShape(Circle())
            .scaleEffect(configuration.isPressed ? 0.9 : 1)
            .animation(
                reduceMotion ? nil : .easeOut(duration: 0.08),
                value: configuration.isPressed
            )
    }
}
