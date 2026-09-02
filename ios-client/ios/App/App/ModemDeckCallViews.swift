import SwiftUI
import UIKit
import AVFAudio

private struct ModemDeckDialerPanelShape: Shape {
    let radius: CGFloat
    let roundsAllCorners: Bool

    func path(in rect: CGRect) -> Path {
        let corners: UIRectCorner = roundsAllCorners ? .allCorners : [.topLeft, .topRight]
        return Path(
            UIBezierPath(
                roundedRect: rect,
                byRoundingCorners: corners,
                cornerRadii: CGSize(width: radius, height: radius)
            ).cgPath
        )
    }
}

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

private struct ModemDeckDialSuggestion: Identifiable {
    let contact: ModemDeckContact
    let phone: ModemDeckContactPhone

    var id: String {
        "\(contact.id):\(phone.id ?? phone.displayNumber)"
    }
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
            .frame(width: size, height: size)
            .background(
                configuration.isPressed
                    ? Color(red: 0.86, green: 0.88, blue: 0.90)
                    : Color(red: 0.945, green: 0.953, blue: 0.961)
            )
            .clipShape(Circle())
            .scaleEffect(configuration.isPressed ? 0.9 : 1)
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
                    .font(.system(size: size < 60 ? 24 : 26, weight: .medium, design: .rounded))
                    .lineLimit(1)
                if !letters.isEmpty {
                    Text(letters)
                        .font(.system(size: digit == "0" ? 14 : 9, weight: .bold))
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

struct ModemDeckDialerPanel: View {
    @ObservedObject var controller: ModemDeckSessionController
    let close: () -> Void
    var floating = false

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text(controller.text("拨号", "Dial"))
                    .font(.system(size: 18, weight: .bold))
                    .foregroundColor(.mdText)
                Spacer()
                Button(action: close) {
                    Image(systemName: "xmark")
                        .font(.system(size: 16, weight: .semibold))
                        .foregroundColor(.mdMuted)
                        .frame(width: 40, height: 40)
                }
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 18)
            .frame(height: 54)
            .overlay(alignment: .bottom) { Rectangle().fill(Color.mdBorder).frame(height: 1) }

            ModemDeckDialerView(controller: controller)
        }
        .frame(maxWidth: floating ? 390 : .infinity, maxHeight: 720)
        .background(Color.mdSurface)
        .clipShape(
            ModemDeckDialerPanelShape(
                radius: floating ? 12 : 8,
                roundsAllCorners: floating
            )
        )
        .overlay(
            ModemDeckDialerPanelShape(
                radius: floating ? 12 : 8,
                roundsAllCorners: floating
            )
            .stroke(Color.mdBorder, lineWidth: floating ? 1 : 0)
        )
        .shadow(color: Color.black.opacity(0.16), radius: 18, y: floating ? 4 : -2)
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
    @StateObject private var contactsStore: ModemDeckContactsStore

    @State private var number = ""
    @State private var selectedLineID = ""
    @State private var recording = false
    @State private var recordingDefault = false
    @State private var recordingReady = false
    @State private var recordingError = ""
    @State private var matchedContactID = ""
    @State private var matchedContactName = ""
    @State private var matchedNumber = ""
    @State private var preferredLineID = ""
    @State private var lineSelectionOverridden = false
    @State private var validationAttempted = false
    @FocusState private var numberFocused: Bool

    private let keys = [
        ("1", ""), ("2", "ABC"), ("3", "DEF"),
        ("4", "GHI"), ("5", "JKL"), ("6", "MNO"),
        ("7", "PQRS"), ("8", "TUV"), ("9", "WXYZ"),
        ("*", ""), ("0", "+"), ("#", "")
    ]

    init(controller: ModemDeckSessionController) {
        self.controller = controller
        _callController = ObservedObject(wrappedValue: controller.callController)
        _contactsStore = StateObject(wrappedValue: controller.contactsStore)
    }

    private var lines: [ModemDeckLine] {
        controller.voiceDialLines
    }

    private var selectedLine: ModemDeckLine? {
        lines.first(where: { $0.id == selectedLineID }) ?? lines.first
    }

    private var dialTarget: ModemDeckDialTarget {
        normalizeModemDeckDialTarget(number)
    }

