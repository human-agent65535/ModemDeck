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
        // During a call CallKit owns the voice-processing session; a second
        // engine there can disturb call audio, so keys keep only haptics.
        guard !ModemDeckCallAudioSession.callAudioActive,
              let pair = frequencies[digit] else { return }
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

/// One round key shared by the dialer and the in-call DTMF keypad.
private struct ModemDeckDialKeyStyle: ButtonStyle {
    let size: CGFloat
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .frame(width: size, height: size)
            .background(Circle().fill(configuration.isPressed ? Color.mdSelected : Color.mdSurfaceHover))
            .contentShape(Circle())
            .scaleEffect(configuration.isPressed && !reduceMotion ? 0.94 : 1)
            .animation(
                reduceMotion ? nil : .easeOut(duration: 0.08),
                value: configuration.isPressed
            )
            .hoverEffect(.highlight)
    }
}

private struct ModemDeckDialKeyButton: View {
    let digit: String
    let letters: String
    var size: CGFloat = 76
    /// The dialer accepts "+" from a long press on 0; DTMF has no "+" tone.
    var allowsPlus = true
    var plusHint = "Long press to enter plus"
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
            VStack(spacing: 1) {
                Text(digit)
                    .font(.system(size: size * 0.45, weight: .regular, design: .rounded))
                    .lineLimit(1)
                Text(letters.isEmpty ? " " : letters)
                    .font(.caption2.weight(.semibold))
                    .tracking(digit == "0" ? 0 : 1.1)
                    .lineLimit(1)
                    .opacity(letters.isEmpty ? 0 : 1)
            }
            .dynamicTypeSize(...DynamicTypeSize.xLarge)
            .foregroundColor(.mdText)
        }
        .buttonStyle(ModemDeckDialKeyStyle(size: size))
        .simultaneousGesture(
            LongPressGesture(minimumDuration: 0.5).onEnded { _ in
                guard allowsPlus, digit == "0" else { return }
                zeroLongPressTriggered = true
                action("+")
            }
        )
        .accessibilityLabel(letters.isEmpty ? digit : "\(digit) \(letters)")
        .accessibilityHint(
            allowsPlus && digit == "0" ? plusHint : ""
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
    }
}

/// Keypad geometry derived from the space the dialer is given, so every key and the call
/// button fit without scrolling on short screens, sheets and iPad form sheets.
struct ModemDeckDialerMetrics {
    static let numberHeight: CGFloat = 54
    static let statusHeight: CGFloat = 24
    static let optionsHeight: CGFloat = 52
    static let minimumGap: CGFloat = 8
    static let bottomPadding: CGFloat = 12
    static let maximumKey: CGFloat = 76
    static let minimumKey: CGFloat = 52

    let key: CGFloat
    let rowSpacing: CGFloat
    let columnSpacing: CGFloat
    let callKey: CGFloat

    init(size: CGSize) {
        let chrome = Self.numberHeight + 3 + Self.statusHeight + Self.optionsHeight + 2 * Self.minimumGap + Self.bottomPadding
        let callKey = min(72, max(60, size.height * 0.1))
        // Four rows of keys plus three gaps of about a sixth of a key.
        let byHeight = (size.height - chrome - callKey) / (4 + 3 * 0.16)
        let byWidth = (min(size.width, 380) - 40) / (3 + 2 * 0.32)
        let key = max(Self.minimumKey, min(Self.maximumKey, byHeight, byWidth))
        self.key = key
        rowSpacing = (key * 0.16).rounded()
        columnSpacing = (key * 0.32).rounded()
        self.callKey = callKey
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

    /// Contacts whose number contains what has been typed, offered before the number is complete.
    private var suggestedContact: (contact: ModemDeckContact, phone: String)? {
        guard matchedContact == nil else { return nil }
        let typed = draft.number.filter(\.isNumber)
        guard typed.count >= 3 else { return nil }
        for contact in contactsStore.contacts {
            if let phone = contact.phones.first(where: { $0.displayNumber.filter(\.isNumber).contains(typed) }) {
                return (contact, phone.displayNumber)
            }
        }
        return nil
    }

    /// One status line under the number: problems first, then who is being called, then guidance.
    private var statusLine: (text: String, problem: Bool)? {
        if !callController.errorMessage.isEmpty { return (callController.errorMessage, true) }
        if !recordingError.isEmpty { return (recordingError, true) }
        if lines.isEmpty || !controller.isOnline {
            return (controller.text("无可用线路，请检查线路连接。", "No available line. Check your connection."), true)
        }
        if !draft.number.isEmpty && dialTarget.error != .none {
            return (controller.text("请输入有效的电话号码", "Enter a valid phone number"), true)
        }
        if let matchedContact { return (matchedContact.displayName, false) }
        if draft.number.isEmpty {
            return (controller.text("选择联系人，或直接输入号码", "Choose a contact or enter a number"), false)
        }
        return nil
    }

    var body: some View {
        GeometryReader { geometry in
            let metrics = ModemDeckDialerMetrics(size: geometry.size)
            VStack(spacing: 0) {
                numberSection
                optionsRow.padding(.top, 8)
                Spacer(minLength: ModemDeckDialerMetrics.minimumGap)
                LazyVGrid(
                    columns: Array(repeating: GridItem(.fixed(metrics.key), spacing: metrics.columnSpacing), count: 3),
                    spacing: metrics.rowSpacing
                ) {
                    ForEach(Array(keys.enumerated()), id: \.offset) { _, key in
                        ModemDeckDialKeyButton(digit: key.0, letters: key.1, size: metrics.key,
                                               plusHint: controller.text("长按输入 +", "Long press to enter plus"),
                                               action: appendDigit)
                            .accessibilityIdentifier("dial-key-\(key.0)")
                    }
                }
                Spacer(minLength: ModemDeckDialerMetrics.minimumGap)
                // The call key never scrolls out of reach: this row is pinned to the bottom.
                actionRow(callSize: metrics.callKey)
            }
            .frame(maxWidth: 340)
            .padding(.horizontal, 20)
            .padding(.bottom, ModemDeckDialerMetrics.bottomPadding)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
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

    private var numberSection: some View {
        VStack(spacing: 3) {
            ModemDeckDialNumberField(text: $draft.number,
                placeholder: controller.text("输入号码", "Enter number"),
                accessibilityLabel: controller.text("电话号码", "Phone number"))
                .frame(height: ModemDeckDialerMetrics.numberHeight)
            Group {
                if let suggestedContact {
                    Button {
                        draft.number = suggestedContact.phone
                        if !draft.lineSelectionOverridden {
                            draft.selectedLineID = controller.voiceDialLine(
                                preferredID: suggestedContact.contact.preferredLineId
                            )?.id ?? ""
                        }
                    } label: {
                        HStack(spacing: 6) {
                            Image(systemName: "person.crop.circle")
                            Text(suggestedContact.contact.displayName).fontWeight(.semibold)
                            Text(suggestedContact.phone).foregroundColor(.mdMuted)
                        }
                        .lineLimit(1)
                        .foregroundColor(.mdAccent)
                    }
                    .buttonStyle(.plain)
                    .accessibilityHint(controller.text("填入此号码", "Fills in this number"))
                    .accessibilityIdentifier("dial-suggestion")
                } else if let statusLine {
                    Text(statusLine.text)
                        .foregroundColor(statusLine.problem ? .mdDanger : .mdMuted)
                        .lineLimit(2)
                        .multilineTextAlignment(.center)
                } else {
                    Text(" ").accessibilityHidden(true)
                }
            }
            .font(.footnote)
            .frame(minHeight: ModemDeckDialerMetrics.statusHeight)
        }
    }

    /// Line and per-call recording sit side by side; narrow widths stack them.
    private var optionsRow: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 8) { linePicker; recordingToggle }
            VStack(spacing: 6) { linePicker; recordingToggle }
        }
    }

