import AVFoundation
import SwiftUI
import UIKit

struct ModemDeckPairingView: View {
    @ObservedObject var session: ModemDeckSessionController

    @State private var showingScanner = false
    @State private var showingManualEntry = false
    @State private var manualPayload = ""
    @State private var cameraError = ""

    private var chinese: Bool {
        Locale.preferredLanguages.first?.hasPrefix("zh") == true
    }

    var body: some View {
        NavigationView {
            ScrollView {
                VStack(spacing: 28) {
                    Spacer(minLength: 24)

                    ZStack {
                        RoundedRectangle(cornerRadius: 28, style: .continuous)
                            .fill(Color.modemDeckTint.opacity(0.12))
                            .frame(width: 108, height: 108)
                        Image(systemName: "qrcode.viewfinder")
                            .font(.system(size: 45, weight: .medium))
                            .foregroundColor(.modemDeckTint)
                    }

                    VStack(spacing: 10) {
                        Text(chinese ? "连接 ModemDeck" : "Connect to ModemDeck")
                            .font(.largeTitle.bold())
                            .multilineTextAlignment(.center)
                        Text(chinese
                            ? "在 Web 设置中打开“配对”，然后扫描二维码。"
                            : "Open Pairing in Web settings, then scan the QR code.")
                            .font(.body)
                            .foregroundColor(.secondary)
                            .multilineTextAlignment(.center)
                    }

                    VStack(spacing: 14) {
                        Button {
                            requestCameraAndOpenScanner()
                        } label: {
                            Label(
                                chinese ? "扫描二维码" : "Scan QR Code",
                                systemImage: "qrcode.viewfinder"
                            )
                            .font(.headline)
                            .frame(maxWidth: .infinity, minHeight: 52)
                        }
                        .buttonStyle(.borderedProminent)
                        .tint(.modemDeckTint)
                        .disabled(session.pairingInProgress)

                        Button(chinese ? "手动粘贴配对信息" : "Paste Pairing Data") {
                            showingManualEntry.toggle()
                        }
                        .buttonStyle(.bordered)
                        .frame(maxWidth: .infinity)

                        if showingManualEntry {
                            VStack(alignment: .leading, spacing: 12) {
                                Text(chinese
                                    ? "粘贴二维码中的完整 JSON 内容"
                                    : "Paste the complete JSON payload from the QR code")
                                    .font(.footnote)
                                    .foregroundColor(.secondary)
                                TextEditor(text: $manualPayload)
                                    .font(.system(.footnote, design: .monospaced))
                                    .frame(minHeight: 130)
                                    .padding(8)
                                    .background(Color(.secondarySystemBackground))
                                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))

                                Button(chinese ? "连接" : "Connect") {
                                    Task { await session.pair(using: manualPayload) }
                                }
                                .buttonStyle(.borderedProminent)
                                .tint(.modemDeckTint)
                                .disabled(
                                    manualPayload.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                                        session.pairingInProgress
                                )
                            }
                            .transition(.opacity.combined(with: .move(edge: .top)))
                        }
                    }

                    if session.pairingInProgress {
                        ProgressView(chinese ? "正在验证配对…" : "Verifying pairing…")
                    }

                    if !session.errorMessage.isEmpty {
                        Label(session.errorMessage, systemImage: "exclamationmark.triangle.fill")
                            .font(.footnote)
                            .foregroundColor(.red)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(14)
                            .background(Color.red.opacity(0.08))
                            .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                    }

                    Spacer(minLength: 24)
                }
                .frame(maxWidth: 520)
                .padding(.horizontal, 24)
                .frame(maxWidth: .infinity)
            }
            .background(Color(.systemGroupedBackground).ignoresSafeArea())
            .navigationBarHidden(true)
        }
        .navigationViewStyle(.stack)
        .fullScreenCover(isPresented: $showingScanner) {
            ModemDeckQRScannerRepresentable { result in
                showingScanner = false
                switch result {
                case .value(let payload):
                    Task { await session.pair(using: payload) }
                case .cancelled:
                    break
                case .failed(let error):
                    cameraError = error.localizedDescription
                }
            }
            .ignoresSafeArea()
        }
        .alert(
            chinese ? "无法使用相机" : "Camera Unavailable",
            isPresented: Binding(
                get: { !cameraError.isEmpty },
                set: { if !$0 { cameraError = "" } }
            )
        ) {
            Button(chinese ? "打开设置" : "Open Settings") {
                guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
                UIApplication.shared.open(url)
            }
            Button(chinese ? "取消" : "Cancel", role: .cancel) {}
        } message: {
            Text(cameraError)
        }
    }

    private func requestCameraAndOpenScanner() {
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized:
            showingScanner = true
        case .notDetermined:
            AVCaptureDevice.requestAccess(for: .video) { granted in
                DispatchQueue.main.async {
                    if granted {
                        showingScanner = true
                    } else {
                        cameraError = chinese
                            ? "请在系统设置中允许 ModemDeck 使用相机。"
                            : "Allow ModemDeck to use the camera in Settings."
                    }
                }
            }
        default:
            cameraError = chinese
                ? "请在系统设置中允许 ModemDeck 使用相机。"
                : "Allow ModemDeck to use the camera in Settings."
        }
    }
}

private struct ModemDeckQRScannerRepresentable: UIViewControllerRepresentable {
    let completion: (ModemDeckQRScanResult) -> Void

    func makeUIViewController(context: Context) -> ModemDeckQRScannerViewController {
        let controller = ModemDeckQRScannerViewController()
        controller.completion = completion
        return controller
    }

    func updateUIViewController(
        _ uiViewController: ModemDeckQRScannerViewController,
        context: Context
    ) {}
}