    private var validationVisible: Bool {
        validationAttempted && dialTarget.error != .none && dialTarget.error != .required
    }

    private var validationMessage: String {
        switch dialTarget.error {
        case .tooLong:
            return controller.text("号码太长", "Number is too long")
        case .invalidCharacter:
            return controller.text("只能使用可拨号字符", "Use dialable characters only")
        case .invalidLength:
            return controller.text("请输入有效号码", "Enter a dialable number")
        default:
            return ""
        }
    }

    private var suggestions: [ModemDeckDialSuggestion] {
        let query = number.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !query.isEmpty else { return [] }
        let digits = query.filter { $0.isNumber }
        var matches: [ModemDeckDialSuggestion] = []
        for contact in contactsStore.contacts {
            let nameMatch = contact.displayName.lowercased().contains(query)
            for phone in contact.phones {
                let phoneDigits = phone.displayNumber.filter { $0.isNumber }
                if nameMatch || (!digits.isEmpty && phoneDigits.contains(digits)) {
                    matches.append(ModemDeckDialSuggestion(contact: contact, phone: phone))
                    if matches.count == 6 { return matches }
                }
            }
        }
        return matches
    }

    private var showsSuggestions: Bool {
        numberFocused && !suggestions.isEmpty && matchedNumber != number
    }

    private var showsContactMatch: Bool {
        !matchedContactName.isEmpty && matchedNumber == number
    }

    private var matchedContact: ModemDeckContact? {
        contactsStore.contacts.first(where: { $0.id == matchedContactID })
    }

