import UIKit
import SwiftUI

class SceneDelegate: UIResponder, UIWindowSceneDelegate {
    var window: UIWindow?
    private var sessionController: ModemDeckSessionController?
    private var privacyShield: UIView?

    func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options connectionOptions: UIScene.ConnectionOptions) {
        guard let windowScene = scene as? UIWindowScene else { return }

        let credentialStore = (UIApplication.shared.delegate as? AppDelegate)?.credentialStore ??
            ModemDeckCredentialStore()
        let controller = ModemDeckSessionController(credentialStore: credentialStore)
        sessionController = controller

        let window = UIWindow(windowScene: windowScene)
        window.tintColor = UIColor(Color.modemDeckTint)
        window.rootViewController = UIHostingController(
            rootView: ModemDeckRootView(controller: controller)
        )
        self.window = window
        window.makeKeyAndVisible()

        if let response = connectionOptions.notificationResponse {
            ModemDeckPushCoordinator.shared.handleNotificationResponse(
                response.notification.request.content.userInfo
            )
        }
    }

    func sceneWillResignActive(_ scene: UIScene) {
        guard let window, privacyShield == nil else { return }
        let shield = UIView(frame: window.bounds)
        shield.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        shield.backgroundColor = .systemBackground

        let image = UIImageView(image: UIImage(systemName: "antenna.radiowaves.left.and.right"))
        image.translatesAutoresizingMaskIntoConstraints = false
        image.tintColor = window.tintColor
        image.contentMode = .scaleAspectFit
        shield.addSubview(image)
        NSLayoutConstraint.activate([
            image.centerXAnchor.constraint(equalTo: shield.centerXAnchor),
            image.centerYAnchor.constraint(equalTo: shield.centerYAnchor),
            image.widthAnchor.constraint(equalToConstant: 52),
            image.heightAnchor.constraint(equalToConstant: 52)
        ])

        window.addSubview(shield)
        privacyShield = shield
    }

    func sceneDidBecomeActive(_ scene: UIScene) {
        privacyShield?.removeFromSuperview()
        privacyShield = nil
    }
}