    private var linePicker: some View {
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
                if let selectedLine {
                    ModemDeckLineTag(line: selectedLine, compact: true)
                } else {
                    Text(controller.text("无可用线路", "No available line"))
                        .lineLimit(1)
                }
                Image(systemName: "chevron.down").font(.caption2.weight(.semibold))
            }
            .font(.subheadline.weight(.medium)).foregroundColor(.mdText)
            .padding(.horizontal, 14).frame(minHeight: 36)
            .background(Capsule().fill(Color.mdSurfaceHover))
            .fixedSize()
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .disabled(lines.isEmpty)
        .accessibilityLabel(controller.text("通话线路", "Calling line"))
        .accessibilityValue(selectedLine?.displayName ?? "")
    }

    /// Starts from the account default and applies to this call only.
    private var recordingToggle: some View {
        Button {
            draft.recording.toggle()
            UISelectionFeedbackGenerator().selectionChanged()
        } label: {
            HStack(spacing: 6) {
                if draft.recordingInitialized {
                    Image(systemName: draft.recording ? "record.circle.fill" : "record.circle")
                } else {
                    ProgressView().controlSize(.mini)
                }
                Text(controller.text("录音", "Record"))
            }
            .font(.subheadline.weight(.medium))
            .foregroundColor(draft.recording ? .mdDanger : .mdMuted)
            .padding(.horizontal, 14).frame(minHeight: 36)
            .background(Capsule().fill(draft.recording ? Color.mdDangerSoft : Color.mdSurfaceHover))
            .fixedSize()
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!draft.recordingInitialized)
        .accessibilityLabel(controller.text("本次通话录音", "Record this call"))
        .accessibilityValue(draft.recording ? controller.text("已开启", "On") : controller.text("已关闭", "Off"))
        .accessibilityAddTraits(draft.recording ? .isSelected : [])
        .accessibilityIdentifier("dial-recording-status")
    }

    private func actionRow(callSize: CGFloat) -> some View {
        HStack(spacing: 14) {
            Button { showingContacts = true } label: {
                Image(systemName: "person.crop.circle")
                    .font(.title2).frame(width: 44, height: 56)
            }
            .accessibilityLabel(controller.text("选择联系人", "Choose contact"))
            Spacer(minLength: 0)
            // Round green call key, as in the system Phone keypad.
            Button(action: startCall) {
                Group {
                    if callController.busy { ProgressView().tint(.white) }
                    else { Image(systemName: "phone.fill").font(.system(size: callSize * 0.39, weight: .semibold)) }
                }
                .foregroundColor(canDial ? .white : .mdFaint)
                .frame(width: callSize, height: callSize)
                .background(Circle().fill(canDial ? Color.mdCallAnswer : Color.mdSurfaceHover))
            }
            .disabled(!canDial)
            .accessibilityLabel(controller.text("拨打", "Call"))
            .accessibilityIdentifier("dial-call")
            Spacer(minLength: 0)
            Button(action: removeDigit) {
                Image(systemName: "delete.left")
                    .font(.title3).frame(width: 44, height: 56)
            }
            // Present only once there is something to delete, as in Phone.
            .opacity(draft.number.isEmpty ? 0 : 1)
            .disabled(draft.number.isEmpty)
            .accessibilityHidden(draft.number.isEmpty)
            .accessibilityLabel(controller.text("删除一位", "Delete digit"))
            .accessibilityIdentifier("dial-delete")
        }
        .buttonStyle(.plain).foregroundColor(.mdText)
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
                                    Text(controller.phoneLabelText(phone.label)).font(.footnote).foregroundColor(.mdMuted)
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
                                       recording: draft.recordingInitialized ? draft.recording : nil,
                                       reportsFailureInline: true)
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
                ModemDeckSplitColumns(controller: controller, detailID: selectedCallID) {
                    callListColumn
                } detail: {
                    callDetail
                }
            } else {
                callListColumn
            }
        }
        .background(Color.mdBackground)
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
            Text(modemDeckCallDeletionMessage(
                controller: controller,
                calls: selectedCalls.count,
                recordings: recordingCount(for: selectedCalls)
            ))
        }
    }

    private func recordingCount(for calls: [ModemDeckCallRecord]) -> Int {
        let ids = Set(calls.map(\.id))
        return store.recordings.filter { ids.contains($0.call.id) }.count
    }

    private var callListColumn: some View {
        VStack(spacing: 0) {
            ModemDeckCollectionHeader(controller: controller, title: controller.text("通话", "Calls"), query: $query, selecting: selecting) {}
            filterBar
            callList
        }
        .background(Color.mdBackground)
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if selecting { callBatchBar() }
        }
        .overlay(alignment: .top) {
            if selecting { callBatchBar(.header) }
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

    private func callBatchBar(_ part: ModemDeckBatchActionBarPart = .toolbar) -> some View {
        let selected = selectedCalls
        let missed = selected.filter(\.missed)
        // Any unread missed call makes "Mark Read" the primary action, as in Messages.
        let missedHasUnread = missed.contains { !$0.read }
        let allFavorite = !selected.isEmpty && selected.allSatisfy(\.favorite)

        return ModemDeckBatchActionBar(
            part: part,
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
                    title: missedHasUnread
                        ? controller.text("标为已读", "Mark Read")
                        : controller.text("标为未读", "Mark Unread"),
                    icon: missedHasUnread ? "phone.badge.checkmark" : "phone.badge.waveform",
                    disabled: batchBusy || !controller.isOnline
                ) { mutateSelectedCalls(missedHasUnread ? .read : .unread, calls: missed) }
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

/// The server deletes a call's recording segments with the call, so every
/// call-deletion confirmation names the recordings it will remove.
@MainActor
private func modemDeckCallDeletionMessage(
    controller: ModemDeckSessionController,
    calls: Int,
    recordings: Int
) -> String {
    let recordingNoun = recordings == 1 ? "recording" : "recordings"
    if calls == 1 {
        return recordings > 0
            ? controller.text(
                "该通话记录及其 \(recordings) 个录音将被永久删除。",
                "This call record and its \(recordings) \(recordingNoun) will be permanently deleted."
            )
            : controller.text("该通话记录将被永久删除。", "This call record will be permanently deleted.")
    }
    return recordings > 0
        ? controller.text(
            "将永久删除 \(calls) 条通话记录及其 \(recordings) 个录音。",
            "This permanently deletes \(calls) call records and their \(recordings) \(recordingNoun)."
        )
        : controller.text(
            "将永久删除 \(calls) 条通话记录。",
            "This permanently deletes \(calls) call records."
        )
}

struct ModemDeckCallRecordRow: View {
    let call: ModemDeckCallRecord
    var contact: ModemDeckContact? = nil
    @ObservedObject var controller: ModemDeckSessionController
    var recorded = false
    var contextActions: [ModemDeckContextAction] = []
    @Environment(\.modemDeckRowsShowTimeOnly) private var rowsShowTimeOnly

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
                ModemDeckRowTitleLine {
                    Text(displayName)
                        .font(.subheadline.weight(.semibold))
                        .foregroundColor(call.missed ? .mdDanger : .mdText)
                } trailing: {
                    Text(rowsShowTimeOnly
                        ? controller.timeOfDayText(call.startedAt)
                        : controller.compactDateText(call.startedAt))
                        .font(.caption)
                        .foregroundColor(.mdFaint)
                }
                HStack(spacing: 7) {
                    // Like Messages, the line tag only distinguishes calls when there is a choice.
                    if let line, (controller.bootstrap?.lineCatalog.count ?? 0) > 1 {
                        ModemDeckLineTag(line: line, compact: true)
                            .font(.caption.weight(.medium))
                            .foregroundColor(.mdMuted)
                    }
                    Image(systemName: directionIcon)
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundColor(call.missed ? .mdDanger : .mdMuted)
                    // An unknown caller's number is already the title; say what kind of call it was.
                    Text(displayName == call.remoteNumber ? directionText : call.remoteNumber)
                        .font(.footnote)
                        .foregroundColor(.mdMuted)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    if recorded {
                        Image(systemName: "waveform")
                            .font(.system(size: 12))
                            .foregroundColor(.mdAccent)
                    }
                    if call.favorite {
                        Image(systemName: "star.fill")
                            .font(.system(size: 12))
                            .foregroundColor(.mdFavorite)
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
        if call.missed { return "phone.arrow.down.left" }
        return call.direction == "incoming" ? "arrow.down.left" : "arrow.up.right"
    }

    private var directionText: String {
        if call.missed { return controller.text("未接来电", "Missed") }
        return call.direction == "incoming"
            ? controller.text("呼入", "Incoming")
            : controller.text("呼出", "Outgoing")
    }
}

struct ModemDeckRecordingsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    var showsBackButton = false
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
                ModemDeckSplitColumns(controller: controller, detailID: selectedRecordingID) {
                    recordingListColumn
                } detail: {
                    recordingDetail
                }
            } else {
                recordingListColumn
            }
        }
        .background(Color.mdBackground)
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
                selecting: selecting) {}
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
            if selecting { recordingBatchBar() }
        }
        .overlay(alignment: .top) {
            if selecting { recordingBatchBar(.header) }
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

    private func recordingBatchBar(_ part: ModemDeckBatchActionBarPart = .toolbar) -> some View {
        let selected = selectedRecordings
        let allFavorite = !selected.isEmpty && selected.allSatisfy(\.favorite)

        return ModemDeckBatchActionBar(
            part: part,
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
                    if let line {
                        ModemDeckLineTag(line: line, compact: true)
                            .font(.caption.weight(.medium))
                            .foregroundColor(.mdMuted)
                    }
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
                            .foregroundColor(.mdFavorite)
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
    var busy = false

    var body: some View {
        // Contact-card style action tile: symbol above a short label.
        VStack(spacing: 4) {
            if busy {
                ProgressView().tint(prominent ? .mdOnAccent : .mdAccent)
                    .frame(height: 22)
            } else {
                Image(systemName: icon)
                    .symbolVariant(.fill)
                    .font(.title3.weight(.semibold))
                    .frame(height: 22)
            }
            Text(title)
                .font(.caption.weight(.semibold))
                .lineLimit(1)
                .minimumScaleFactor(0.8)
        }
        .foregroundColor(prominent ? .mdOnAccent : .mdAccent)
        .frame(maxWidth: .infinity, minHeight: 58)
        .background(prominent ? Color.mdAccent : Color.mdSurface)
        .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.medium))
        .overlay(
            RoundedRectangle.modemDeck(ModemDeckRadius.medium)
                .stroke(prominent ? Color.clear : Color.mdBorder, lineWidth: 0.5)
        )
        .accessibilityElement(children: .combine)
    }
}