    var body: some View {
        GeometryReader { geometry in
            let compact = geometry.size.height < 620
            let keySize: CGFloat = compact ? 56 : 62
            let keySpacing: CGFloat = 9

            VStack(spacing: 0) {
                VStack(alignment: .leading, spacing: 8) {
                    Text(controller.text("通话线路", "Calling line"))
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundColor(.mdMuted)
                    lineSelector
                }
                .padding(.horizontal, 20)
                .padding(.top, compact ? 10 : 16)
                .padding(.bottom, compact ? 8 : 13)
                .overlay(alignment: .bottom) {
                    Rectangle().fill(Color.mdBorder).frame(height: 1)
                }

                numberEntry(compact: compact)
                    .zIndex(10)

                Spacer(minLength: compact ? 0 : 8)

                LazyVGrid(
                    columns: Array(repeating: GridItem(.fixed(62), spacing: 26), count: 3),
                    spacing: keySpacing
                ) {
                    ForEach(Array(keys.enumerated()), id: \.offset) { _, key in
                        ModemDeckDialKeyButton(
                            digit: key.0,
                            letters: key.1,
                            size: keySize,
                            action: appendDigit
                        )
                    }
                }
                .frame(width: 238)

                Spacer(minLength: compact ? 0 : 8)

                if !recordingError.isEmpty {
                    Text(recordingError)
                        .font(.system(size: 11))
                        .foregroundColor(.mdDanger)
                        .lineLimit(2)
                        .padding(.horizontal, 20)
                        .padding(.bottom, 4)
                }

                actionBar(compact: compact)

                ModemDeckInlineError(message: callController.errorMessage)
                    .padding(.horizontal, 16)
                    .padding(.bottom, callController.errorMessage.isEmpty ? 0 : 8)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(Color.mdSurface)
        .onAppear { chooseDefaultLine() }
        .onChange(of: lines.map(\.id)) { _ in chooseDefaultLine() }
        .task {
            await contactsStore.load()
            await loadRecordingPreference()
        }
    }

    private func numberEntry(compact: Bool) -> some View {
        let fieldHeight: CGFloat = compact ? 58 : 68
        return VStack(spacing: 0) {
            numberField(height: fieldHeight)
            if showsContactMatch {
                HStack(spacing: 8) {
                    ModemDeckAvatar(
                        name: matchedContactName,
                        avatarSource: matchedContact?.avatar,
                        size: 28
                    )
                    VStack(alignment: .leading, spacing: 1) {
                        Text(controller.text("匹配联系人", "Matched contact"))
                            .font(.system(size: 9))
                            .foregroundColor(.mdMuted)
                        Text(matchedContactName)
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundColor(.mdText)
                            .lineLimit(1)
                    }
                }
                .padding(.leading, 6)
                .padding(.trailing, 11)
                .frame(height: 36)
                .background(Color.mdAccentSoft)
                .clipShape(Capsule())
                .overlay(Capsule().stroke(Color.mdAccent.opacity(0.12), lineWidth: 1))
                .padding(.top, 6)
                .transition(.opacity.combined(with: .scale(scale: 0.96)))
            }
        }
        .frame(height: fieldHeight + (showsContactMatch ? 42 : 0), alignment: .top)
        .overlay(alignment: .top) {
            if showsSuggestions {
                suggestionMenu
                    .offset(y: fieldHeight - 1)
                    .transition(.opacity.combined(with: .move(edge: .top)))
                    .zIndex(20)
            }
        }
    }

    private func numberField(height: CGFloat) -> some View {
        ZStack {
            TextField(
                "",
                text: $number,
                prompt: Text(controller.text("输入号码或搜索联系人", "Enter a number or search contacts"))
                    .font(.system(size: 14, weight: .medium))
                    .foregroundColor(.mdMuted)
            )
            .keyboardType(.namePhonePad)
            .font(.system(size: 24, weight: .semibold))
            .monospacedDigit()
            .foregroundColor(.mdText)
            .multilineTextAlignment(.center)
            .textContentType(.telephoneNumber)
            .padding(.horizontal, 58)
            .focused($numberFocused)
            .onChange(of: number) { value in
                validationAttempted = false
                if value != matchedNumber {
                    matchedContactID = ""
                    matchedContactName = ""
                    matchedNumber = ""
                    preferredLineID = ""
                    if !lineSelectionOverridden { chooseDefaultLine(force: true) }
                }
            }
            .onSubmit { startCall() }

            if !number.isEmpty {
                HStack(spacing: 2) {
                    Spacer()
                    if validationVisible {
                        Text(controller.text("无效", "Invalid"))
                            .font(.system(size: 10, weight: .bold))
                            .foregroundColor(.mdDanger)
                            .lineLimit(1)
                    }
                    Button(action: removeDigit) {
                        Image(systemName: "delete.left")
                            .font(.system(size: 18))
                            .foregroundColor(.mdMuted)
                            .frame(width: 42, height: 42)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(controller.text("删除一位", "Delete digit"))
                }
                .padding(.trailing, 7)
            }
        }
        .frame(height: height)
        .background(numberFocused ? Color.mdAccent.opacity(0.04) : Color.mdSurface)
        .overlay(alignment: .bottom) {
            Rectangle()
                .fill(validationVisible ? Color.mdDanger : (numberFocused ? Color.mdAccent : Color.mdBorder))
                .frame(height: 2)
        }
        .overlay(alignment: .bottomLeading) {
            if validationVisible {
                Text(validationMessage)
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundColor(.mdDanger)
                    .lineLimit(1)
                    .padding(.leading, 12)
                    .padding(.bottom, 4)
            }
        }
    }

    private var suggestionMenu: some View {
        VStack(spacing: 0) {
            ForEach(suggestions) { suggestion in
                Button {
                    chooseSuggestion(suggestion)
                } label: {
                    HStack(spacing: 10) {
                        ModemDeckAvatar(
                            name: suggestion.contact.displayName,
                            avatarSource: suggestion.contact.avatar,
                            size: 32
                        )
                        VStack(alignment: .leading, spacing: 2) {
                            Text(suggestion.contact.displayName)
                                .font(.system(size: 13, weight: .semibold))
                                .foregroundColor(.mdText)
                                .lineLimit(1)
                            Text("\(suggestion.phone.label) · \(suggestion.phone.displayNumber)")
                                .font(.system(size: 11))
                                .foregroundColor(.mdMuted)
                                .lineLimit(1)
                        }
                        Spacer()
                        if suggestion.phone.primary {
                            Text(controller.text("主号码", "Primary"))
                                .font(.system(size: 9, weight: .bold))
                                .foregroundColor(.mdAccentStrong)
                        }
                    }
                    .padding(.horizontal, 12)
                    .frame(minHeight: 52)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                if suggestion.id != suggestions.last?.id {
                    Rectangle().fill(Color.mdBorder).frame(height: 1)
                }
            }
        }
        .frame(maxWidth: 350)
        .background(Color.mdSurface)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(Color.mdBorder, lineWidth: 1)
        )
        .shadow(color: Color.black.opacity(0.14), radius: 14, y: 5)
        .padding(.horizontal, 20)
    }

    private func actionBar(compact: Bool) -> some View {
        let secondarySize: CGFloat = compact ? 48 : 52
        let primarySize: CGFloat = compact ? 56 : 62
        return HStack(alignment: .top, spacing: 26) {
            Button { recording.toggle() } label: {
                VStack(spacing: 5) {
                    Image(systemName: "circle.fill")
                        .font(.system(size: 19, weight: .semibold))
                        .foregroundColor(recording ? .white : .mdText)
                        .frame(width: secondarySize, height: secondarySize)
                        .background(recording ? Color.mdDanger : Color(red: 0.945, green: 0.953, blue: 0.961))
                        .clipShape(Circle())
                        .overlay(Circle().stroke(recording ? Color.mdDanger : Color.mdBorder, lineWidth: 1))
                    Text(controller.text("录音", "Record"))
                        .font(.system(size: 11, weight: .medium))
                        .foregroundColor(recording ? .mdDanger : .mdMuted)
                }
                .frame(width: 62)
                .frame(minHeight: 80)
            }
            .buttonStyle(.plain)
            .disabled(!recordingReady)
            .opacity(recordingReady ? 1 : 0.5)

            Button { startCall() } label: {
                VStack(spacing: 5) {
                    ZStack {
                        Circle().fill(Color.mdAccent)
                        if callController.busy {
                            ProgressView().tint(.white)
                        } else {
                            Image(systemName: "phone.fill")
                                .font(.system(size: 22, weight: .bold))
                                .foregroundColor(.white)
                        }
                    }
                    .frame(width: primarySize, height: primarySize)
                    Text(controller.text("拨打", "Call"))
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundColor(.mdAccent)
                }
                .frame(width: 62)
                .frame(minHeight: 84)
            }
            .buttonStyle(.plain)
            .disabled(
                number.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                    selectedLine == nil ||
                    callController.busy ||
                    !controller.isOnline
            )
            .opacity(selectedLine == nil || !controller.isOnline ? 0.5 : 1)

            Color.clear.frame(width: 62, height: 84)
        }
        .frame(width: 238)
        .padding(.top, compact ? 8 : 10)
        .frame(maxWidth: .infinity, minHeight: compact ? 96 : 108)
        .background(Color.mdSurface)
        .overlay(alignment: .top) {
            Rectangle().fill(Color.mdBorder).frame(height: 1)
        }
    }

    @ViewBuilder
    private var lineSelector: some View {
        if lines.count > 1 {
            Menu {
                ForEach(lines) { line in
                    Button(line.displayName) {
                        selectedLineID = line.id
                        lineSelectionOverridden = true
                    }
                }
            } label: {
                lineSelectorLabel
            }
        } else {
            lineSelectorLabel
        }
    }

    private var lineSelectorLabel: some View {
        HStack(spacing: 10) {
            Image(systemName: "simcard.fill")
                .font(.system(size: 16, weight: .semibold))
                .foregroundColor(selectedLine?.tint ?? .mdMuted)
                .frame(width: 36, height: 36)
                .background((selectedLine?.tint ?? Color.mdMuted).opacity(0.10))
                .clipShape(Circle())
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(selectedLine?.displayName ?? controller.text("没有可用线路", "No available line"))
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundColor(.mdText)
                    if selectedLine?.id == controller.bootstrap?.lineSettings.defaultLineId {
                        Text(controller.text("默认", "Default"))
                            .font(.system(size: 9, weight: .bold))
                            .foregroundColor(.mdAccentStrong)
                            .padding(.horizontal, 5)
                            .frame(height: 17)
                            .background(Color.mdAccentSoft)
                            .clipShape(Capsule())
                    }
                }
                if let line = selectedLine {
                    Text([line.phoneNumber, line.networkName].filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(.system(size: 11))
                        .foregroundColor(.mdMuted)
                        .lineLimit(1)
                }
            }
            Spacer()
            if lines.count > 1 {
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundColor(.mdFaint)
            }
        }
        .padding(.horizontal, 13)
        .frame(height: 54)
        .background(Color.mdSurface)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(Color.mdBorder, lineWidth: 1)
        )
    }

    private func chooseDefaultLine(force: Bool = false) {
        if !force,
           !selectedLineID.isEmpty,
           lines.contains(where: { $0.id == selectedLineID }) {
            return
        }
        if !lineSelectionOverridden,
           !preferredLineID.isEmpty,
           lines.contains(where: { $0.id == preferredLineID }) {
            selectedLineID = preferredLineID
            return
        }
        let defaultID = controller.bootstrap?.lineSettings.defaultLineId ?? ""
        selectedLineID = lines.contains(where: { $0.id == defaultID }) ? defaultID : (lines.first?.id ?? "")
    }

    private func appendDigit(_ digit: String) {
        number.append(digit)
        matchedContactID = ""
        matchedContactName = ""
        matchedNumber = ""
        preferredLineID = ""
        validationAttempted = false
        if !lineSelectionOverridden { chooseDefaultLine(force: true) }
        UIImpactFeedbackGenerator(style: .light).impactOccurred(intensity: 0.65)
        ModemDeckDTMFTonePlayer.shared.play(digit)
    }

    private func removeDigit() {
        guard !number.isEmpty else { return }
        number.removeLast()
        matchedContactID = ""
        matchedContactName = ""
        matchedNumber = ""
        preferredLineID = ""
        validationAttempted = false
        UISelectionFeedbackGenerator().selectionChanged()
    }

    private func chooseSuggestion(_ suggestion: ModemDeckDialSuggestion) {
        number = suggestion.phone.displayNumber
        matchedContactID = suggestion.contact.id
        matchedContactName = suggestion.contact.displayName
        matchedNumber = suggestion.phone.displayNumber
        preferredLineID = suggestion.contact.preferredLineId ?? ""
        validationAttempted = false
        if !lineSelectionOverridden { chooseDefaultLine(force: true) }
        numberFocused = false
        UISelectionFeedbackGenerator().selectionChanged()
    }

    private func loadRecordingPreference() async {
        do {
            let settings = try await controller.api.recordingSettings()
            recordingDefault = settings.defaultEnabled
            recording = settings.defaultEnabled
            recordingReady = true
            recordingError = ""
        } catch {
            recordingReady = false
            recordingError = error.localizedDescription
        }
    }

    private func startCall() {
        validationAttempted = true
        guard controller.isOnline, let line = selectedLine else { return }
        let target = dialTarget
        guard target.error == .none else { return }
        let displayName = showsContactMatch ? matchedContactName : target.original
        Task {
            await callController.start(
                lineID: line.id,
                number: target.original,
                displayName: displayName,
                recording: recordingReady ? recording : nil
            )
            if callController.errorMessage.isEmpty {
                number = ""
                matchedContactID = ""
                matchedContactName = ""
                matchedNumber = ""
                preferredLineID = ""
                validationAttempted = false
                lineSelectionOverridden = false
                recording = recordingDefault
                chooseDefaultLine(force: true)
            }
        }
    }
}

struct ModemDeckCallsView: View {
    @ObservedObject var controller: ModemDeckSessionController
    @StateObject private var store: ModemDeckCallsStore
    @Environment(\.modemDeckUsesSplitWorkspace) private var usesSplitWorkspace
    @Environment(\.modemDeckNavigate) private var navigate
    @State private var query = ""
    @State private var statusFilter = "all"
    @State private var lineFilter = ""
    @State private var favoriteOnly = false
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
        return store.calls.filter { call in
            let matchesStatus = statusFilter == "all" ||
                (statusFilter == "missed" && call.missed) ||
                (statusFilter == "incoming" && call.direction == "incoming" && !call.missed) ||
                (statusFilter == "outgoing" && call.direction == "outgoing")
            let matchesQuery = normalized.isEmpty ||
                displayName(for: call).localizedCaseInsensitiveContains(normalized) ||
                call.remoteNumber.localizedCaseInsensitiveContains(normalized)
            return matchesStatus && matchesQuery &&
                (lineFilter.isEmpty || call.lineId == lineFilter) &&
                (!favoriteOnly || call.favorite)
        }
    }

