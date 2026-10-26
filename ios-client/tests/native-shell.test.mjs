import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = new URL('../', import.meta.url)
const rootPath = fileURLToPath(root)

async function source(path) {
  return readFile(new URL(path, root), 'utf8')
}

test('native app keeps a shared Release archive scheme separate from simulator UAT', async () => {
  const [app, uat] = await Promise.all([
    source('ios/App/App.xcodeproj/xcshareddata/xcschemes/App.xcscheme'),
    source('ios/App/App.xcodeproj/xcshareddata/xcschemes/App-UAT.xcscheme')
  ])
  assert.match(app, /BuildableName="App\.app" BlueprintName="App"/)
  assert.match(app, /buildForArchiving="YES"/)
  assert.match(app, /ArchiveAction buildConfiguration="Release"/)
  assert.match(app, /LaunchAction buildConfiguration="Debug"/)
  assert.doesNotMatch(app, /AppUITests|MODEMDECK_UAT/)
  assert.match(uat, /TestAction buildConfiguration="Debug"/)
  assert.match(uat, /BuildableName="AppUITests\.xctest"/)
  assert.doesNotMatch(uat, /buildForArchiving="YES"/)
})

test('native iOS registers APNs and PushKit tokens after pairing', async () => {
  const [delegate, native, entitlements, info, debugConfig, releaseConfig] = await Promise.all([
    source('ios/App/App/AppDelegate.swift'),
    source('ios/App/App/ModemDeckNative.swift'),
    source('ios/App/App/App.entitlements'),
    source('ios/App/App/Info.plist'),
    source('ios/debug.xcconfig'),
    source('ios/release.xcconfig')
  ])

  assert.match(delegate, /let credentialStore = ModemDeckCredentialStore\(\)/)
  assert.match(delegate, /ModemDeckPushCoordinator\.shared\.configure\(store: credentialStore\)/)
  assert.match(delegate, /didRegisterForRemoteNotificationsWithDeviceToken/)
  assert.match(native, /PKPushRegistryDelegate/)
  assert.match(native, /desiredPushTypes = \[\.voIP\]/)
  assert.match(native, /\/api\/v1\/mobile\/push/)
  assert.match(native, /CXProvider/)
  assert.match(native, /includesCallsInRecents = true/)
  assert.match(native, /maximumCallGroups = 1/)
  assert.match(native, /func configure\(store:[\s\S]*Thread\.isMainThread/)
  assert.match(native, /testCallUUIDs\.contains\(uuid\)/)
  assert.match(
    native,
    /outgoingCallUUIDs\.contains\(uuid\)[\s\S]*answeredCallUUIDs\.contains\(uuid\)[\s\S]*answerRequestedCallUUIDs\.contains\(uuid\)/
  )
  assert.match(entitlements, /<key>aps-environment<\/key>/)
  assert.match(entitlements, /\$\(MODEMDECK_APNS_ENVIRONMENT\)/)
  assert.match(info, /<key>ModemDeckAPNSEnvironment<\/key>/)
  assert.match(info, /<key>ITSAppUsesNonExemptEncryption<\/key>\s*<false\/>/)
  assert.match(debugConfig, /MODEMDECK_APNS_ENVIRONMENT = development/)
  assert.match(releaseConfig, /MODEMDECK_APNS_ENVIRONMENT = production/)
  assert.match(info, /<string>remote-notification<\/string>/)
  assert.match(info, /<string>voip<\/string>/)
  assert.match(info, /<string>audio<\/string>/)
})

test('CallKit owns native WSS Opus audio and keeps synthetic calls local', async () => {
  const [native, audio, session, project] = await Promise.all([
    source('ios/App/App/ModemDeckNative.swift'),
    source('ios/App/App/ModemDeckCallAudio.swift'),
    source('ios/App/App/ModemDeckSession.swift'),
    source('ios/App/App.xcodeproj/project.pbxproj')
  ])

  assert.match(project, /MDNetEq.xcframework in Frameworks/)
  assert.doesNotMatch(project, /Capacitor|CapApp-SPM|Cordova|Copus|swift-opus/)
  assert.match(audio, /import ModemDeckAudioCore/)
  assert.doesNotMatch(audio, /import WebRTC|RTCPeerConnection/)
  assert.match(audio, /func didActivate/)
  assert.match(audio, /guard !stopped, activated, ready, engine == nil/)
  assert.match(audio, /setVoiceProcessingEnabled\(true\)/)
  assert.match(audio, /isVoiceProcessingAGCEnabled = true/)
  assert.match(audio, /opus_encode_float/)
  assert.match(audio, /md_neteq_render_float/)
  assert.match(audio, /AVAudioSourceNode/)
  assert.match(audio, /callPath\("media\/ws"\)/)
  assert.match(audio, /components\.scheme = "wss"/)
  assert.match(audio, /callPath\("media"\)/)
  assert.match(audio, /callPath\("lease"\)/)
  // Queue capacity and sample-age deadlines run against the product methods in state-regression.
  assert.match(audio, /reconnectAttempts < 4/)
  assert.match(audio, /timeoutIntervalForResource = 24 \* 60 \* 60/)
  assert.match(audio, /func start\(audioSession: AVAudioSession\)/)
  assert.match(audio, /overrideOutputAudioPort\(\.speaker\)/)
  assert.ok(audio.indexOf('player.scheduleBuffer(buffer, at: nil, options: .loops)') < audio.lastIndexOf('try engine.start()'))
  assert.match(native, /audioSession\.connect/)
  assert.match(native, /sendEndCallAction\(verb: "hangup"/)
  assert.match(native, /sendCallAction\([\s\S]*verb: "dtmf"/)
  assert.match(native, /CXStartCallAction/)
  assert.match(native, /reportOutgoingCall/)
  assert.match(native, /startOutgoingCall/)
  const coordinatorInitializer = native.match(
    /private override init\(\) \{([\s\S]*?)\n    \}/
  )?.[1] || ''
  assert.doesNotMatch(coordinatorInitializer, /prepareAudioSession/)
  assert.match(
    native,
    /requestMicrophoneAccess[\s\S]*guard let self[\s\S]*guard granted[\s\S]*prepareAudioSession\(\)[\s\S]*performAnswer/
  )
  assert.match(native, /if testCallUUIDs\.contains\(uuid\)[\s\S]*action\.fulfill\(\)/)
  assert.match(native, /scheduleTestCallTimeout/)
  assert.match(native, /private lazy var testCallTone = ModemDeckTestCallTone\(\)/)
  assert.match(native, /setCurrentCallMuted/)
  assert.match(native, /PresentedCall/)
  assert.match(native, /markCallActive/)
  assert.match(session, /ModemDeckPushCoordinator\.shared\.observeCallState\(self\)/)
  assert.match(session, /func callStateDidChange\(_ state: \[String: Any\]\)/)
  assert.match(native, /callStateObserver\?\.callStateDidChange\(currentCallStatePayload\(\)\)/)
})

test('native call control preserves server defaults and serializes completed DTMF actions', async () => {
  const [delegate, app, api, session, calls, native] = await Promise.all([
    source('ios/App/App/AppDelegate.swift'),
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckAPI.swift'),
    source('ios/App/App/ModemDeckSession.swift'),
    source('ios/App/App/ModemDeckCallViews.swift'),
    source('ios/App/App/ModemDeckNative.swift')
  ])

  assert.match(delegate, /configure\(store: credentialStore\)/)
  assert.match(app, /\.accessibilityHidden\(callController\.visibleCall != nil && callExpanded\)/)
  assert.match(app, /\.accessibilityAddTraits\(callExpanded \? \.isModal : \[\]\)/)
  assert.match(api, /recordingEnabled: Bool\?/)
  assert.match(api, /func callRecording\(callID:/)
  assert.match(session, /func enqueueDTMF\(_ digits: String\) -> Bool/)
  assert.match(session, /private var dtmfQueue: \[DTMFRequest\]/)
  assert.match(session, /drainDTMFQueue\(\)/)
  assert.match(session, /recordingReady \? recording : nil|recording: Bool\?/)
  assert.match(session, /capabilities\?\.dial == true && \$0\.capabilities\?\.media == true/)
  assert.match(calls, /\(48\.\.\.57\)\.contains\(scalar\.value\)/)
  assert.match(calls, /field\.inputView = UIView\(frame: \.zero\)/)
  assert.match(calls, /guard callController\.enqueueDTMF\(digit\) else \{ return \}/)
  assert.match(calls, /if call\.testCall \{ return true \}/)
  assert.match(calls, /!call\.testCall &&[\s\S]*canSendDTMF/)
  assert.match(native, /private var requestedCallActions:/)
  assert.match(native, /finishProviderCallAction\(action, result: \.success\(\(\)\)\)/)
  assert.match(native, /payload\["recording_enabled"\] = recordingEnabled/)
  assert.match(native, /preferredRecordingByUUID\[uuid\]/)
  assert.doesNotMatch(session, /func sendDTMF\(_ digits: String\) async/)
})

test('terminal VoIP pushes close the same CallKit UUID without reviving ended calls', async () => {
  const native = await source('ios/App/App/ModemDeckNative.swift')

  assert.match(native, /didReceiveIncomingVoIPPushWith payload: PKPushPayload/)
  assert.match(native, /mustReport: metadata\.mustReport/)
  assert.match(native, /payload\["modemdeck_call_end"\]/)
  assert.match(native, /let uuid = UUID\(uuidString: uuidText\)/)
  assert.doesNotMatch(native, /UUID\(uuidString:[^\n]+\) \?\? UUID\(\)/)
  assert.match(native, /callController\.callObserver\.calls\.contains/)
  assert.match(native, /reportCallEnded\(with: uuid, endedAt: Date\(\), reason: reason\)/)
  assert.match(
    native,
    /guard mustReport else[\s\S]*reportNewIncomingCall\(with: uuid, update: update\)[\s\S]*reportCall/
  )
  assert.match(native, /private static let maximumRememberedEndedCalls = 64/)
  assert.match(native, /endedCallUUIDs\.contains\(uuid\)/)
  assert.match(native, /private var providerCallActions:/)
  assert.match(native, /settleProviderCallActions\(for: uuid\)/)
})

test('every VoIP push that must be reported reaches CallKit', async () => {
  const native = await source('ios/App/App/ModemDeckNative.swift')
  const body = name => {
    const start = native.indexOf(`private func ${name}(`)
    assert.ok(start >= 0, `${name} is missing`)
    const end = native.indexOf('\n    private func ', start + 1)
    return native.slice(start, end)
  }

  // Until a new incoming call claims its UUID, no path may finish the push
  // without reporting: PushKit kills the app and later stops VoIP delivery.
  const incoming = body('reportIncomingCall')
  const beforeClaim = incoming.slice(0, incoming.indexOf('callIDsByUUID[uuid] = callID'))
  assert.doesNotMatch(beforeClaim, /\bcompletion\(\)/)
  assert.equal(beforeClaim.match(/reportUnpresentedPush\(/g)?.length, 2)

  const terminal = body('reportTerminalCall')
  const beforeReport = terminal.slice(0, terminal.indexOf('guard mustReport else'))
  assert.doesNotMatch(beforeReport, /\bcompletion\(\)\n\s*return/)
  assert.match(beforeReport, /satisfyPushReport\(uuid: uuid, call: call, mustReport: mustReport\)/)

  const placeholder = body('reportUnpresentedPush')
  assert.match(placeholder, /guard mustReport else/)
  assert.match(placeholder, /configureCallHistory\(enabled: false\)[\s\S]*reportNewIncomingCall\(with: reportUUID/)
  assert.match(placeholder, /reportCall\(with: reportUUID, endedAt: Date\(\), reason: \.remoteEnded\)/)
  assert.match(placeholder, /configureCallHistory\(enabled: previousRecents\)/)
  assert.match(body('satisfyPushReport'), /self\.callIDsByUUID\[uuid\] == nil/)
})

test('tracked iOS configuration contains no personal signing identity', async () => {
  const [project, sharedConfig] = await Promise.all([
    source('ios/App/App.xcodeproj/project.pbxproj'),
    source('ios/Config/ModemDeck.xcconfig')
  ])
  const trackedPaths = execFileSync('git', ['ls-files', '-z'], { cwd: rootPath })
    .toString('utf8')
    .split('\0')
    .filter(Boolean)
  const trackedEntries = await Promise.all(trackedPaths.map(async path => {
    const data = await readFile(new URL(path, root)).catch(error => {
      if (error.code === 'ENOENT') return Buffer.alloc(0) // A tracked deletion has no packaged content.
      throw error
    })
    return data.includes(0) ? '' : `[${path}]\n${data.toString('utf8')}`
  }))
  const trackedConfiguration = trackedEntries.join('\n')
  const personalSigningFiles = /(?:^|\/)(?:LocalSigning\.xcconfig|[^/]+\.(?:p8|p12|cer|mobileprovision))$|\.xcarchive\//i
  const developmentTeamAssignment = new RegExp(`DEVELOPMENT${'_TEAM'}\\s*=`)
  const privateKeyHeader = new RegExp(`BEGIN (?:EC |RSA )?${'PRIVATE'} KEY`)

  assert.equal(trackedPaths.some(path => personalSigningFiles.test(path)), false)
  assert.doesNotMatch(
    trackedConfiguration,
    /com\.(?!example\b)[a-z0-9.-]+\.modemdeck/i
  )
  assert.doesNotMatch(trackedConfiguration, developmentTeamAssignment)
  assert.doesNotMatch(trackedConfiguration, privateKeyHeader)
  assert.match(sharedConfig, /MODEMDECK_BUNDLE_IDENTIFIER = com\.example\.modemdeck/)
  assert.match(sharedConfig, /#include\? "\.\.\/LocalSigning\.xcconfig"/)
  assert.match(project, /PRODUCT_BUNDLE_IDENTIFIER = "\$\(MODEMDECK_BUNDLE_IDENTIFIER\)"/)
})

test('SwiftUI is the universal iPhone and iPad application root', async () => {
  const [scene, app, calls, project, info, runner, packageJSON] = await Promise.all([
    source('ios/App/App/SceneDelegate.swift'),
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckCallViews.swift'),
    source('ios/App/App.xcodeproj/project.pbxproj'),
    source('ios/App/App/Info.plist'),
    source('scripts/run-ios.mjs'),
    source('package.json')
  ])

  assert.match(scene, /UIHostingController\([\s\S]*ModemDeckRootView/)
  assert.doesNotMatch(scene, /CAPBridgeViewController|SceneDelegateProxy/)
  assert.match(app, /ModemDeckWorkspaceShell/)
  // The system tab view: a tab bar on iPhone, a sidebar on iPad and the status-column tab bar on
  // iPhone Duo; iOS 17 falls back to the classic tab items.
  assert.match(app, /if #available\(iOS 18\.0, \*\) \{\s*TabView\(selection: selection\) \{[\s\S]*Tab\(section\.title\(controller\), systemImage: section\.systemImage, value: section\)[\s\S]*\.tabViewStyle\(\.sidebarAdaptable\)/)
  assert.match(app, /\.tabItem \{ Label\(section\.title\(controller\), systemImage: section\.systemImage\) \}/)
  assert.match(app, /\.badge\(badges\.count\(for: section\)\)/)
  assert.doesNotMatch(app, /ModemDeckPhoneTabBar|ModemDeckPadNavigationRail|ModemDeckStatusColumnNavigation|ModemDeckLazySectionHost/)
  assert.match(app, /case dial/)
  assert.match(app, /ModemDeckDialerPanel/)
  assert.match(app, /\.preferredColorScheme\(ModemDeckAppearance\(rawValue: appearance\)\?\.colorScheme\)/)
  assert.match(app, /get: \{ controller\.selectedSection == \.dial \? \.home : controller\.selectedSection \}/)
  assert.match(app, /struct ModemDeckPadDialerAction/)
  assert.match(app, /accessibilityLabel: controller\.text\("打开拨号盘", "Open dialer"\)/)
  assert.doesNotMatch(app, /interactiveSpring\(response: 0\.3[48]/)
  assert.match(project, /TARGETED_DEVICE_FAMILY = "1,2";/)
  assert.doesNotMatch(
    project.match(/PBXResourcesBuildPhase section[\s\S]*?End PBXResourcesBuildPhase section/)?.[0] || '',
    /public in Resources|Main\.storyboard in Resources|capacitor\.config\.json in Resources|config\.xml in Resources/
  )
  assert.doesNotMatch(info, /UIMainStoryboardFile|UISceneStoryboardFile/)
  assert.doesNotMatch(info, /<key>UIUserInterfaceStyle<\/key>\s*<string>Light<\/string>/)
  const phoneOrientations = info.match(
    /<key>UISupportedInterfaceOrientations<\/key>\s*<array>([\s\S]*?)<\/array>/
  )?.[1] || ''
  const padOrientations = info.match(
    /<key>UISupportedInterfaceOrientations~ipad<\/key>\s*<array>([\s\S]*?)<\/array>/
  )?.[1] || ''
  assert.match(phoneOrientations, /UIInterfaceOrientationPortrait/)
  assert.doesNotMatch(phoneOrientations, /Landscape|PortraitUpsideDown/)
  for (const orientation of [
    'UIInterfaceOrientationPortrait',
    'UIInterfaceOrientationPortraitUpsideDown',
    'UIInterfaceOrientationLandscapeLeft',
    'UIInterfaceOrientationLandscapeRight'
  ]) {
    assert.match(padOrientations, new RegExp(`<string>${orientation}<\\/string>`))
  }
  assert.match(app, /GeometryReader \{ geometry in[\s\S]*let sceneWidth = geometry\.size\.width \+ geometry\.safeAreaInsets\.leading \+ geometry\.safeAreaInsets\.trailing[\s\S]*let usesSplitWorkspace = sceneWidth >= ModemDeckLayout\.splitWorkspaceMinimumWidth/)
  assert.doesNotMatch(app, /horizontalSizeClass/)
  assert.match(
    app,
    /static var contentSections: \[ModemDeckSection\] \{\s*\[\.home, \.messages, \.contacts, \.calls, \.settings\]/
  )
  assert.doesNotMatch(app, /ModemDeckMoreSheet|case more|showingMore/)
  assert.match(runner, /MODEMDECK_SIMULATOR_FAMILY/)
  assert.match(runner, /iPhone 17 Pro Max/)
  assert.match(runner, /iPad Pro 13-inch/)
  assert.match(app, /MODEMDECK_UAT_INITIAL_SECTION/)
  assert.match(calls, /MODEMDECK_UAT_CALL_KEYPAD/)
  assert.match(calls, /let panelWidth = min\(\s*geometry\.size\.height < ModemDeckCallStageLayout\.widePanelMaximumHeight \? 480 : 420/)
  assert.match(calls, /if reduceMotion \{[\s\S]*showingKeypad\.toggle\(\)/)
  assert.match(calls, /callLine\?\.capabilities\?\.sendDtmf != false/)
  const scripts = JSON.parse(packageJSON).scripts
  assert.equal(scripts['run:ios:iphone'].includes('MODEMDECK_SIMULATOR_FAMILY=iphone'), true)
  assert.equal(scripts['run:ios:ipad'].includes('MODEMDECK_SIMULATOR_FAMILY=ipad'), true)
})

test('native branding uses the Web mark and exact Lucide vector family', async () => {
  const [generator, communication, phoneSVG, messageSVG, audioSVG, notice, project] = await Promise.all([
    source('scripts/generate-app-icon.swift'),
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/Assets.xcassets/LucidePhone.imageset/lucide-phone.svg'),
    source('ios/App/App/Assets.xcassets/LucideMessageSquareText.imageset/lucide-message-square-text.svg'),
    source('ios/App/App/Assets.xcassets/LucideAudioLines.imageset/lucide-audio-lines.svg'),
    source('ios/App/App/ThirdPartyNotices.txt'),
    source('ios/App/App.xcodeproj/project.pbxproj')
  ])
  const appIcon = await readFile(
    new URL('ios/App/App/Assets.xcassets/AppIcon.appiconset/AppIcon-512@2x.png', root)
  )

  assert.match(generator, /17 \/ 255[\s\S]*120 \/ 255[\s\S]*100 \/ 255/)
  assert.match(generator, /point\(8, 23\)[\s\S]*point\(24, 23\)/)
  assert.equal(appIcon.readUInt32BE(16), 1024)
  assert.equal(appIcon.readUInt32BE(20), 1024)
  assert.equal(appIcon[25], 2)
  assert.match(communication, /static let phone = "LucidePhone"/)
  assert.match(phoneSVG, /M13\.832 16\.568/)
  assert.match(messageSVG, /M22 17[\s\S]*M7 11h10/)
  assert.match(audioSVG, /M2 10v3[\s\S]*M22 10v3/)
  assert.match(notice, /Lucide Icons 1\.28\.0[\s\S]*ISC License/)
  assert.match(project, /ThirdPartyNotices\.txt in Resources/)
})

test('simulator builds retain local signing so Keychain-backed UAT pairing works', async () => {
  const builder = await source('scripts/build-ios.mjs')

  assert.doesNotMatch(builder, /CODE_SIGNING_ALLOWED=NO/)
  assert.match(builder, /'generic\/platform=iOS Simulator'/)
})

test('native API models match populated communication responses', async () => {
  const [api, calls, settings, fixture] = await Promise.all([
    source('ios/App/App/ModemDeckAPI.swift'),
    source('ios/App/App/ModemDeckCallViews.swift'),
    source('ios/App/App/ModemDeckSettingsView.swift'),
    source('scripts/uat-mock-server.mjs')
  ])

  assert.match(api, /let lastMessageId: Int64/)
  assert.match(api, /struct ModemDeckRecordingCall/)
  assert.match(api, /serverURL = "server_url"/)
  assert.match(api, /let answerCall: Bool\?/)
  assert.match(api, /let sendDtmf: Bool\?/)
  assert.match(api, /let sendMessage: Bool\?/)
  assert.match(calls, /accessibilityIdentifier\("call-recording"\)/)
  assert.match(calls, /accessibilityIdentifier\("call-recording-status"\)/)
  assert.match(calls, /accessibilityIdentifier\("dial-recording-status"\)/)
  assert.match(calls, /ModemDeckDialerPanel/)
  assert.match(settings, /从本机通讯录导入/)
  assert.match(settings, /开始通话测试/)
  assert.match(fixture, /示例联系人/)
  assert.match(fixture, /UAT-SERVICE/)
  assert.match(fixture, /不包含真实个人信息/)
  assert.match(fixture, /answer_call: true/)
  assert.match(fixture, /send_dtmf: true/)
  assert.match(fixture, /send_message: true/)
})

test('native pairing decodes the explicit server_url key exactly once', async () => {
  const [api, session] = await Promise.all([
    source('ios/App/App/ModemDeckAPI.swift'),
    source('ios/App/App/ModemDeckSession.swift')
  ])

  assert.match(api, /serverURL = "server_url"/)
  assert.match(
    session,
    /let decoder = JSONDecoder\(\)\s+let payload = try decoder\.decode\(ModemDeckPairingPayload\.self/
  )
  assert.doesNotMatch(
    session,
    /let decoder = JSONDecoder\(\)\s+decoder\.keyDecodingStrategy/
  )
})

test('native recents keeps real line identities and shortcuts into the shared calls store', async () => {
  const [app, home] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'), source('ios/App/App/ModemDeckHomeView.swift')
  ])
  assert.match(home, /ModemDeckHomeLineGrid\([\s\S]*lines: controller\.bootstrap\?\.lines \?\? \[\]/)
  assert.match(app, /private struct ModemDeckHomeLineCard/)
  assert.match(home, /controller\.openCalls\(filter: filter\)/)
  assert.match(home, /ModemDeckPageHeader\(title: controller\.text\("最近", "Recents"\)\)/)
  assert.doesNotMatch(app + home, /ModemDeckHomeSummaryGrid|unreadMessageCount|missedCallCount|onlineLineCount/)
})

test('native notification permission and delivery use the SwiftUI session', async () => {
  const [settings, delegate, scene, native, session] = await Promise.all([
    source('ios/App/App/ModemDeckSettingsView.swift'),
    source('ios/App/App/AppDelegate.swift'),
    source('ios/App/App/SceneDelegate.swift'),
    source('ios/App/App/ModemDeckNative.swift'),
    source('ios/App/App/ModemDeckSession.swift')
  ])

  for (const method of [
    'requestNotificationPermission',
    'openNotificationSettings',
    'scheduleLocalTestNotification'
  ]) {
    assert.match(settings, new RegExp(`controller\\.${method}\\(`))
    assert.match(session, new RegExp(`func ${method}\\(`))
  }
  assert.match(session, /UNUserNotificationCenter\.current\(\)\.requestAuthorization/)
  assert.match(session, /UNTimeIntervalNotificationTrigger/)
  assert.match(native, /#if DEBUG[\s\S]*installModemDeckUATCredentialFromEnvironment/)
  assert.match(delegate, /#if DEBUG[\s\S]*installModemDeckUATCredentialFromEnvironment/)
  assert.match(scene, /AppDelegate\)\?\.credentialStore/)
  assert.match(scene, /UIHostingController/)
  assert.match(session, /ModemDeckPushCoordinator\.shared\.configure\(store: credentialStore\)/)
})

test('native notifications refresh foreground data, deep-link taps, and shield private app snapshots', async () => {
  const [delegate, scene, native, app, session] = await Promise.all([
    source('ios/App/App/AppDelegate.swift'),
    source('ios/App/App/SceneDelegate.swift'),
    source('ios/App/App/ModemDeckNative.swift'),
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckSession.swift')
  ])

  assert.match(delegate, /willPresent[\s\S]*handleRemoteNotification/)
  assert.match(delegate, /didReceive response[\s\S]*handleNotificationResponse/)
  assert.match(scene, /connectionOptions\.notificationResponse/)
  assert.match(scene, /sceneWillResignActive[\s\S]*privacyShield/)
  assert.match(scene, /sceneDidBecomeActive[\s\S]*removeFromSuperview/)
  assert.match(native, /modemdeck_message[\s\S]*thread_key/)
  assert.match(native, /consumePendingNotificationRoute/)
  assert.match(app, /modemDeckNotificationResponse[\s\S]*consumePendingNotificationRoute/)
  assert.match(session, /func openMessage\(threadKey:/)
})

test('native pairing and push retries preserve server ownership boundaries', async () => {
  const [native, session, api] = await Promise.all([
    source('ios/App/App/ModemDeckNative.swift'),
    source('ios/App/App/ModemDeckSession.swift'),
    source('ios/App/App/ModemDeckAPI.swift')
  ])

  assert.match(native, /didFailToRegisterForRemoteNotifications[\s\S]*scheduleAPNSRegistrationRetry/)
  assert.match(native, /status == 408 \|\| status == 429 \|\| status >= 500/)
  assert.match(native, /didInvalidatePushTokenFor[\s\S]*syncTokens\(forceEmpty: true\)/)
  assert.match(session, /func disconnect\(\) async \{[\s\S]*try await api\.revokePairing\(\)[\s\S]*resetAfterRevocation/)
  assert.match(session, /refreshGeneration/)
  assert.match(api, /response\.statusCode == 401[\s\S]*modemDeckAuthenticationFailed/)
})

test('native collection stores coalesce refreshes that arrive during an active request', async () => {
  const [home, session] = await Promise.all([
    source('ios/App/App/ModemDeckHomeView.swift'), source('ios/App/App/ModemDeckSession.swift')
  ])
  assert.doesNotMatch(home, /class ModemDeckActivityStore|api\.messageThreads\(|api\.calls\(/)
  assert.match(home, /messages = controller\.messagesStore/)
  assert.match(home, /calls = controller\.callsStore/)
  assert.match(session, /await withCheckedContinuation \{ reloadWaiters\.append/)
  assert.match(session, /guard revision == self\.revision/)
  assert.ok((session.match(/private var reloadRequested = false/g) || []).length >= 4)
  assert.ok((session.match(/reloadRequested = true/g) || []).length >= 4)
  assert.ok((session.match(/while reloadRequested/g) || []).length >= 4)
})

test('native app targets iOS 16 and supports expanding message composers', async () => {
  const [project, communication] = await Promise.all([
    source('ios/App/App.xcodeproj/project.pbxproj'),
    source('ios/App/App/ModemDeckCommunicationViews.swift')
  ])

  assert.equal((project.match(/IPHONEOS_DEPLOYMENT_TARGET = 16\.0;/g) || []).length, 4)
  assert.doesNotMatch(project, /IPHONEOS_DEPLOYMENT_TARGET = 15\./)
  assert.ok((communication.match(/axis: \.vertical/g) || []).length >= 2)
  assert.ok((communication.match(/\.lineLimit\(1\.\.\.5\)/g) || []).length >= 2)
})

test('native tabs retain an independent NavigationStack per module', async () => {
  const app = await source('ios/App/App/ModemDeckApp.swift')

  assert.match(app, /struct ModemDeckSectionRoot[\s\S]*NavigationStack\(path: \$navigation\.path\)/)
  assert.match(app, /final class ModemDeckShellNavigation: ObservableObject[\s\S]*if let existing = paths\[section\] \{ return existing \}/)
  assert.match(app, /@StateObject private var navigation = ModemDeckShellNavigation\(\)/)
  assert.match(app, /navigation: navigation\.navigation\(for: section\)/)
  // Tab switches never rebuild list rows, so returning to a tab doesn't flash its content.
  assert.doesNotMatch(app, /entryRevisions|modemDeckListEntryRevision|listInteractionRevision/)
  // Only the section being left resets its open swipes, while it is hidden.
  // Every section change, including ones the app makes, resets swipes after the switch settles,
  // and only if the user has not already returned.
  assert.match(app, /\.onChange\(of: controller\.selectedSection\) \{ section in[\s\S]{0,240}closeSwipesInSectionLeft\(for: tabSection\(section\)\)/)
  assert.match(app, /guard tabSection\(controller\.selectedSection\) != left else \{ return \}\s*leftNavigation\.swipeResetRevision &\+= 1/)
  // The side-by-side selection lives on the section, so layout changes carry it across.
  // It is a separate object, so pages do not redraw on every push or pop.
  assert.match(app, /final class ModemDeckSectionSelection: ObservableObject \{\s*@Published var route: ModemDeckRoute\?/)
  assert.match(app, /\.environmentObject\(navigation\.selection\)/)
  assert.match(app, /navigation\.path\.removeFirst\(\)\s*navigation\.selection\.route = first/)
  assert.match(app, /navigation\.selection\.route = nil\s*navigation\.path\.insert\(selection, at: 0\)/)
  assert.match(app, /\.environment\(\\\.modemDeckSwipeResetRevision, navigation\.swipeResetRevision\)/)
  // Side-by-side columns each own a navigation bar, so one column's scroll doesn't move the other.
  assert.match(app, /struct ModemDeckSplitColumns[\s\S]*NavigationSplitView\(columnVisibility: \.constant\(\.all\)\)/)
  assert.match(app, /\.onChange\(of: detailID\) \{ _ in navigation\?\.path\.removeAll\(\) \}/)
  assert.match(app, /\.onChange\(of: controller\.selectedSection\)[\s\S]{0,200}resignFirstResponder/)
  assert.doesNotMatch(app, /NavigationView \{\s*destination\(for: controller\.selectedSection\)/)
})

test('native offline mode preserves protected cached history without blocking the shell', async () => {
  const [app, api, session] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckAPI.swift'),
    source('ios/App/App/ModemDeckSession.swift')
  ])

  assert.match(api, /final class ModemDeckOfflineCache/)
  assert.match(api, /for: \.applicationSupportDirectory/)
  assert.match(api, /completeFileProtectionUntilFirstUserAuthentication/)
  assert.match(api, /isExcludedFromBackup = true/)
  assert.match(api, /digest\("\\\(credential\.serverURL\)\\u\{0\}\\\(credential\.token\)"\)/)
  assert.match(api, /func clearCachedData\(\)/)
  assert.match(session, /phase = \.paired[\s\S]*await api\.cachedStartupData\(\)/)
  assert.match(session, /connectionState = \.offline[\s\S]*phase = \.paired/)
  assert.match(session, /contactsStore\.restore\(cached\.contacts\)/)
  assert.match(session, /messagesStore\.restore\(threads: cached\.threads, unread: cached\.unread\)/)
  assert.match(session, /messages = api\.cachedMessages/)
  assert.match(session, /callsStore\.restore\(calls: cached\.calls, recordings: cached\.recordings\)/)
  assert.match(app, /ModemDeckOfflineBanner/)
  assert.match(app, /暂时离线 · 自动重连中/)
  assert.match(app, /历史内容仍可查看和复制/)
  assert.match(session, /ModemDeckConnectionRecovery/)
  assert.match(session, /NWPathMonitor/)
  assert.match(session, /await refreshCollections\(\)/)
  assert.match(api, /isCurrentCredential\(credential\)/)
})

test('native communication rows support selection, copy menus, and Web-equivalent swipe actions', async () => {
  const [app, communication, calls, home, interaction] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckCallViews.swift'),
    source('ios/App/App/ModemDeckHomeView.swift'),
    source('ios/App/App/ModemDeckListInteraction.swift')
  ])
  assert.match(app, /\.textSelection\(\.enabled\)/)
  assert.match(app, /UIPasteboard\.general\.string = item\.value/)
  assert.ok((communication.match(/List \{/g) || []).length >= 2)
  assert.ok((calls.match(/List \{/g) || []).length >= 2)
  assert.match(communication, /contactListRow[\s\S]*ModemDeckListRow/)
  assert.match(communication, /threadListRow[\s\S]*ModemDeckListRow/)
  assert.match(calls, /callListRow[\s\S]*ModemDeckListRow/)
  assert.match(calls, /recordingListRow[\s\S]*ModemDeckListRow/)
  assert.match(home, /activityRow[\s\S]*ModemDeckListRow/)
  assert.match(interaction, /swipeActions\(edge: \.leading, allowsFullSwipe: true\)/)
  assert.match(interaction, /swipeActions\(edge: \.trailing, allowsFullSwipe: false\)/)
  assert.match(interaction, /Button \{ confirmingDelete = true \}/)
  assert.match(interaction, /role: \.destructive\) \{ perform\(delete\) \}/)
  assert.match(calls, /toggleRead: call\.missed \? /)
  assert.match(app, /NavigationStack\(path: \$navigation\.path\)/)
  assert.match(app, /navigationDestination\(for: ModemDeckRoute\.self\)/)
  assert.doesNotMatch(interaction, /NavigationLink \{/)
  for (const rows of [communication, calls]) assert.match(rows, /actions: contextActions/)
})

test('native communication avatars and phone copying follow the Web identity rules', async () => {
  const [api, session, communication, calls] = await Promise.all([
    source('ios/App/App/ModemDeckAPI.swift'),
    source('ios/App/App/ModemDeckSession.swift'),
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckCallViews.swift')
  ])

  assert.match(api, /func modemDeckContact\(id: String\?, number: String\)/)
  assert.match(api, /return matches\.count == 1 \? matches\.first : nil/)
  assert.match(session, /ModemDeckMessagesStore[\s\S]*@Published private\(set\) var contacts/)
  assert.match(session, /ModemDeckCallsStore[\s\S]*@Published private\(set\) var contacts/)
  assert.match(communication, /struct ModemDeckCommunicationAvatar/)
  assert.match(communication, /enum ModemDeckLucideAsset/)
  assert.match(communication, /ModemDeckLucideIcon\(asset: channel\.iconAsset/)
  assert.doesNotMatch(communication, /Image\(systemName: channel\.symbol\)/)
  assert.match(communication, /guard contactBound else \{ return channel\.fallback \}/)
  assert.match(communication, /if contactBound \{[\s\S]*ModemDeckLucideIcon\(asset: channel\.iconAsset/)
  assert.match(communication, /Data\(base64Encoded:[\s\S]*UIImage\(data:/)
  assert.match(communication, /ModemDeckIdentityHeader[\s\S]*var copyItems: \[ModemDeckCopyItem\]/)
  assert.match(communication, /ForEach\(Array\(displayedContact\.phones[\s\S]*复制号码[\s\S]*phone\.displayNumber/)
  assert.match(calls, /ModemDeckCallRecordRow[\s\S]*ModemDeckCommunicationAvatar\([\s\S]*channel: \.call/)
  assert.match(calls, /ModemDeckRecordingRow[\s\S]*ModemDeckCommunicationAvatar\([\s\S]*channel: \.recording/)
})

test('native NetEq links one shared static core without PeerConnection or a second Opus package', async () => {
  const project = await source('ios/App/App.xcodeproj/project.pbxproj')
  assert.doesNotMatch(project, /WebRTC|generate-webrtc-dsym/)
  assert.match(project, /MDNetEq.xcframework/)
  assert.doesNotMatch(project, /Copus|swift-opus|audio_core.c/)
})

test('native communication density keeps compact visuals and full touch targets', async () => {
  const [app, communication, calls, settings] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckCallViews.swift'),
    source('ios/App/App/ModemDeckSettingsView.swift')
  ])

  assert.match(app, /static let controlHitSize: CGFloat = 44/)
  assert.match(app, /static let controlVisualSize: CGFloat = 36/)
  assert.match(app, /static let listRowMinHeight: CGFloat = 80/)
  // Search is the system field, sized and styled by the platform.
  assert.match(app, /\.searchable\(\s*text: \$query,\s*placement: \.navigationBarDrawer\(displayMode: \.automatic\)/)
  assert.match(app, /struct ModemDeckSegmentPicker[\s\S]*\.font\(\.caption\.weight/)
  assert.match(app, /accessibilityAddTraits\(active \? \.isSelected : \[\]\)/)
  assert.ok((communication.match(/ModemDeckCollectionHeader\(/g) || []).length >= 2)
  assert.ok((calls.match(/ModemDeckCollectionHeader\(/g) || []).length >= 2)
  assert.match(app, /static let settingsRowMinHeight: CGFloat = 64/)
  assert.match(settings, /ModemDeckSettingsDirectoryRow[\s\S]*ModemDeckLayout\.settingsRowMinHeight/)
})

test('microphone permission is requested only by an explicit call or settings action', async () => {
  const [native, session, settings] = await Promise.all([
    source('ios/App/App/ModemDeckNative.swift'),
    source('ios/App/App/ModemDeckSession.swift'),
    source('ios/App/App/ModemDeckSettingsView.swift')
  ])

  const configureBody = native.match(
    /func configure\(store: ModemDeckCredentialStore\) \{([\s\S]*?)\n    \}/
  )?.[1] || ''
  assert.doesNotMatch(configureBody, /requestMicrophoneAccess/)
  assert.match(native, /func requestMicrophoneAccess/)
  assert.match(session, /func requestMicrophonePermission\(\) async/)
  assert.match(session, /func start\([\s\S]*requestMicrophoneAccess/)
  assert.match(settings, /requestMicrophonePermission/)
})

test('native phone detail screens enable the standard left-edge back gesture', async () => {
  const [app, communication, calls, settings] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckCallViews.swift'),
    source('ios/App/App/ModemDeckSettingsView.swift')
  ])

  assert.match(app, /interactivePopGestureRecognizer/)
  assert.match(app, /UIGestureRecognizerDelegate/)
  assert.match(app, /viewControllers\.count[^\n]*> 1/)
  // Slow swipes must not depend on a fast flick: direction comes from accumulated movement.
  assert.match(app, /if gestureRecognizer === edgeGesture \{ return true \}/)
  assert.match(app, /translation == \.zero \? pan\.velocity\(in: view\) : translation/)
  assert.doesNotMatch(app, /return velocity\.x \* direction > abs\(velocity\.y\)/)
  assert.match(communication, /let movement = ModemDeckPanDirection\.movement\(of: pan, in: pan\.view\)/)
  // A full-width message long-press target claimed back swipes that began with a pause.
  assert.match(communication, /\.clipShape\(bubbleShape\)\s*\.contentShape\(\.contextMenuPreview, bubbleShape\)[\s\S]{0,240}\.modemDeckCopyMenu\(/)
  assert.match(app, /func modemDeckInteractiveBack/)
  assert.match(communication, /\.modemDeckInteractiveBack\(showsBackButton\)/)
  assert.match(communication, /\.modemDeckInteractiveBack\(\)/)
  assert.match(calls, /\.modemDeckInteractiveBack\(showsBackButton\)/)
  assert.match(settings, /ModemDeckSettingsDetailScaffold[\s\S]*\.modemDeckInteractiveBack\(showsBackButton\)/)
})

test('conversations and selection hide the system tab bar natively', async () => {
  const [app, communication, routes] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckListInteraction.swift')
  ])

  // The system hides the tab bar and resizes the page together, keeping transitions and swipe-back.
  assert.match(app, /func modemDeckTabBarVisible\(_ visible: Bool\) -> some View \{\s*if visible \{ self \} else \{ toolbar\(\.hidden, for: \.tabBar\) \}/)
  assert.match(app, /func modemDeckImmersiveScreen\(_ pushed: Bool = true\) -> some View \{\s*modifier\(ModemDeckImmersiveScreenModifier\(pushed: pushed\)\)/)
  assert.match(app, /content\.modemDeckTabBarVisible\(!pushed \|\| inSplitDetail\)/)
  // A conversation embedded beside its list leaves the tab bar or sidebar alone.
  assert.match(communication, /\.modemDeckImmersiveScreen\(showsBackButton\)/)
  // Selection's toolbar takes the tab bar's place.
  assert.match(app, /private var toolbar: some View \{[\s\S]*\.toolbar\(\.hidden, for: \.tabBar\)/)
  // A collapsed call sits above the tab bar, or above the composer when it steps aside.
  assert.match(app, /\.safeAreaInset\(edge: \.bottom, spacing: 0\) \{\s*if let call = miniCall \{\s*ModemDeckMiniCallBar/)
  assert.doesNotMatch(app, /modemDeckTabBarClearance|ModemDeckBottomChrome|immersiveScreenCount/)
  assert.match(routes, /var opensConversation: Bool \{\s*if case \.message = self/)
})

test('native iPad communication workspaces mirror the Web list-detail geometry', async () => {
  const [app, communication, calls] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckCallViews.swift')
  ])

  assert.match(app, /static let padListWidth: CGFloat = 340/)
  assert.match(app, /static let splitWorkspaceMinimumWidth: CGFloat = 900/)
  // iPhone Duo's unfolded display is phone idiom, so layout follows the scene, not the device.
  assert.doesNotMatch(app, /userInterfaceIdiom|isPad/)
  assert.match(calls, /presentsAsPadPanel = horizontalSizeClass == \.regular && verticalSizeClass == \.regular/)
  // iPhone Duo's status column is the system tab view's own; nothing custom draws over it.
  assert.doesNotMatch(app, /statusColumn/)
  // Short stages keep every call control visible without scrolling.
  assert.match(calls, /let compact = available\.size\.height < ModemDeckCallStageLayout\.compactHeight/)
  assert.match(calls, /dynamicTypeSize\.isAccessibilitySize \? 2 : oneRow \? 4 : \(kinds\.count == 4 \? 2 : 3\)/)
  // List columns are proportional to the screen, between a usable minimum and a readable maximum.
  assert.match(app, /static func listColumnWidth\(forContentWidth width: CGFloat\) -> CGFloat \{\s*min\(400, max\(320, \(width \* 0\.36\)\.rounded\(\)\)\)/)
  assert.match(communication, /\.frame\(maxWidth: ModemDeckLayout\.readableContentWidth\)/)
  assert.match(communication, /selectedContactID/)
  assert.match(communication, /selectedThreadID/)
  assert.ok((communication.match(/ModemDeckSplitColumns\(/g) || []).length >= 2)
  assert.ok(
    (communication.match(/showsBackButton: false/g) || []).length >= 2
  )
  assert.match(calls, /selectedCallID/)
  assert.match(calls, /selectedRecordingID/)
  assert.ok((calls.match(/ModemDeckSplitColumns\(/g) || []).length >= 2)
  assert.match(app, /list\(\)\s*\.navigationSplitViewColumnWidth\(listWidth\)/)
  assert.ok((calls.match(/showsBackButton: false/g) || []).length >= 2)
})

test('native settings expose the full personal and management directory', async () => {
  const [settings, api] = await Promise.all([
    source('ios/App/App/ModemDeckSettingsView.swift'),
    source('ios/App/App/ModemDeckAPI.swift')
  ])

  for (const view of [
    'ModemDeckPreferencesSettingsView',
    'ModemDeckSecuritySettingsView',
    'ModemDeckNotificationSettingsView',
    'ModemDeckCallSettingsView',
    'ModemDeckConnectionSettingsView',
    'ModemDeckContactImportSettingsView',
    'ModemDeckDevicesSettingsView',
    'ModemDeckUsersSettingsView',
    'ModemDeckTelegramSettingsView',
    'ModemDeckAccessSettingsView',
    'ModemDeckDiagnosticsSettingsView',
    'ModemDeckAboutSettingsView'
  ]) {
    assert.match(settings, new RegExp(`\\b${view}\\b`))
  }

  const managementOrder = [
    'ModemDeckUsersSettingsView',
    'ModemDeckDevicesSettingsView',
    'ModemDeckTelegramSettingsView',
    'ModemDeckAccessSettingsView',
    'ModemDeckDiagnosticsSettingsView',
    'ModemDeckAboutSettingsView'
  ].map(view => settings.indexOf(view))
  assert.deepEqual(managementOrder, [...managementOrder].sort((left, right) => left - right))
  assert.doesNotMatch(settings, /controller\.text\("信息", "Information"\)/)

  assert.match(settings, /controller\.text\("接收来电", "Receive Calls"\)/)
  assert.match(settings, /Apple 设备 \\\(user\.iosPairingDeviceCount\)\/3/)
  assert.doesNotMatch(settings, /profileName!/)
  assert.match(api, /func accountSessions\(/)
  assert.match(api, /func managedDevices\(/)
  assert.match(api, /func users\(/)
  assert.match(api, /func telegramUnits\(/)
  assert.match(api, /func diagnostics\(/)
  assert.match(api, /let callRuntime: ModemDeckDiagnosticAvailability/)
  assert.match(api, /var calls: ModemDeckDiagnosticAvailability \{ callRuntime \}/)
})

test('native dialer preserves its draft, line choice, recording preference and DTMF controls', async () => {
  const [calls, app, session] = await Promise.all([
    source('ios/App/App/ModemDeckCallViews.swift'), source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckSession.swift')
  ])
  assert.match(session, /let dialDraft = ModemDeckDialDraft\(\)/)
  assert.match(calls, /wrappedValue: controller\.dialDraft/)
  assert.match(calls, /draft\.lineSelectionOverridden/)
  assert.match(calls, /voiceDialLine\(preferredID: contact\.preferredLineId\)/)
  assert.match(calls, /normalizeModemDeckDialTarget/)
  assert.match(calls, /controller\.api\.recordingSettings\(\)/)
  assert.match(calls, /recording: draft\.recordingInitialized \? draft\.recording : nil/)
  assert.match(calls, /LongPressGesture\(minimumDuration: 0\.5\)/)
  assert.match(calls, /ModemDeckDTMFTonePlayer\.shared\.play\(digit\)/)
  assert.match(app, /\.sheet\(isPresented: \$showingDialer\)/)
  assert.match(app, /\.presentationDragIndicator\(\.visible\)/)
  assert.match(calls, /pressedDigits: dtmfDigits/)
  // The in-call keypad reuses the dialer key without the dialer-only "+" long press.
  assert.match(calls, /ModemDeckDialKeyButton\(digit: key\.0, letters: key\.1, size: keySize, allowsPlus: false/)
})

test('CallKit foreground reconciliation opens the native active-call surface', async () => {
  const [app, session, native] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckSession.swift'),
    source('ios/App/App/ModemDeckNative.swift')
  ])

  assert.match(app, /if let call = callController\.visibleCall[\s\S]*ModemDeckActiveCallView/)
  assert.match(app, /UIApplication\.didBecomeActiveNotification[\s\S]*callController\.refreshFromCallKit\(\)/)
  assert.match(session, /func refreshFromCallKit\(\)[\s\S]*currentCallState/)
  assert.match(native, /answerRequestedCallUUIDs\.insert\(uuid\)[\s\S]*publishCallState\(\)/)
  assert.match(native, /state = "connecting"/)
})

test('native call reconciliation scopes snapshots to the current call stream', async () => {
  const native = await source('ios/App/App/ModemDeckNative.swift')

  assert.match(native, /guard eventName == "call_state"/)
  assert.match(native, /runtime\/events\?call_id=\\\(encodedCallID\)/)
  assert.match(native, /acceptRuntimeCallState\(state, callID: callID, generation: generation\)/)
  assert.match(native, /guard generation == runtimeCallGeneration,[\s\S]*state\.id == callID, state\.revision >= runtimeCallRevision/)
})

test('unresolved native writes retain one operation identity across ambiguous responses', async () => {
  const [api, session, communication] = await Promise.all([
    source('ios/App/App/ModemDeckAPI.swift'),
    source('ios/App/App/ModemDeckSession.swift'),
    source('ios/App/App/ModemDeckCommunicationViews.swift')
  ])

  assert.match(api, /status == 408 \|\| status == 409 \|\| status == 425/)
  // The SMS identity belongs to one send of one draft, not to its text: an
  // unedited retry reuses it, while the same words sent later are a new SMS.
  assert.match(api, /kind: "send-message",\s*parts: \[lineID, to, content, attempt\]/)
  assert.match(session, /@Published var text = "" \{\s*didSet \{\s*revision \+= 1[\s\S]*?if text != oldValue \{ sendAttempt = UUID\(\)\.uuidString \}/)
  assert.match(communication, /let attempt = draft\.sendAttempt[\s\S]*store\.send\(value, attempt: attempt\)/)
  assert.equal(
    communication.match(/\.onChange\(of: content\) \{ _ in sendAttempt = UUID\(\)\.uuidString \}/g)?.length,
    2
  )
  assert.equal(communication.match(/sendMessage\([^)]*attempt: attempt\)/g)?.length, 2)
})

test('native contacts support full editing and destructive selection actions', async () => {
  const [communication, api, session] = await Promise.all([
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckAPI.swift'),
    source('ios/App/App/ModemDeckSession.swift')
  ])

  assert.match(communication, /struct ModemDeckContactEditor/)
  assert.match(communication, /controller\.text\("姓名", "Name"\)/)
  assert.match(communication, /controller\.text\("电话号码", "Phone Numbers"\)/)
  assert.match(communication, /controller\.text\("首选线路", "Preferred Line"\)/)
  assert.match(communication, /controller\.text\("备注", "Notes"\)/)
  assert.match(communication, /ModemDeckBatchActionBar[\s\S]*deleteSelectedContacts/)
  assert.match(api, /path: "\/api\/v1\/contacts\/batch"/)
  assert.match(api, /func updateContact\(id:/)
  assert.match(api, /func deleteContact\(_ contact:/)
  assert.match(session, /func upsert\(_ contact: ModemDeckContact\)/)
})

test('native communication lists expose Web-equivalent batch mutations', async () => {
  const [communication, calls, api] = await Promise.all([
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckCallViews.swift'),
    source('ios/App/App/ModemDeckAPI.swift')
  ])

  assert.match(communication, /struct ModemDeckNewMessageView/)
  assert.match(communication, /icon: "square\.and\.pencil"/)
  assert.match(communication, /private func messageBatchBar\(/)
  assert.match(communication, /mutateSelectedThreads\(hasUnread \? \.read : \.unread\)/)
  assert.match(communication, /mutateSelectedThreads\(allFavorite \? \.unfavorite : \.favorite\)/)
  // Select All with any unread missed call must offer Mark Read first, as in Messages.
  assert.match(calls, /let missedHasUnread = missed\.contains \{ !\$0\.read \}/)
  assert.match(calls, /mutateSelectedCalls\(missedHasUnread \? \.read : \.unread, calls: missed\)/)
  assert.doesNotMatch(calls, /missed\.allSatisfy \{ !\$0\.read \}/)
  assert.match(
    communication,
    /private var selectedContacts:[\s\S]*?filteredContacts\.filter \{ selectedIDs\.contains\(\$0\.id\) \}/
  )
  assert.match(
    communication,
    /private var selectedThreads:[\s\S]*?filteredThreads\.filter \{ selectedIDs\.contains\(\$0\.id\) \}/
  )
  assert.match(calls, /private func callBatchBar\(/)
  assert.match(calls, /private func recordingBatchBar\(/)
  assert.match(calls, /mutateSelectedCalls\(\.delete\)/)
  assert.match(calls, /mutateSelectedRecordings\(\.delete\)/)
  assert.match(
    calls,
    /private var selectedCalls:[\s\S]*?filteredCalls\.filter \{ selectedIDs\.contains\(\$0\.id\) \}/
  )
  assert.match(
    calls,
    /private var selectedRecordings:[\s\S]*?filteredRecordings\.filter \{ selectedIDs\.contains\(\$0\.id\) \}/
  )
  assert.ok(
    ((communication + calls).match(/\.safeAreaInset\(edge: \.bottom, spacing: 0\)/g) || [])
      .length >= 4
  )

  for (const path of [
    '/api/v1/messages/threads/state',
    '/api/v1/calls/batch',
    '/api/v1/recordings/batch'
  ]) {
    assert.match(api, new RegExp(path.replaceAll('/', '\\/')))
  }
})

test('native communication details expose single-item state and delete actions', async () => {
  const [communication, calls] = await Promise.all([
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckCallViews.swift')
  ])

  assert.match(communication, /private var threadHeaderActions/)
  assert.match(communication, /messagesStore\.mutate\(\.delete, threads: \[thread\]\)/)
  assert.match(communication, /mutateThread\(favorite \? \.unfavorite : \.favorite\)/)
  assert.match(calls, /private var callHeaderActions/)
  assert.match(calls, /private var recordingHeaderActions/)
  assert.match(calls, /callsStore\.mutate\(\.delete, calls: \[call\]\)/)
  assert.match(calls, /callsStore\.mutate\(\.delete, recordings: \[recording\]\)/)
})

test('home stays a quick view with shared row actions and split detail', async () => {
  const [home, routing, session] = await Promise.all([
    source('ios/App/App/ModemDeckHomeView.swift'),
    source('ios/App/App/ModemDeckListInteraction.swift'),
    source('ios/App/App/ModemDeckSession.swift')
  ])
  assert.match(home, /if usesSplitWorkspace/)
  assert.match(home, /ModemDeckRouteContent\(route: selectedRoute/)
  assert.match(routing, /recordings: calls\.recordings\.filter/)
  assert.doesNotMatch(home, /recordings: \[\]/)
  assert.doesNotMatch(home, /ModemDeckSearchField|ModemDeckToolbarButton|ModemDeckSegmentPicker|ModemDeckLineFilterMenu|ModemDeckBatchActionBar/)
  assert.doesNotMatch(home, /selectedIDs|mutateSelected|filteredItems/)
  assert.match(home, /ForEach\(activity.days\)/)
  assert.match(home, /\.refreshable \{ await controller\.refresh\(\) \}/)
  assert.match(home, /await messages\.mutate\(action, threads: \[thread\]\)/)
  assert.match(home, /await calls\.mutate\(action, calls: \[call\]\)/)
  assert.match(session, /recordings\.removeAll \{ ids\.contains\(\$0\.call\.id\) \}/)
  assert.match(session, /messagesStore\.apply\(\.read, to: \[thread\.id\]\)/)
})

test('outgoing native calls require prepared media', async () => {
  const [api, native, audio] = await Promise.all([
    source('ios/App/App/ModemDeckAPI.swift'), source('ios/App/App/ModemDeckNative.swift'),
    source('ios/App/App/ModemDeckCallAudio.swift')
  ])
  assert.doesNotMatch(api, /func startCall\(/)
  assert.match(native, /preparation: ModemDeckCallPreparation,/)
  assert.doesNotMatch(native, /preparation: ModemDeckCallPreparation\? = nil/)
  assert.doesNotMatch(audio, /endLateCommittedCall/)
})

test('native layouts adapt to the space and text size instead of clipping', async () => {
  const [app, calls, communication, home] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckCallViews.swift'),
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckHomeView.swift')
  ])

  // The dialer sizes its keys from the space it is given and pins the call row, so the call
  // key is never clipped on short screens or iPad form sheets.
  assert.match(calls, /struct ModemDeckDialerMetrics \{[\s\S]*let byHeight = \(size\.height - chrome - callKey\)/)
  assert.match(calls, /GeometryReader \{ geometry in\s*let metrics = ModemDeckDialerMetrics\(size: geometry\.size\)/)
  assert.match(calls, /actionRow\(callSize: metrics\.callKey\)/)
  // Recording is a real per-call toggle beside the line picker, not a static label.
  assert.match(calls, /private var recordingToggle: some View \{\s*Button \{\s*draft\.recording\.toggle\(\)/)
  assert.match(calls, /private var suggestedContact:/)
  // The in-call keypad shows all twelve keys, and one control grid spans dialling to connected.
  assert.match(calls, /static func keypadKey\(for size: CGSize\) -> CGFloat/)
  assert.match(calls, /return canRecordBeforeConnect \? \[\.mute, \.keypad, \.audio, \.record\] : \[\]/)
  assert.match(calls, /oneRow \? 4 : \(kinds\.count == 4 \? 2 : 3\)/)

  // Detail headers lead with identity: one inline action, the rest (Delete last) behind "…".
  assert.match(communication, /Array\(actions\.filter \{ \$0\.inline && !\$0\.destructive \}\.prefix\(1\)\)/)
  assert.match(communication, /Button\(role: \.destructive, action: item\.action\)/)
  assert.match(communication, /compact\(subtitle\) != compact\(title\)/)
  assert.doesNotMatch(communication, /if actions\.count > 3/)

  // Accessibility sizes rearrange instead of truncating.
  assert.match(app, /if dynamicTypeSize\.isAccessibilitySize \{\s*accessibilityMenu/)
  assert.match(app, /struct ModemDeckRowTitleLine/)
  assert.match(app, /static let headerGlyphMaximumSize = DynamicTypeSize\.xxxLarge/)
  assert.match(home, /AnyLayout\(VStackLayout\(spacing: 8\)\)/)

  // Dialing belongs to Recents and Calls, and never duplicates the iPad rail's Dial button.
  assert.match(app, /section == \.home \|\| section == \.calls \? padDialerAction : nil/)

  // Unknown callers are named once, missed calls use the missed glyph, labels are localized.
  assert.match(calls, /displayName == call\.remoteNumber \? directionText : call\.remoteNumber/)
  assert.doesNotMatch(calls, /if call\.missed \{ return "phone\.down\.fill" \}/)
  assert.match(app, /func phoneLabelText\(_ label: String\) -> String/)
})

test('native interaction feedback follows platform conventions', async () => {
  const [app, calls, communication, home, settings, list, session, mock] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckCallViews.swift'),
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckHomeView.swift'),
    source('ios/App/App/ModemDeckSettingsView.swift'),
    source('ios/App/App/ModemDeckListInteraction.swift'),
    source('ios/App/App/ModemDeckSession.swift'),
    source('scripts/uat-mock-server.mjs')
  ])

  // A sent SMS becomes a bubble at once; a failure stays on it with retry, not a banner.
  assert.match(session, /struct ModemDeckOutgoingMessage: Identifiable, Equatable/)
  assert.match(session, /outbox\[index\]\.state = \.failed\(reason: error\.localizedDescription\)/)
  assert.match(session, /func retry\(_ outgoing: ModemDeckOutgoingMessage\) async -> Bool \{\s*await send\(outgoing\.content, attempt: outgoing\.id\)/)
  assert.match(communication, /private struct ModemDeckOutgoingBubble: View/)
  assert.match(communication, /"未送达 · 轻点重试", "Not Delivered · Tap to Retry"/)
  // Replying keeps the newest message above the keyboard and a growing composer.
  assert.match(communication, /defaultScrollAnchor\(\.bottom, for: \.sizeChanges\)/)
  assert.match(communication, /keyboardDidShowNotification[\s\S]{0,120}if composerFocused \{ scrollToBottom\(\) \}/)

  // Unread sits in the leading gutter for every list; rows under a day heading show the time.
  assert.match(list, /if unread == true && !selecting \{\s*Circle\(\)/)
  assert.doesNotMatch(communication, /contactBound: contactBound,\s*unread: unread,/)
  assert.match(home, /\.environment\(\\\.modemDeckRowsShowTimeOnly, true\)/)

  // Selection: a title-row header and a toolbar in the tab bar's place, which hides.
  assert.match(app, /enum ModemDeckBatchActionBarPart \{\s*\/\/\/[^\n]*\n\s*case header/)
  for (const bar of ['contactBatchBar', 'messageBatchBar']) {
    assert.match(communication, new RegExp(`if selecting \\{ ${bar}\\(\\.header\\) \\}`))
  }
  for (const bar of ['callBatchBar', 'recordingBatchBar']) {
    assert.match(calls, new RegExp(`if selecting \\{ ${bar}\\(\\.header\\) \\}`))
  }


  // Settings: inset-grouped, account first, distinct diagnostics, status-first pairing.
  assert.match(settings, /func modemDeckSettingsGroup\(\) -> some View/)
  assert.match(settings, /profileRow\s*\.modemDeckSettingsGroup\(\)/)
  assert.match(settings, /"服务器诊断", "Server Diagnostics"/)
  assert.doesNotMatch(settings, /"当前账户", "Current Account"/)

  // Contact editing is a native grouped form with localized standard labels.
  assert.match(communication, /private static let standardLabels = \["mobile", "home", "work", "main", "other"\]/)
  assert.match(communication, /ToolbarItem\(placement: \.confirmationAction\)/)

  // Opening a missed call marks it seen, so the Calls badge clears.
  assert.match(calls, /\.task\(id: call\.id\) \{\s*if call\.missed && !isRead && controller\.isOnline \{ mutateCall\(\.read\) \}/)

  // The mutable UAT fixture accepts the writes design reviews exercise.
  assert.match(mock, /pathname === '\/api\/v1\/messages'\) \{[\s\S]*#fail/)
  assert.match(mock, /pathname === '\/api\/v1\/settings\/system'/)
})