struct ModemDeckCallDetailView: View {
    private let initialCall: ModemDeckCallRecord
    @ObservedObject private var store: ModemDeckCallsStore

    private var call: ModemDeckCallRecord {
        store.calls.first(where: { $0.id == initialCall.id }) ?? initialCall
    }

    private var recordings: [ModemDeckRecording] {
        store.recordings.filter { $0.call.id == initialCall.id }
    }
    @ObservedObject var controller: ModemDeckSessionController
    @ObservedObject private var callController: ModemDeckCallController
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
        initialCall = call
        _store = ObservedObject(wrappedValue: controller.callsStore)
        self.controller = controller
        _callController = ObservedObject(wrappedValue: controller.callController)
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
                moreActionsTitle: controller.text("更多操作", "More actions"),
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
            )

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
                                prominent: true,
                                busy: callController.busy
                            )
                        }
                        .buttonStyle(.plain)
                        .disabled(dialLine == nil || callController.busy || !controller.isOnline)

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
        .modemDeckInteractiveBack(showsBackButton)
        .onChange(of: call) { value in
            favorite = value.favorite
            isRead = value.read
        }
        // Opening a missed call has seen it, as in Phone; the Calls badge clears with it.
        .task(id: call.id) {
            if call.missed && !isRead && controller.isOnline { mutateCall(.read) }
        }
        .alert(
            controller.text("删除通话记录？", "Delete Call Record?"),
            isPresented: $confirmDelete
        ) {
            Button(controller.text("取消", "Cancel"), role: .cancel) {}
            Button(controller.text("删除", "Delete"), role: .destructive) { deleteCall() }
        } message: {
            Text(modemDeckCallDeletionMessage(
                controller: controller,
                calls: 1,
                recordings: recordings.count
            ))
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
            color: favorite ? .mdFavorite : .mdMuted,
            disabled: mutationBusy || !controller.isOnline
        ) { mutateCall(favorite ? .unfavorite : .favorite) })
        items.append(ModemDeckHeaderAction(
            id: "delete",
            icon: "trash",
            title: controller.text("删除通话", "Delete call"),
            color: .mdDanger,
            disabled: mutationBusy || !controller.isOnline,
            destructive: true
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
        if call.missed { return "phone.arrow.down.left" }
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
    /// True while this player holds the media session it activated.
    private var ownsPlaybackSession = false

    init(api: ModemDeckAPIClient, callID: String, segmentID: String) {
        self.api = api
        self.callID = callID
        self.segmentID = segmentID
    }

    var isLoaded: Bool { player != nil }
    var currentTime: TimeInterval { player?.currentTime ?? 0 }
    var duration: TimeInterval { player?.duration ?? 0 }

    /// Seeks within the loaded recording; the UI only offers seeking after the first load.
    func seek(to time: TimeInterval) {
        guard let player else { return }
        player.currentTime = min(max(0, time), player.duration)
        objectWillChange.send()
    }

    func skip(by seconds: TimeInterval) {
        seek(to: currentTime + seconds)
    }

    func toggle() {
        if let player {
            if player.isPlaying {
                player.pause()
                playing = false
                deactivatePlaybackSession()
            } else {
                activatePlaybackSession()
                playing = player.play()
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
                activatePlaybackSession()
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
            self?.deactivatePlaybackSession()
        }
    }

    /// Calls leave the shared session in playAndRecord/voiceChat, which routes
    /// playback to the receiver; the default category obeys the silent switch.
    /// Recordings use media playback unless CallKit owns a live call session.
    private func activatePlaybackSession() {
        guard !ModemDeckCallAudioSession.callAudioActive else { return }
        let session = AVAudioSession.sharedInstance()
        do {
            try session.setCategory(.playback, mode: .spokenAudio)
            try session.setActive(true)
            ownsPlaybackSession = true
        } catch {
            ModemDeckDiagnostics.shared.record(.audio, "recording_session_failed", error: error)
        }
    }

    /// Lets other apps resume after a pause, the end, or leaving the screen.
    private func deactivatePlaybackSession() {
        guard ownsPlaybackSession else { return }
        ownsPlaybackSession = false
        Self.releasePlaybackSession()
    }

    deinit {
        // The detail view went away, possibly mid-playback; the player stops
        // with this object, so hand the session back as well.
        player?.stop()
        if ownsPlaybackSession { Self.releasePlaybackSession() }
    }

    private nonisolated static func releasePlaybackSession() {
        guard !ModemDeckCallAudioSession.callAudioActive else { return }
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }
}

struct ModemDeckRecordingDetailView: View {
    private let initialRecording: ModemDeckRecording
    @ObservedObject private var store: ModemDeckCallsStore

    private var recording: ModemDeckRecording {
        store.recordings.first(where: { $0.id == initialRecording.id }) ?? initialRecording
    }
    @ObservedObject var controller: ModemDeckSessionController
    @ObservedObject private var callController: ModemDeckCallController
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
        initialRecording = recording
        _store = ObservedObject(wrappedValue: controller.callsStore)
        self.controller = controller
        _callController = ObservedObject(wrappedValue: controller.callController)
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
                moreActionsTitle: controller.text("更多操作", "More actions"),
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
            )

            if !mutationError.isEmpty {
                ModemDeckInlineError(message: mutationError)
                    .padding(.horizontal, 16)
                    .padding(.top, 8)
            }

            ScrollView {
                VStack(spacing: 18) {
                    ModemDeckDetailCard(title: controller.text("播放", "Playback")) {
                        ModemDeckRecordingPlaybackControls(
                            player: player,
                            controller: controller,
                            playable: recording.playable,
                            knownDuration: TimeInterval(recording.segment.durationMs) / 1_000
                        )
                    }

                    if !player.errorMessage.isEmpty {
                        ModemDeckInlineError(message: player.errorMessage)
                    }

                    HStack(spacing: 10) {
                        Button { startCall() } label: {
                            ModemDeckDetailAction(
                                title: controller.text("呼叫", "Call"),
                                icon: "phone.fill",
                                prominent: true,
                                busy: callController.busy
                            )
                        }
                        .buttonStyle(.plain)
                        .disabled(dialLine == nil || callController.busy || !controller.isOnline)

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
                color: favorite ? .mdFavorite : .mdMuted,
                disabled: mutationBusy || !controller.isOnline
            ) { toggleFavorite() },
            ModemDeckHeaderAction(
                id: "delete",
                icon: "trash",
                title: controller.text("删除录音", "Delete recording"),
                color: .mdDanger,
                disabled: mutationBusy || !controller.isOnline,
                destructive: true
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

/// Shared call clock: `mm:ss`, then `h:mm:ss` past an hour (matches Web).
func modemDeckCallClock(_ interval: TimeInterval) -> String {
    let total = max(0, Int(interval))
    let hours = total / 3600
    let minutes = (total % 3600) / 60
    let seconds = total % 60
    return hours > 0
        ? String(format: "%d:%02d:%02d", hours, minutes, seconds)
        : String(format: "%02d:%02d", minutes, seconds)
}

private func modemDeckCallLine(_ call: ModemDeckPresentedCall, in bootstrap: ModemDeckBootstrap?) -> ModemDeckLine? {
    guard let bootstrap else { return nil }
    return bootstrap.lines.first(where: { $0.id == call.lineID }) ??
        (call.lineID.isEmpty && bootstrap.lines.count == 1 ? bootstrap.lines.first : nil)
}

struct ModemDeckMiniCallBar: View {
    let call: ModemDeckPresentedCall
    @ObservedObject var controller: ModemDeckSessionController
    @ObservedObject var callController: ModemDeckCallController
    let restore: () -> Void
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var endedCall: ModemDeckEndedCall? { callController.endedCall?.id == call.callID ? callController.endedCall : nil }
    private var isLive: Bool { endedCall == nil && !callController.ending && call.state == "active" }
    private var activeDate: Date? { call.activeAt.flatMap { ModemDeckDateText.date($0) } }
    private var name: String { call.displayName.isEmpty ? call.remoteNumber : call.displayName }
    private var line: ModemDeckLine? { modemDeckCallLine(call, in: controller.bootstrap) }

    private var canEnd: Bool {
        if call.testCall || controller.bootstrap == nil { return true }
        let line = controller.bootstrap?.lines.first { $0.id == call.lineID }
        return call.state == "ringing" && call.direction == "incoming"
            ? line?.capabilities?.rejectCall == true : line?.capabilities?.hangupCall == true
    }

    private var endLabel: String {
        endedCall == nil ? controller.text("挂断", "End call") : controller.text("关闭结束结果", "Close call result")
    }

    private var muteLabel: String {
        call.muted ? controller.text("取消静音", "Unmute") : controller.text("静音", "Mute")
    }

    private func status(at now: Date) -> String {
        if let endedCall { return controller.text(endedCall.message.chinese, endedCall.message.english) }
        if callController.ending { return controller.text("正在结束…", "Ending…") }
        if isLive, let activeDate { return modemDeckCallClock(now.timeIntervalSince(activeDate)) }
        if call.state == "ringing" && call.direction == "incoming" { return controller.text("来电", "Incoming Call") }
        return controller.text("通话进行中", "Call in progress")
    }

    var body: some View {
        TimelineView(.periodic(from: activeDate ?? Date(), by: 1)) { context in
            bar(status: status(at: context.date))
        }
        .opacity(callController.endedCallClosing ? 0 : 1)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: callController.endedCallClosing)
        .allowsHitTesting(!callController.endedCallClosing)
    }

    private func bar(status: String) -> some View {
        HStack(spacing: 0) {
            Button(action: restore) {
                HStack(spacing: 12) {
                    Image(systemName: endedCall == nil ? "waveform" : "phone.down.fill")
                        .font(.body.weight(.semibold))
                        .foregroundColor(endedCall == nil ? .mdAccent : .mdMuted)
                        .modemDeckVariableColor(isLive && !reduceMotion)
                        .frame(width: 24)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(name)
                            .font(.subheadline.weight(.semibold))
                            .foregroundColor(.mdText)
                            .lineLimit(1)
                        if let line {
                            ModemDeckLineTag(line: line, compact: true)
                                .font(.caption)
                                .foregroundColor(.mdMuted)
                        }
                    }
                    Spacer(minLength: 4)
                    Text(status)
                        .font(.subheadline.weight(isLive ? .semibold : .regular))
                        .monospacedDigit()
                        .foregroundColor(isLive ? .mdText : .mdMuted)
                        .lineLimit(1)
                }
                .padding(.leading, ModemDeckSpacing.md)
                .padding(.trailing, ModemDeckSpacing.xxs)
                .frame(minHeight: 56)
                .contentShape(Rectangle())
            }
            .accessibilityLabel(
                [isLive ? controller.text("通话中", "In call") : nil, name, status]
                    .compactMap { $0 }
                    .joined(separator: " · ")
            )
            .accessibilityHint(controller.text("恢复通话", "Return to call"))
            .accessibilityActions {
                if isLive {
                    Button(muteLabel) { Task { await callController.setMuted(!call.muted) } }
                }
                Button(endLabel) { endOrClose() }
            }
            .accessibilityIdentifier("call-restore")

            if isLive {
                Button {
                    Task { await callController.setMuted(!call.muted) }
                } label: {
                    Image(systemName: "mic.slash.fill")
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundColor(call.muted ? .mdSurface : .mdText)
                        .frame(width: 36, height: 36)
                        .background(Circle().fill(call.muted ? Color.mdText : Color.mdSurfaceHover))
                        .frame(width: 44, height: 44)
                        .contentShape(Rectangle())
                }
                .disabled(callController.muteBusy)
                .accessibilityLabel(controller.text("静音", "Mute"))
                .accessibilityValue(call.muted ? controller.text("已开启", "On") : controller.text("已关闭", "Off"))
                .accessibilityAddTraits(call.muted ? .isSelected : [])
                .modemDeckSelectionFeedback(call.muted)
                .accessibilityIdentifier("call-mini-mute")
            }
            Button(action: endOrClose) {
                Group {
                    if callController.ending { ProgressView().tint(.white) }
                    else { Image(systemName: endedCall == nil ? "phone.down.fill" : "xmark") }
                }
                .font(.system(size: 15, weight: .semibold))
                .foregroundColor(.white)
                .frame(width: 36, height: 36)
                .background(Circle().fill(Color.mdCallEnd))
                .frame(width: 44, height: 44)
                .contentShape(Rectangle())
            }
            .disabled(endedCall == nil && (!canEnd || callController.ending))
            .accessibilityLabel(endLabel)
            .accessibilityIdentifier("call-mini-end")
            .padding(.trailing, ModemDeckSpacing.xs)
        }
        .buttonStyle(.plain)
        .modemDeckCallBarSurface()
        .overlay(alignment: .leading) {
            Rectangle()
                .fill(endedCall == nil ? Color.mdAccent : Color.mdBorder)
                .frame(width: 3)
                .accessibilityHidden(true)
        }
        .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.large))
        .overlay(RoundedRectangle.modemDeck(ModemDeckRadius.large).stroke(Color.mdBorder, lineWidth: 0.5))
        .shadow(color: Color.black.opacity(0.12), radius: 12, y: 4)
    }

    private func endOrClose() {
        if endedCall != nil { callController.closeEndedCall() }
        else { Task { await callController.end() } }
    }
}

enum ModemDeckCallStageLayout {
    /// Below this scrollable stage height the avatar and spacing shrink to keep controls visible.
    static let compactHeight: CGFloat = 480
    /// Four 84 pt control cells with 16 pt gaps.
    static let oneRowControlsWidth: CGFloat = 4 * 84 + 3 * 16
    static let widePanelMaximumHeight: CGFloat = 760

    /// The DTMF keypad shows all twelve keys in the stage it is given instead of scrolling
    /// its last row under the footer: room for the compact identity and pressed digits first.
    static func keypadKey(for size: CGSize) -> CGFloat {
        let identityAndDigits: CGFloat = 150
        let byHeight = (size.height - identityAndDigits) / (4 + 3 * 0.16)
        let byWidth = size.width / (3 + 2 * 0.32)
        return max(48, min(76, byHeight, byWidth))
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
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @Environment(\.verticalSizeClass) private var verticalSizeClass

    private var endedCall: ModemDeckEndedCall? { callController.endedCall?.id == call.callID ? callController.endedCall : nil }
    private var isEnded: Bool { endedCall != nil }
    private var isRinging: Bool { !isEnded && call.state == "ringing" }
    private var isActive: Bool { !isEnded && call.state == "active" }

    private var callLine: ModemDeckLine? { modemDeckCallLine(call, in: controller.bootstrap) }

    private var contact: ModemDeckContact? {
        call.testCall ? nil : controller.contactsStore.contacts.modemDeckContact(id: nil, number: call.remoteNumber)
    }

    private var displayName: String {
        if !call.displayName.isEmpty { return call.displayName }
        return contact?.displayName ?? call.remoteNumber
    }

    private var canAnswer: Bool {
        if call.testCall { return true }
        guard let bootstrap = controller.bootstrap else { return true }
        return bootstrap.capabilities.wssAudio &&
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
        !isEnded && !call.testCall && callLine?.capabilities?.sendDtmf != false
    }

    /// The controller applies recording to an outgoing call before it connects.
    private var canRecordBeforeConnect: Bool {
        !isEnded && !isActive && !call.testCall && call.direction == "outgoing" && !callController.ending
    }

    var body: some View {
        GeometryReader { geometry in
            // Regular in both axes covers iPad and iPhone Duo's unfolded display; compact
            // scenes such as Slide Over or the folded cover screen keep the full-screen stage.
            let presentsAsPadPanel = horizontalSizeClass == .regular && verticalSizeClass == .regular
            // A short regular scene (iPad or iPhone Duo in landscape) gets a wider panel so the
            // call controls can sit in one row instead of overflowing the height.
            let panelWidth = min(
                geometry.size.height < ModemDeckCallStageLayout.widePanelMaximumHeight ? 480 : 420,
                max(0, geometry.size.width - 24)
            )
            let panelHeight = min(720, max(0, geometry.size.height - 24))

            ZStack {
                if presentsAsPadPanel {
                    Color.black.opacity(0.38)
                        .ignoresSafeArea()

                    callStage
                        .frame(width: panelWidth, height: panelHeight)
                        .background(callBackground)
                        .clipShape(RoundedRectangle.modemDeck(ModemDeckRadius.xLarge))
                        .overlay(
                            RoundedRectangle.modemDeck(ModemDeckRadius.xLarge)
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

    /// Accent-soft wash into the surface, so the live call reads apart from
    /// the workspace behind it. Both tokens keep text above 4.5:1.
    private var callBackground: some View {
        LinearGradient(
            stops: [
                .init(color: .mdAccentSoft, location: 0),
                .init(color: .mdSurface, location: 0.42)
            ],
            startPoint: .top,
            endPoint: .bottom
        )
    }

    private var collapseHeader: some View {
        HStack {
            Color.clear.frame(width: 44, height: 44).accessibilityHidden(true)
            Spacer()
            if isEnded {
                Text(controller.text("通话结果", "Call result")).font(.headline)
            }
            Spacer()
            Button(action: collapse) {
                Image(systemName: "chevron.down")
                    .font(.body.weight(.semibold))
                    .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
                    .frame(width: 44, height: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .hoverEffect(.highlight)
            .accessibilityLabel(controller.text("收起通话", "Collapse call"))
            .accessibilityIdentifier("call-collapse")
        }
        .foregroundColor(.mdText)
        .padding(.top, 14)
        .overlay(alignment: .top) {
            Capsule().fill(Color.mdBorder).frame(width: 36, height: 5).padding(.top, 5)
                .accessibilityHidden(true)
        }
        // Without a title the row would otherwise merge into the collapse button.
        .accessibilityElement(children: .contain)
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
    private var keypadVisible: Bool { showingKeypad && isActive && canSendDTMF }

    private var callStage: some View {
        VStack(spacing: 0) {
            collapseHeader
            GeometryReader { available in
                // Short stages (iPhone Duo's cover screen, iPhone SE) tighten the identity block
                // so the controls fit without scrolling.
                // Accessibility text sizes take the same tighter stage, so the larger labels fit.
                let compact = available.size.height < ModemDeckCallStageLayout.compactHeight ||
                    dynamicTypeSize.isAccessibilitySize
                let controlsInOneRow = available.size.width >= ModemDeckCallStageLayout.oneRowControlsWidth
                let keypadKey = ModemDeckCallStageLayout.keypadKey(for: available.size)
                ScrollView {
                    VStack(spacing: 0) {
                        Spacer(minLength: keypadVisible || compact ? ModemDeckSpacing.xs : ModemDeckSpacing.md)
                        callIdentity(compact: compact)
                        Spacer(minLength: keypadVisible ? ModemDeckSpacing.lg : compact ? ModemDeckSpacing.xs : ModemDeckSpacing.xxl)
                        VStack(spacing: ModemDeckSpacing.xl) {
                            if !isIncomingRinging && !isEnded {
                                if call.testAudio && isActive { testAudioFeedback }
                                if keypadVisible {
                                    ModemDeckInCallKeypad(
                                        keySize: keypadKey,
                                        pressedDigits: dtmfDigits,
                                        pressedLabel: controller.text("已按按键", "Pressed keys"),
                                        action: sendDTMF
                                    )
                                } else if !controlKinds.isEmpty {
                                    auxiliaryControls(oneRow: controlsInOneRow, compact: compact,
                                                      width: available.size.width)
                                }
                            }
                            ModemDeckInlineError(message: callController.errorMessage)
                            if !isEnded { ModemDeckInlineError(message: audioRoute.errorMessage) }
                            if let endedCall, !endedCall.failureMessage.isEmpty { ModemDeckInlineError(message: endedCall.failureMessage) }
                        }
                    }
                    .foregroundColor(.mdText)
                    .padding(.vertical, ModemDeckSpacing.xs)
                    .frame(maxWidth: .infinity, minHeight: available.size.height)
                }
            }
            callFooter
                .padding(.top, ModemDeckSpacing.xl)
                .padding(.bottom, ModemDeckSpacing.xl)
        }
        .frame(maxWidth: 640)
        .padding(.horizontal, ModemDeckSpacing.xl)
        .offset(y: collapseOffset)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.18), value: collapseOffset == 0)
        .accessibilityAction(.escape, collapse)
    }

    // MARK: Identity

    private var showsNumber: Bool {
        !call.remoteNumber.isEmpty && displayName != call.remoteNumber
    }

    private func callIdentity(compact: Bool) -> some View {
        VStack(spacing: 0) {
            if !keypadVisible {
                callAvatar(compact: compact).padding(.bottom, compact ? ModemDeckSpacing.xs : ModemDeckSpacing.md)
            }
            Text(displayName)
                .font(keypadVisible ? .title3.weight(.semibold) : .title.weight(.semibold))
                .lineLimit(dynamicTypeSize.isAccessibilitySize ? 2 : 1)
                .minimumScaleFactor(0.7)
                .multilineTextAlignment(.center)
            if !keypadVisible, showsNumber || callLine != nil {
                subtitleRow.padding(.top, 6)
            }
            statusSlot.padding(.top, keypadVisible ? ModemDeckSpacing.xxs : ModemDeckSpacing.sm)
            if canRecordBeforeConnect {
                Text(callController.recordingReady
                    ? controller.text(callController.recordingEnabled ? "录音已开启" : "录音已关闭", callController.recordingEnabled ? "Recording on" : "Recording off")
                    : controller.text("正在读取录音设置…", "Loading recording setting…"))
                    .font(.footnote).foregroundColor(.mdMuted)
                    .padding(.top, 6)
                    .accessibilityIdentifier("call-recording-status")
            }
        }
    }

    @ViewBuilder private func callAvatar(compact: Bool) -> some View {
        let size: CGFloat = compact || dynamicTypeSize.isAccessibilitySize ? 64 : 96
        if call.testCall {
            Image(systemName: "checkmark.shield.fill")
                .font(.system(size: compact ? 28 : 40, weight: .medium))
                .foregroundColor(.mdAccent)
                .frame(width: size, height: size)
                .background(Circle().fill(Color.mdSurface))
                .overlay(Circle().stroke(Color.mdBorder, lineWidth: 1))
                .accessibilityHidden(true)
        } else {
            let known = contact != nil || (!call.displayName.isEmpty && call.displayName != call.remoteNumber)
            ModemDeckAvatar(
                name: displayName,
                avatarSource: contact?.avatar,
                fallback: known ? .initials : .unknown,
                paletteKey: call.remoteNumber,
                size: size
            )
        }
    }

    private var subtitleRow: some View {
        let number = Text(call.remoteNumber).monospacedDigit()
        return ViewThatFits(in: .horizontal) {
            HStack(spacing: 6) {
                if showsNumber { number }
                if showsNumber, callLine != nil { Text("·").accessibilityHidden(true) }
                if let callLine { ModemDeckLineTag(line: callLine, compact: true) }
            }
            VStack(spacing: 4) {
                if showsNumber { number }
                if let callLine { ModemDeckLineTag(line: callLine, compact: true) }
            }
        }
        .font(.subheadline)
        .foregroundColor(.mdMuted)
        .lineLimit(dynamicTypeSize.isAccessibilitySize ? 2 : 1)
        .multilineTextAlignment(.center)
        .accessibilityElement(children: .combine)
    }

    private var connectedText: String { controller.text("通话中", "Connected") }

    @ViewBuilder private var statusSlot: some View {
        if let endedCall {
            VStack(spacing: 4) {
                Text(stateText)
                    .font(.title3.weight(.medium))
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("call-ended-status")
                if let elapsed = endedCall.elapsedSeconds {
                    Text(controller.text("通话时长 ", "Duration ") + modemDeckCallClock(TimeInterval(elapsed)))
                        .font(.subheadline).monospacedDigit().foregroundColor(.mdMuted)
                        .accessibilityIdentifier("call-ended-duration")
                }
            }
        } else if isActive, let activeAt = call.activeAt, let date = ModemDeckDateText.date(activeAt) {
            VStack(spacing: 4) {
                ModemDeckCallTimerText(
                    start: date,
                    font: keypadVisible ? .headline : .title2.weight(.medium),
                    accessibilityLabel: stateText
                )
                if stateText != connectedText {
                    Text(stateText)
                        .font(.subheadline.weight(.medium)).foregroundColor(.mdMuted)
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                        .contentTransition(reduceMotion ? .identity : .opacity)
                        .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: stateText)
                        .accessibilityIdentifier("call-status")
                }
            }
        } else {
            Text(stateText)
                .font(.title3.weight(.medium))
                .foregroundColor(isIncomingRinging ? .mdText : .mdMuted)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
                .contentTransition(reduceMotion ? .identity : .opacity)
                .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: stateText)
                .accessibilityIdentifier("call-status")
        }
    }

    // MARK: Controls

    private enum ControlKind: Hashable { case mute, keypad, audio, record }

    /// Mute · Keypad · Audio · Record. Only actions this call supports. An outgoing call shows
    /// the same grid while dialling, with what needs a connection disabled, so nothing jumps.
    private var controlKinds: [ControlKind] {
        if isActive {
            return call.testCall ? [.mute, .audio] : [.mute, .keypad, .audio, .record]
        }
        return canRecordBeforeConnect ? [.mute, .keypad, .audio, .record] : []
    }

    private var availableOnceConnected: String {
        controller.text("接通后可用", "Available once connected")
    }

    private func auxiliaryControls(oneRow: Bool, compact: Bool = false, width: CGFloat = 0) -> some View {
        let kinds = controlKinds
        // Four controls sit in one row when they fit, otherwise in a balanced 2×2 grid.
        let columns = dynamicTypeSize.isAccessibilitySize ? 2 : oneRow ? 4 : (kinds.count == 4 ? 2 : 3)
        // Rows are centered, so a short last row (e.g. Record alone) sits under the middle.
        let rows: [[ControlKind]] = stride(from: 0, to: kinds.count, by: columns).map { start in
            Array(kinds[start..<min(start + columns, kinds.count)])
        }
        // At accessibility sizes two wide cells keep labels on one line instead of hyphenating.
        let cellWidth: CGFloat = dynamicTypeSize.isAccessibilitySize
            ? max(128, min(180, (width - ModemDeckSpacing.md) / 2)) : 84
        return VStack(spacing: compact ? ModemDeckSpacing.sm : ModemDeckSpacing.lg) {
            ForEach(rows.indices, id: \.self) { index in
                HStack(alignment: .top, spacing: ModemDeckSpacing.md) {
                    ForEach(rows[index], id: \.self) { kind in
                        control(kind).frame(width: cellWidth)
                    }
                }
            }
        }
    }

    private var onValue: String { controller.text("已开启", "On") }
    private var offValue: String { controller.text("已关闭", "Off") }

    @ViewBuilder private func control(_ kind: ControlKind) -> some View {
        switch kind {
        case .mute:
            ModemDeckCallControl(
                title: controller.text("静音", "Mute"),
                icon: "mic.slash.fill",
                isOn: call.muted,
                value: call.muted ? onValue : offValue,
                busy: callController.muteBusy,
                unavailableHint: isActive ? nil : availableOnceConnected
            ) {
                Task { await callController.setMuted(!call.muted) }
            }
            .accessibilityIdentifier("call-mute")
        case .keypad:
            ModemDeckCallControl(
                title: controller.text("拨号盘", "Keypad"),
                icon: "circle.grid.3x3.fill",
                unavailableHint: !isActive ? availableOnceConnected
                    : canSendDTMF ? nil : controller.text("此线路不支持拨号音", "This line can't send keypad tones")
            ) {
                if reduceMotion { showingKeypad.toggle() }
                else { withAnimation(.easeOut(duration: 0.18)) { showingKeypad.toggle() } }
            }
            .accessibilityIdentifier("call-keypad")
        case .audio:
            audioRouteControl
        case .record:
            ModemDeckCallControl(
                title: isActive ? recordingTitle : controller.text("本次录音", "Record call"),
                icon: callController.recordingEnabled ? "record.circle.fill" : "record.circle",
                isOn: callController.recordingEnabled,
                onGlyph: .mdDanger,
                pulse: callController.recordingStatus == "recording",
                value: callController.recordingEnabled ? onValue : offValue,
                busy: !callController.recordingReady || callController.recordingBusy || callController.busy,
                hint: isActive ? nil : controller.text("仅对本次通话生效", "Applies to this call only")
            ) {
                Task { await callController.toggleRecording() }
            }
            .accessibilityIdentifier("call-recording")
        }
    }

    private var callFooter: some View {
        // The incoming targets sit outside the centered end-call target, so a
        // second tap at Answer's position cannot immediately end the call.
        HStack(alignment: .top, spacing: 84) {
            if isEnded {
                ModemDeckCallPrimaryAction(title: controller.text("完成", "Done"), icon: "xmark", color: .mdAccent) {
                    callController.closeEndedCall()
                }
                .accessibilityIdentifier("call-ended-done")
            } else if isIncomingRinging {
                ModemDeckCallPrimaryAction(
                    title: callController.ending ? controller.text("正在结束…", "Ending…") : controller.text("拒绝", "Decline"), icon: "phone.down.fill", color: .mdCallEnd,
                    busy: callController.ending, disabled: callController.ending || !canReject
                ) { Task { await callController.end() } }
                .accessibilityIdentifier("call-decline")
                ModemDeckCallPrimaryAction(
                    title: controller.text("接听", "Answer"), icon: "phone.fill", color: .mdCallAnswer,
                    disabled: callController.busy || callController.ending || !canAnswer
                ) { Task { await callController.answer() } }
                .accessibilityIdentifier("call-answer")
            } else {
                ModemDeckCallPrimaryAction(
                    title: callController.ending ? controller.text("正在结束…", "Ending…") : controller.text("挂断", "End"), icon: "phone.down.fill", color: .mdCallEnd,
                    busy: callController.ending, disabled: callController.ending || !canHangup
                ) { Task { await callController.end() } }
                .accessibilityIdentifier("call-end")
            }
        }
        .frame(maxWidth: .infinity, minHeight: 104, alignment: .top)
        .overlay(alignment: .topTrailing) {
            if keypadVisible {
                Button {
                    if reduceMotion { showingKeypad = false }
                    else { withAnimation(.easeOut(duration: 0.18)) { showingKeypad = false } }
                } label: {
                    Text(controller.text("隐藏", "Hide"))
                        .font(.body.weight(.medium))
                        .foregroundColor(.mdText)
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .hoverEffect(.highlight)
                .padding(.top, 14)
                .accessibilityLabel(controller.text("隐藏拨号盘", "Hide Keypad"))
                .accessibilityIdentifier("call-keypad-hide")
            }
        }
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
        if let endedCall { return controller.text(endedCall.message.chinese, endedCall.message.english) }
        if callController.ending { return controller.text("正在结束…", "Ending…") }
        if call.mediaState == "reconnecting" { return controller.text("正在恢复音频连接…", "Reconnecting audio…") }
        if call.mediaState == "waiting_for_audio" { return controller.text("等待系统启用音频…", "Waiting for system audio…") }
        if call.state == "ringing-out" { return controller.text("对方振铃中…", "Ringing…") }
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
        case "active": return connectedText
        default: return call.state
        }
    }

    private var countdown: String {
        call.testRemainingMS > 0 ? " · \((call.testRemainingMS + 999) / 1000)s" : ""
    }

    // MARK: Audio route

    private var speakerOn: Bool { audioRoute.output == "speaker" }

    private var routeTitle: String {
        switch audioRoute.output {
        case "speaker": return controller.text("扬声器", "Speaker")
        case "receiver": return "iPhone"
        case "bluetooth", "headphones", "external":
            return audioRoute.currentOutputName.isEmpty ? controller.text("音频", "Audio") : audioRoute.currentOutputName
        default: return controller.text("音频", "Audio")
        }
    }

    private var routeIcon: String {
        switch audioRoute.output {
        case "speaker": return "speaker.wave.2.fill"
        case "receiver": return "iphone"
        case "bluetooth", "headphones", "external": return "headphones"
        default: return "speaker.wave.2.fill"
        }
    }

    private var speakerUnavailableHint: String? {
        if !audioRoute.active { return controller.text("通话音频连接后可用", "Available once call audio connects") }
        if !audioRoute.hasReceiver { return controller.text("此设备始终使用扬声器", "This device always uses its speaker") }
        return nil
    }

    private func routeMenuItem(_ title: String, icon: String, selected: Bool) -> some View {
        Label(title, systemImage: selected ? "checkmark" : icon)
    }

    @ViewBuilder private var audioRouteControl: some View {
        if audioRoute.externalInputs.isEmpty {
            ModemDeckCallControl(
                title: controller.text("扬声器", "Speaker"),
                icon: "speaker.wave.2.fill",
                isOn: speakerOn,
                value: speakerOn ? onValue : offValue,
                unavailableHint: speakerUnavailableHint
            ) {
                audioRoute.selectSpeaker(!speakerOn)
            }
            .accessibilityIdentifier("call-speaker")
        } else {
            // Routes still switch through the CallKit-activated session.
            Menu {
                Button { audioRoute.selectSpeaker(true) } label: {
                    routeMenuItem(controller.text("扬声器", "Speaker"), icon: "speaker.wave.2.fill", selected: speakerOn)
                }
                if audioRoute.hasReceiver {
                    Button { audioRoute.selectSpeaker(false) } label: {
                        routeMenuItem(controller.text("iPhone 听筒", "iPhone receiver"), icon: "iphone", selected: audioRoute.output == "receiver")
                    }
                }
                ForEach(audioRoute.externalInputs, id: \.uid) { input in
                    Button { audioRoute.selectInput(input) } label: {
                        routeMenuItem(input.portName, icon: "headphones",
                                      selected: !speakerOn && audioRoute.currentInputUID == input.uid)
                    }
                }
            } label: {
                ModemDeckCallControlFace(title: routeTitle, icon: routeIcon, isOn: speakerOn, unavailable: !audioRoute.active)
            }
            .disabled(!audioRoute.active)
            .accessibilityLabel(controller.text("音频输出", "Audio output"))
            .accessibilityValue(routeTitle)
            .accessibilityHint(audioRoute.active ? "" : controller.text("通话音频连接后可用", "Available once call audio connects"))
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

/// Live call clock; the status is its VoiceOver label so "Connected" is not lost.
private struct ModemDeckCallTimerText: View {
    let start: Date
    let font: Font
    let accessibilityLabel: String
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        TimelineView(.periodic(from: start, by: 1)) { context in
            let clock = modemDeckCallClock(context.date.timeIntervalSince(start))
            Text(clock)
                .font(font)
                .monospacedDigit()
                .foregroundColor(.mdText)
                .contentTransition(reduceMotion ? .identity : .numericText())
                .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: clock)
                .accessibilityLabel(accessibilityLabel)
                .accessibilityValue(clock)
                .accessibilityIdentifier("call-timer")
        }
    }
}

/// 72pt control circle + label. "On" is inverted (text-colored fill), like
/// the system Phone app; unavailable is dashed and faint, not just faded.
private struct ModemDeckCallControlFace: View {
    let title: String
    let icon: String
    var isOn = false
    var onGlyph: Color? = nil
    var pulse = false
    var unavailable = false
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var glyphColor: Color {
        if unavailable { return .mdFaint }
        if isOn { return onGlyph ?? .mdSurface }
        return .mdText
    }

    var body: some View {
        VStack(spacing: ModemDeckSpacing.xs) {
            Image(systemName: icon)
                .font(.system(size: 26, weight: .medium))
                .foregroundColor(glyphColor)
                // A status glyph on the inverted fill resolves in the opposite
                // appearance, so it keeps its contrast against that fill.
                .environment(\.colorScheme, isOn && onGlyph != nil ? (colorScheme == .dark ? .light : .dark) : colorScheme)
                .modemDeckPulse(pulse && !reduceMotion)
                .frame(width: 72, height: 72)
                .background(Circle().fill(isOn ? Color.mdText : Color.mdSurfaceHover))
                .overlay {
                    if unavailable {
                        Circle().strokeBorder(Color.mdBorder, style: StrokeStyle(lineWidth: 1.5, dash: [4, 3]))
                    }
                }
                .contentShape(.hoverEffect, Circle())
                .hoverEffect(.highlight)
            Text(title)
                .font(.footnote)
                .foregroundColor(unavailable ? .mdFaint : .mdText)
                .multilineTextAlignment(.center)
                .lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(minWidth: 72)
        .contentShape(Rectangle())
    }
}

private struct ModemDeckCallControl: View {
    let title: String
    let icon: String
    var isOn = false
    var onGlyph: Color? = nil
    var pulse = false
    var value = ""
    /// Temporarily disabled while a change applies.
    var busy = false
    /// Present when the action is not supported right now; read as a hint.
    var unavailableHint: String? = nil
    var hint: String? = nil
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            ModemDeckCallControlFace(
                title: title, icon: icon, isOn: isOn, onGlyph: onGlyph,
                pulse: pulse, unavailable: unavailableHint != nil
            )
        }
        .buttonStyle(ModemDeckCallFooterStyle())
        .disabled(busy || unavailableHint != nil)
        .accessibilityAddTraits(isOn ? .isSelected : [])
        .accessibilityValue(value)
        .accessibilityHint(unavailableHint ?? hint ?? "")
        .modemDeckSelectionFeedback(isOn)
    }
}

private struct ModemDeckCallFooterStyle: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed && !reduceMotion ? 0.94 : 1)
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
    var busy = false
    var disabled = false
    let action: () -> Void
    @State private var taps = 0

    var body: some View {
        Button {
            taps += 1
            action()
        } label: {
            VStack(spacing: ModemDeckSpacing.xs) {
                Group {
                    if busy { ProgressView().tint(.white) }
                    else { Image(systemName: icon) }
                }
                    .font(.system(size: 28, weight: .semibold))
                    .foregroundColor(.white)
                    .frame(width: 72, height: 72)
                    .background(Circle().fill(color))
                    .contentShape(.hoverEffect, Circle())
                    .hoverEffect(.lift)
                Text(title).font(.footnote.weight(.medium)).foregroundColor(.mdMuted)
                    // The 80pt column keeps Answer clear of End; cap growth so labels stay words.
                    .dynamicTypeSize(...DynamicTypeSize.accessibility1)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(width: 80)
            .frame(minHeight: 104, alignment: .top)
            .contentShape(Rectangle())
        }
        .buttonStyle(ModemDeckCallFooterStyle())
        .disabled(disabled)
        .opacity(disabled && !busy ? 0.5 : 1)
        .modemDeckImpactFeedback(taps)
    }
}

private struct ModemDeckInCallKeypad: View {
    var keySize: CGFloat = 76
    let pressedDigits: String
    let pressedLabel: String
    let action: (String) -> Void
    private let keys = [
        ("1", ""), ("2", "ABC"), ("3", "DEF"),
        ("4", "GHI"), ("5", "JKL"), ("6", "MNO"),
        ("7", "PQRS"), ("8", "TUV"), ("9", "WXYZ"),
        ("*", ""), ("0", "+"), ("#", "")
    ]

    var body: some View {
        VStack(spacing: ModemDeckSpacing.md) {
            Text(pressedDigits.isEmpty ? " " : pressedDigits)
                .font(.system(size: 30, weight: .medium, design: .rounded))
                .monospacedDigit()
                .foregroundColor(.mdText)
                .lineLimit(1)
                .truncationMode(.head)
                .frame(maxWidth: gridWidth, minHeight: 40)
                .accessibilityLabel(pressedLabel)
                .accessibilityValue(pressedDigits)

            LazyVGrid(
                columns: Array(repeating: GridItem(.fixed(keySize), spacing: columnSpacing), count: 3),
                spacing: (keySize * 0.16).rounded()
            ) {
                ForEach(Array(keys.enumerated()), id: \.offset) { _, key in
                    ModemDeckDialKeyButton(digit: key.0, letters: key.1, size: keySize, allowsPlus: false, action: action)
                        .accessibilityIdentifier("call-dtmf-\(key.0)")
                }
            }
            .frame(width: gridWidth)
        }
    }

    private var columnSpacing: CGFloat { (keySize * 0.32).rounded() }
    private var gridWidth: CGFloat { 3 * keySize + 2 * columnSpacing }
}

// iOS 17+ effects. Haptics are not motion; symbol effects are gated by callers on Reduce Motion.
extension View {
    @ViewBuilder func modemDeckSelectionFeedback<Trigger: Equatable>(_ trigger: Trigger) -> some View {
        if #available(iOS 17.0, *) { sensoryFeedback(.selection, trigger: trigger) } else { self }
    }

    @ViewBuilder func modemDeckImpactFeedback<Trigger: Equatable>(_ trigger: Trigger) -> some View {
        if #available(iOS 17.0, *) { sensoryFeedback(.impact(weight: .medium), trigger: trigger) } else { self }
    }

    @ViewBuilder func modemDeckPulse(_ active: Bool) -> some View {
        if #available(iOS 17.0, *) { symbolEffect(.pulse, isActive: active) } else { self }
    }

    @ViewBuilder func modemDeckVariableColor(_ active: Bool) -> some View {
        if #available(iOS 17.0, *) { symbolEffect(.variableColor.iterative, isActive: active) } else { self }
    }

    @ViewBuilder func modemDeckCallBarSurface() -> some View {
        if #available(iOS 26.0, *) {
            glassEffect(.regular, in: RoundedRectangle.modemDeck(ModemDeckRadius.large))
        } else {
            background(.thickMaterial, in: RoundedRectangle.modemDeck(ModemDeckRadius.large))
        }
    }
}

/// Play/pause with elapsed time, a scrubber once audio is loaded, and 15-second skips.
private struct ModemDeckRecordingPlaybackControls: View {
    @ObservedObject var player: ModemDeckRecordingPlayer
    @ObservedObject var controller: ModemDeckSessionController
    let playable: Bool
    let knownDuration: TimeInterval
    @State private var scrubbing: TimeInterval?

    var body: some View {
        // Refresh the clock while playing; a paused player needs no timeline ticks.
        TimelineView(.periodic(from: .now, by: player.playing ? 0.25 : 3_600)) { _ in
            let duration = player.isLoaded ? player.duration : knownDuration
            let elapsed = scrubbing ?? player.currentTime
            VStack(spacing: 10) {
                HStack(spacing: 14) {
                    Button { player.toggle() } label: {
                        ZStack {
                            Circle()
                                .fill(playable ? Color.mdAccent : Color.mdSurfaceHover)
                                .frame(width: 52, height: 52)
                            if player.loading {
                                ProgressView().tint(.mdOnAccent)
                            } else {
                                Image(systemName: player.playing ? "pause.fill" : "play.fill")
                                    .font(.title3.weight(.bold))
                                    .foregroundColor(playable ? .mdOnAccent : .mdFaint)
                                    .offset(x: player.playing ? 0 : 1)
                            }
                        }
                    }
                    .buttonStyle(.plain)
                    .disabled(!playable || player.loading)
                    .accessibilityLabel(player.playing
                        ? controller.text("暂停", "Pause")
                        : controller.text("播放录音", "Play Recording"))

                    VStack(alignment: .leading, spacing: 2) {
                        Text("\(clock(elapsed)) / \(clock(duration))")
                            .font(.headline.monospacedDigit())
                            .foregroundColor(.mdText)
                        Text(player.playing
                             ? controller.text("正在播放", "Playing")
                             : controller.text("录音", "Recording"))
                            .font(.footnote)
                            .foregroundColor(.mdMuted)
                    }
                    Spacer(minLength: 0)
                    skipButton(-15, icon: "gobackward.15", label: controller.text("后退 15 秒", "Back 15 seconds"))
                    skipButton(15, icon: "goforward.15", label: controller.text("前进 15 秒", "Forward 15 seconds"))
                }
                Slider(
                    value: Binding(
                        get: { min(elapsed, max(duration, 0.1)) },
                        set: { scrubbing = $0 }
                    ),
                    in: 0...max(duration, 0.1),
                    onEditingChanged: { editing in
                        if !editing, let target = scrubbing {
                            player.seek(to: target)
                            scrubbing = nil
                        }
                    }
                )
                .tint(.mdAccent)
                .disabled(!player.isLoaded)
                .accessibilityLabel(controller.text("播放进度", "Playback position"))
                .accessibilityValue(clock(elapsed))
            }
            .padding(14)
        }
    }

    private func skipButton(_ seconds: TimeInterval, icon: String, label: String) -> some View {
        Button { player.skip(by: seconds) } label: {
            Image(systemName: icon)
                .font(.title3)
                .foregroundColor(player.isLoaded ? .mdAccent : .mdFaint)
                .frame(width: 44, height: 44)
        }
        .buttonStyle(.plain)
        .disabled(!player.isLoaded)
        .accessibilityLabel(label)
    }

    private func clock(_ seconds: TimeInterval) -> String {
        let total = max(0, Int(seconds.rounded(.down)))
        let hours = total / 3_600, minutes = (total % 3_600) / 60, rest = total % 60
        return hours > 0
            ? String(format: "%d:%02d:%02d", hours, minutes, rest)
            : String(format: "%d:%02d", minutes, rest)
    }
}