    private var recordedCallIDs: Set<String> {
        Set(store.recordings.filter(\.playable).map { $0.call.id })
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
            guard selecting else { return }
            selectedIDs.formIntersection(Set(visibleIDs))
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
            ModemDeckPageHeader(title: controller.text("通话", "Calls"))
            toolbar
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

    private var toolbar: some View {
        HStack(spacing: ModemDeckLayout.toolbarGap) {
            ModemDeckToolbarButton(
                icon: selecting ? "xmark" : "checklist",
                active: selecting,
                accessibilityText: controller.text("选择通话", "Select calls")
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
                    .init(id: "missed", title: controller.text("未接", "Missed")),
                    .init(id: "incoming", title: controller.text("呼入", "Incoming")),
                    .init(id: "outgoing", title: controller.text("呼出", "Outgoing"))
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
        HStack(alignment: .center, spacing: 11) {
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

    init(controller: ModemDeckSessionController) {
        self.controller = controller
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
            ModemDeckPageHeader(title: controller.text("录音", "Recordings"))
            HStack(spacing: ModemDeckLayout.toolbarGap) {
                ModemDeckToolbarButton(
                    icon: selecting ? "xmark" : "checklist",
                    active: selecting,
                    accessibilityText: controller.text("选择录音", "Select recordings")
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
                ModemDeckToolbarButton(
                    icon: favoriteOnly ? "star.fill" : "star",
                    active: favoriteOnly,
                    accessibilityText: controller.text("仅收藏", "Favorites only")
                ) { favoriteOnly.toggle() }
            }
            .modemDeckListToolbar(showsDivider: true)

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
        HStack(alignment: .center, spacing: 11) {
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
            .foregroundColor(prominent ? .white : .mdAccent)
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
                                        ProgressView().tint(.white)
                                    } else {
                                        Image(systemName: player.playing ? "pause.fill" : "play.fill")
                                            .font(.system(size: 19, weight: .bold))
                                            .foregroundColor(recording.playable ? .white : .mdFaint)
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

struct ModemDeckActiveCallView: View {
    let call: ModemDeckPresentedCall
    @ObservedObject var controller: ModemDeckSessionController
    @ObservedObject var callController: ModemDeckCallController

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
                        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                        .overlay(
                            RoundedRectangle(cornerRadius: 14, style: .continuous)
                                .stroke(Color.white.opacity(0.10), lineWidth: 1)
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

    private var callBackground: some View {
        LinearGradient(
            colors: [Color(red: 0.04, green: 0.16, blue: 0.18), Color.black],
            startPoint: .top,
            endPoint: .bottom
        )
    }

    private var callStage: some View {
        VStack(spacing: 0) {
                Spacer(minLength: showingKeypad ? 24 : 48)
                if !showingKeypad {
                    Image(systemName: call.testCall ? "checkmark.shield.fill" : "person.crop.circle.fill")
                        .font(.system(size: 92))
                        .foregroundColor(.white.opacity(0.92))
                }
                Text(call.displayName.isEmpty ? call.remoteNumber : call.displayName)
                    .font(.system(size: showingKeypad ? 20 : 32, weight: .semibold))
                    .foregroundColor(.white)
                    .multilineTextAlignment(.center)
                    .padding(.top, showingKeypad ? 0 : 22)
                if !showingKeypad,
                   call.displayName != call.remoteNumber,
                   !call.remoteNumber.isEmpty {
                    Text(call.remoteNumber)
                        .font(.title3)
                        .foregroundColor(.white.opacity(0.68))
                        .padding(.top, 5)
                }
                Text(stateText)
                    .font(.subheadline.weight(.medium))
                    .foregroundColor(.white.opacity(0.72))
                    .padding(.top, 10)

                Spacer()

                if showingKeypad && isActive && canSendDTMF {
                    ModemDeckInCallKeypad(
                        pressedDigits: dtmfDigits,
                        action: sendDTMF
                    )
                    .transition(.opacity.combined(with: .move(edge: .bottom)))
                } else if isActive {
                    ModemDeckCallControl(
                        title: call.muted
                            ? controller.text("取消静音", "Unmute")
                            : controller.text("静音", "Mute"),
                        icon: call.muted ? "mic.slash.fill" : "mic.fill",
                        selected: call.muted,
                        disabled: callController.muteBusy
                    ) {
                        Task { await callController.setMuted(!call.muted) }
                    }
                }

                ModemDeckInlineError(message: callController.errorMessage)
                    .foregroundColor(.white)
                    .padding(.horizontal)

                callFooter
                    .padding(.top, 34)
                    .padding(.bottom, 46)
            }
            .frame(maxWidth: 640)
            .padding(.horizontal, 24)
    }

    @ViewBuilder
    private var callFooter: some View {
        HStack(alignment: .top, spacing: 26) {
            if !call.testCall {
                ModemDeckCallFooterAction(
                    title: recordingTitle,
                    icon: callController.recordingEnabled ? "stop.fill" : "circle.fill",
                    color: callController.recordingEnabled ? .red : .white.opacity(0.16),
                    selected: callController.recordingEnabled,
                    disabled: !callController.recordingReady ||
                        callController.recordingBusy ||
                        callController.busy
                ) {
                    Task { await callController.toggleRecording() }
                }
            } else {
                Color.clear.frame(width: 62, height: 80)
            }

            if isRinging && call.direction == "incoming" {
                ModemDeckCallFooterAction(
                    title: controller.text("拒绝", "Decline"),
                    icon: "phone.down.fill",
                    color: .red,
                    primary: true,
                    disabled: callController.busy || !canReject
                ) {
                    Task { await callController.end() }
                }
                ModemDeckCallFooterAction(
                    title: controller.text("接听", "Answer"),
                    icon: "phone.fill",
                    color: .green,
                    primary: true,
                    disabled: callController.busy || !canAnswer
                ) {
                    Task { await callController.answer() }
                }
            } else {
                ModemDeckCallFooterAction(
                    title: controller.text("挂断", "End"),
                    icon: "phone.down.fill",
                    color: .red,
                    primary: true,
                    disabled: callController.busy || !canHangup
                ) {
                    Task { await callController.end() }
                }
                if isActive && !call.testCall {
                    ModemDeckCallFooterAction(
                        title: showingKeypad
                            ? controller.text("隐藏键盘", "Hide Keypad")
                            : controller.text("键盘", "Keypad"),
                        icon: "circle.grid.3x3.fill",
                        color: showingKeypad ? .mdAccent : .white.opacity(0.16),
                        selected: showingKeypad,
                        disabled: !canSendDTMF
                    ) {
                        if reduceMotion {
                            showingKeypad.toggle()
                        } else {
                            withAnimation(.easeOut(duration: 0.18)) {
                                showingKeypad.toggle()
                            }
                        }
                    }
                } else {
                    Color.clear.frame(width: 62, height: 80)
                }
            }
        }
        .frame(width: 238)
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
        if call.testCall {
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
                    .foregroundColor(.white)
                    .frame(width: 62, height: 62)
                    .background(selected ? selectedColor : Color.white.opacity(0.16))
                    .clipShape(Circle())
                Text(title)
                    .font(.caption)
                    .foregroundColor(.white)
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

private struct ModemDeckCallFooterAction: View {
    let title: String
    let icon: String
    let color: Color
    var selected = false
    var primary = false
    var disabled = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: 6) {
                Image(systemName: icon)
                    .font(.system(size: primary ? 23 : 20, weight: .bold))
                    .foregroundColor(.white)
                    .frame(width: primary ? 62 : 48, height: primary ? 62 : 48)
                    .background(color)
                    .clipShape(Circle())
                Text(title)
                    .font(.system(size: 11, weight: .medium))
                    .foregroundColor(selected ? color : .white.opacity(0.74))
                    .lineLimit(1)
            }
            .frame(width: 62)
            .frame(minHeight: 80, alignment: .top)
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
                .foregroundColor(.white)
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
                        .foregroundColor(.white)
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
                    ? Color.white.opacity(0.28)
                    : Color.white.opacity(0.15)
            )
            .clipShape(Circle())
            .scaleEffect(configuration.isPressed ? 0.9 : 1)
            .animation(
                reduceMotion ? nil : .easeOut(duration: 0.08),
                value: configuration.isPressed
            )
    }
}
