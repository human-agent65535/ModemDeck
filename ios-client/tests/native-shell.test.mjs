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

test('native preview shows one persistent header only for the settings flow', async () => {
  const [builder, styles] = await Promise.all([
    source('scripts/build-web-preview.mjs'),
    source('assets/native-ios.css')
  ])

  assert.match(builder, /nativeIOSSettingsHeaderVisible/)
  assert.match(
    builder,
    /<header v-if=\"nativeIOSSettingsHeaderVisible\" class=\"shell-header\">/
  )
  assert.match(styles, /\.app-shell \.shell-main[\s\S]*safe-area-inset-top/)
  assert.match(styles, /native-ios-settings-header-visible[\s\S]*padding-top: 0/)
  assert.match(styles, /\.shell-header__controls[\s\S]*display: none/)
  assert.match(styles, /\.settings-detail-header[\s\S]*display: none/)
})

test('native communication details keep back navigation beside the avatar', async () => {
  const [builder, navigation] = await Promise.all([
    source('scripts/build-web-preview.mjs'),
    source('assets/nativeIOSNavigation.ts')
  ])

  assert.match(builder, /nativeIOSMobileBackKey/)
  assert.match(builder, /WorkspaceDetailHeader\.vue/)
  assert.match(builder, /workspace-detail-header__native-back/)
  assert.match(builder, /nativeIOSMobileBack && !\$slots\.leading/)
  assert.match(navigation, /nativeIOSMobileBackKey/)
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

test('CallKit owns native WebRTC audio and keeps synthetic calls local', async () => {
  const [native, audio, bridge, builder, project, resolved] = await Promise.all([
    source('ios/App/App/ModemDeckNative.swift'),
    source('ios/App/App/ModemDeckCallAudio.swift'),
    source('assets/native-ios.ts'),
    source('scripts/build-web-preview.mjs'),
    source('ios/App/App.xcodeproj/project.pbxproj'),
    source('ios/App/App.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/Package.resolved')
  ])

  assert.match(project, /stasel\/WebRTC\.git/)
  assert.match(project, /kind = exactVersion;[\s\S]*version = 151\.0\.0;/)
  assert.match(project, /WebRTC in Frameworks/)
  assert.match(resolved, /"identity" : "webrtc"/)
  assert.match(audio, /useManualAudio = true/)
  assert.match(audio, /audioSessionDidActivate/)
  assert.match(audio, /func start\(audioSession: AVAudioSession\)/)
  assert.match(audio, /outputFormat\(forBus: 0\)/)
  assert.match(audio, /overrideOutputAudioPort\(\.speaker\)/)
  assert.ok(
    audio.indexOf('player.scheduleBuffer') < audio.indexOf('try engine.start()'),
    'the test tone must be scheduled before the CallKit-owned audio engine starts'
  )
  assert.match(audio, /callPath\("media\/ice"\)/)
  assert.match(audio, /callPath\("media"\)/)
  assert.match(audio, /callPath\("lease"\)/)
  assert.match(native, /audioSession\.connect/)
  assert.match(native, /sendCallAction\(verb: "hangup"/)
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
  assert.match(native, /notifyListeners\("callState"/)
  assert.match(native, /func currentCallState\(_ call: CAPPluginCall\)/)
  assert.match(bridge, /setNativeIOSCallMuted/)
  assert.match(bridge, /startNativeIOSOutgoingCall/)
  assert.match(bridge, /answerNativeIOSCall/)
  assert.match(bridge, /endNativeIOSCall/)
  assert.match(bridge, /sendNativeIOSCallDTMF/)
  assert.match(bridge, /readNativeIOSCallState/)
  assert.match(bridge, /listenNativeIOSCallState/)
  assert.match(builder, /nativeIOSMode, setNativeIOSCallMuted/)
  assert.match(builder, /if \(nativeIOSMode\)[\s\S]*callMediaState\.status = 'active'/)
  assert.match(builder, /startNativeIOSOutgoingCall/)
  assert.match(builder, /answerNativeIOSCall/)
  assert.match(builder, /endNativeIOSCall/)
  assert.match(builder, /sendNativeIOSCallDTMF/)
  assert.match(builder, /acceptNativeIOSCallState/)
  assert.match(builder, /isNativeIOSTestCallSession/)
  assert.match(builder, /function showIncomingCallNotification[\s\S]*if \(nativeIOSMode\) return/)
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
  assert.match(app, /\.accessibilityHidden\(callController\.call != nil\)/)
  assert.match(app, /\.accessibilityAddTraits\(\.isModal\)/)
  assert.match(api, /recordingEnabled: Bool\?/)
  assert.match(api, /func callRecording\(callID:/)
  assert.match(session, /func enqueueDTMF\(_ digits: String\) -> Bool/)
  assert.match(session, /private var dtmfQueue: \[DTMFRequest\]/)
  assert.match(session, /drainDTMFQueue\(\)/)
  assert.match(session, /recordingReady \? recording : nil|recording: Bool\?/)
  assert.match(session, /capabilities\?\.dial == true && \$0\.capabilities\?\.media == true/)
  assert.match(calls, /\(48\.\.\.57\)\.contains\(scalar\.value\)/)
  assert.match(calls, /\.keyboardType\(\.namePhonePad\)/)
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
  assert.match(native, /reportCall\(with: uuid, endedAt: Date\(\), reason: reason\)/)
  assert.match(
    native,
    /guard mustReport else[\s\S]*reportNewIncomingCall\(with: uuid, update: update\)[\s\S]*reportCall/
  )
  assert.match(native, /private static let maximumRememberedEndedCalls = 64/)
  assert.match(native, /endedCallUUIDs\.contains\(uuid\)/)
  assert.match(native, /private var providerCallActions:/)
  assert.match(native, /settleProviderCallActions\(for: uuid\)/)
})

test('tracked iOS configuration contains no personal signing identity', async () => {
  const [project, sharedConfig, capacitorConfig] = await Promise.all([
    source('ios/App/App.xcodeproj/project.pbxproj'),
    source('ios/Config/ModemDeck.xcconfig'),
    source('capacitor.config.json')
  ])
  const trackedPaths = execFileSync('git', ['ls-files', '-z'], { cwd: rootPath })
    .toString('utf8')
    .split('\0')
    .filter(Boolean)
  const trackedEntries = await Promise.all(trackedPaths.map(async path => {
    const data = await readFile(new URL(path, root))
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
  assert.equal(JSON.parse(capacitorConfig).appId, 'com.example.modemdeck')
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
  assert.match(app, /ModemDeckPhoneTabBar/)
  const phoneTabStart = app.indexOf("private struct ModemDeckPhoneTabBar")
  const padShellStart = app.indexOf("private struct ModemDeckPadNavigationRail")
  const phoneTab = app.slice(phoneTabStart, padShellStart)
  assert.match(phoneTab, /\.offset\(y: -10\)/)
  assert.match(phoneTab, /guard !showingDialer else \{ return \}/)
  assert.doesNotMatch(phoneTab, /showingDialer \? "xmark"/)
  assert.doesNotMatch(phoneTab, /showingDialer\.toggle\(\)/)
  assert.match(phoneTab, /\.background\(alignment: \.top\)/)
  assert.match(app, /case dial/)
  assert.match(app, /ModemDeckDialerPanel/)
  assert.match(app, /\.preferredColorScheme\(callController\.call == nil \? \.light : \.dark\)/)
  assert.match(
    app,
    /struct ModemDeckSectionTabs[\s\S]*ModemDeckLazySectionHost\([\s\S]*activeSection: activeSection/
  )
  assert.doesNotMatch(app, /\.opacity\(section == activeSection \? 1 : 0\)/)
  assert.doesNotMatch(app, /TabView\(selection:/)
  assert.match(app, /ModemDeckPadNavigationRail/)
  assert.match(app, /\.frame\(width: 92\)/)
  assert.match(app, /struct ModemDeckPadDialerAction/)
  assert.match(app, /\\\.modemDeckPadDialerAction[\s\S]*controller\.text\("打开拨号盘", "Open dialer"\)/)
  assert.match(app, /ModemDeckLucideIcon\([\s\S]*ModemDeckLucideAsset\.phoneCall/)
  assert.match(app, /duration: 0\.22/)
  assert.match(app, /ModemDeckDialerMotion\.phoneTransition/)
  assert.match(app, /ModemDeckDialerMotion\.padTransition/)
  assert.doesNotMatch(app, /interactiveSpring\(response: 0\.3[48]/)
  assert.doesNotMatch(app, /\.animation\(dialerAnimation, value: showingDialer\)/)
  assert.match(project, /TARGETED_DEVICE_FAMILY = "1,2";/)
  assert.doesNotMatch(
    project.match(/PBXResourcesBuildPhase section[\s\S]*?End PBXResourcesBuildPhase section/)?.[0] || '',
    /public in Resources|Main\.storyboard in Resources|capacitor\.config\.json in Resources|config\.xml in Resources/
  )
  assert.doesNotMatch(info, /UIMainStoryboardFile|UISceneStoryboardFile/)
  assert.match(info, /<key>UIUserInterfaceStyle<\/key>\s*<string>Light<\/string>/)
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
  assert.match(app, /GeometryReader \{ geometry in[\s\S]*geometry\.size\.width >= ModemDeckLayout\.splitWorkspaceMinimumWidth/)
  assert.doesNotMatch(app, /horizontalSizeClass/)
  assert.match(
    app,
    /private enum ModemDeckPhoneTab:[\s\S]*case home[\s\S]*case contacts[\s\S]*case messages[\s\S]*case dial[\s\S]*case calls[\s\S]*case recordings[\s\S]*case settings/
  )
  assert.doesNotMatch(app, /ModemDeckMoreSheet|case more|showingMore/)
  assert.match(runner, /MODEMDECK_SIMULATOR_FAMILY/)
  assert.match(runner, /iPhone 17 Pro Max/)
  assert.match(runner, /iPad Pro 13-inch/)
  assert.match(app, /MODEMDECK_UAT_INITIAL_SECTION/)
  assert.match(calls, /MODEMDECK_UAT_CALL_KEYPAD/)
  assert.match(calls, /let panelWidth = min\(420/)
  assert.match(calls, /if reduceMotion \{[\s\S]*showingKeypad\.toggle\(\)/)
  assert.match(calls, /callLine\?\.capabilities\?\.sendDtmf != false/)
  assert.match(calls, /if isActive && !call\.testCall \{[\s\S]*disabled: !canSendDTMF/)
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
  assert.match(calls, /private var recordingTitle: String/)
  assert.match(calls, /case "recording":[\s\S]*controller\.text\("录音中", "Recording"\)/)
  assert.match(calls, /ModemDeckDialerPanel/)
  assert.match(settings, /从本机通讯录导入/)
  assert.match(settings, /发送测试来电/)
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

test('native home omits overview and recent activity heading rows', async () => {
  const builder = await source('scripts/build-web-preview.mjs')

  assert.match(builder, /v-if=\"false\"[\s\S]*dashboard-overview-row/)
  assert.match(builder, /v-if=\"false\" class=\"dashboard-list-label\"/)
})

test('native home uses compact line cards instead of summary counters', async () => {
  const [app, home] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'), source('ios/App/App/ModemDeckHomeView.swift')
  ])
  assert.match(home, /ModemDeckHomeLineGrid\([\s\S]*lines: controller\.bootstrap\?\.lines \?\? \[\]/)
  assert.match(app, /private struct ModemDeckHomeLineCard/)
  assert.match(app, /GridItem\(\.adaptive\(minimum: 168, maximum: 280\), spacing: 8\)/)
  assert.doesNotMatch(home, /ModemDeckPageHeader|最近活动|Recent Activity/)
  assert.doesNotMatch(app + home, /ModemDeckHomeSummaryGrid|unreadMessageCount|missedCallCount|onlineLineCount/)
})

test('native contact import is injected into Settings Contacts only', async () => {
  const [builder, button] = await Promise.all([
    source('scripts/build-web-preview.mjs'),
    source('assets/NativeContactImportButton.vue')
  ])

  assert.match(builder, /ContactSyncSettings\.vue/)
  assert.doesNotMatch(builder, /ContactsView\.vue/)
  assert.match(button, /class=\"primary-button native-contact-import\"/)
  assert.match(button, /contacts\.importFromIPhone/)
})

test('native WebView prevents double-tap and input focus zoom', async () => {
  const [builder, styles, capacitorConfig] = await Promise.all([
    source('scripts/build-web-preview.mjs'),
    source('assets/native-ios.css'),
    source('capacitor.config.json')
  ])

  assert.match(builder, /maximum-scale=1\.0, user-scalable=no/)
  assert.match(styles, /touch-action: manipulation/)
  assert.match(styles, /input,[\s\S]*font-size: 16px !important/)
  assert.equal(JSON.parse(capacitorConfig).ios.zoomEnabled, false)
})

test('native preview injects iPhone settings and the packaged app version', async () => {
  const builder = await source('scripts/build-web-preview.mjs')

  assert.match(builder, /NativeIOSSettingsPanel/)
  assert.match(builder, /readNativeIOSAppInfo/)
  assert.match(builder, /nativeAppVersion\.value/)
})

test('native notification permission and delivery remain wired during migration', async () => {
  const [bridge, panel, delegate, scene, swift, session, capacitorConfig] = await Promise.all([
    source('assets/native-ios.ts'),
    source('assets/NativeIOSSettingsPanel.vue'),
    source('ios/App/App/AppDelegate.swift'),
    source('ios/App/App/SceneDelegate.swift'),
    source('ios/App/App/ModemDeckNative.swift'),
    source('ios/App/App/ModemDeckSession.swift'),
    source('capacitor.config.json')
  ])

  for (const method of [
    'notificationStatus',
    'requestNotificationPermission',
    'openNotificationSettings',
    'showNotification'
  ]) {
    assert.match(bridge, new RegExp(`\\b${method}\\b`))
    assert.match(swift, new RegExp(`\\b${method}\\b`))
  }
  assert.match(panel, /showNativeIOSNotification/)
  assert.match(panel, /sendTestSMSNotification/)
  assert.match(panel, /testNotificationScheduled/)
  assert.match(swift, /UNTimeIntervalNotificationTrigger/)
  assert.equal(
    JSON.parse(capacitorConfig).ios.handleApplicationNotifications,
    false
  )
  assert.match(swift, /#if DEBUG[\s\S]*installModemDeckUATCredentialFromEnvironment/)
  assert.match(delegate, /#if DEBUG[\s\S]*installModemDeckUATCredentialFromEnvironment/)
  assert.match(scene, /AppDelegate\)\?\.credentialStore/)
  assert.match(scene, /UIHostingController/)
  assert.match(session, /ModemDeckPushCoordinator\.shared\.configure\(store: credentialStore\)/)
  assert.match(session, /scheduleLocalTestNotification/)
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

test('native tabs lazily retain an independent NavigationStack per visited module', async () => {
  const app = await source('ios/App/App/ModemDeckApp.swift')

  assert.match(app, /struct ModemDeckSectionRoot[\s\S]*NavigationStack/)
  assert.match(app, /struct ModemDeckLazySectionHost: UIViewControllerRepresentable/)
  assert.match(app, /var sections: \[ModemDeckSection: UIHostingController<ModemDeckSectionRoot>\]/)
  assert.match(app, /if let existing = sections\[section\] \{ return existing \}/)
  assert.match(app, /container\.show\([\s\S]*sectionController\(for: activeSection/)
  assert.match(app, /\.animation\(nil, value: activeSection\)/)
  assert.match(app, /\.onChange\(of: activeSection\)[\s\S]*resignFirstResponder/)
  assert.equal((app.match(/ModemDeckSectionTabs\(controller: controller\)/g) || []).length, 1)
  assert.doesNotMatch(app, /ForEach\(ModemDeckSection\.contentSections\)[\s\S]*\.opacity\(/)
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
  assert.match(session, /session = api\.cachedMobileSession\(\)[\s\S]*bootstrap = api\.cachedBootstrap\(\)[\s\S]*phase = \.paired/)
  assert.match(session, /connectionState = \.offline[\s\S]*phase = \.paired/)
  assert.match(session, /contacts = api\.cachedContacts\(\)/)
  assert.match(session, /threads = api\.cachedMessageThreads\(\)/)
  assert.match(session, /messages = api\.cachedMessages/)
  assert.match(session, /calls = api\.cachedCalls\(\)[\s\S]*recordings = api\.cachedRecordings\(\)/)
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

test('native archives generate a UUID-checked WebRTC dSYM', async () => {
  const [project, script] = await Promise.all([
    source('ios/App/App.xcodeproj/project.pbxproj'),
    source('scripts/generate-webrtc-dsym.sh')
  ])

  assert.match(project, /Generate WebRTC dSYM/)
  assert.match(project, /scripts\/generate-webrtc-dsym\.sh/)
  assert.match(project, /DWARF_DSYM_FOLDER_PATH.*WebRTC\.framework\.dSYM/)
  assert.match(script, /ACTION:-.*install/)
  assert.match(script, /xcrun dsymutil/)
  assert.ok((script.match(/xcrun dwarfdump --uuid/g) || []).length >= 2)
  assert.match(script, /binary_uuids.*!=.*dsym_uuids/)
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
  assert.match(app, /static let listRowMinHeight: CGFloat = 66/)
  assert.match(app, /struct ModemDeckSearchField[\s\S]*\.font\(\.subheadline\)/)
  assert.match(app, /struct ModemDeckSegmentPicker[\s\S]*\.font\(\.caption\.weight/)
  assert.match(app, /accessibilityAddTraits\(active \? \.isSelected : \[\]\)/)
  assert.ok((communication.match(/\.modemDeckListToolbar/g) || []).length >= 2)
  assert.ok((calls.match(/\.modemDeckListToolbar/g) || []).length >= 2)
  assert.match(settings, /ModemDeckSettingsDirectoryRow[\s\S]*ModemDeckLayout\.listRowMinHeight/)
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
  assert.match(app, /func modemDeckInteractiveBack/)
  assert.match(communication, /\.modemDeckInteractiveBack\(showsBackButton\)/)
  assert.match(communication, /\.modemDeckInteractiveBack\(\)/)
  assert.match(calls, /\.modemDeckInteractiveBack\(showsBackButton\)/)
  assert.match(settings, /ModemDeckSettingsDetailScaffold[\s\S]*\.modemDeckInteractiveBack\(showsBackButton\)/)
})

test('native iPad communication workspaces mirror the Web list-detail geometry', async () => {
  const [app, communication, calls] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckCommunicationViews.swift'),
    source('ios/App/App/ModemDeckCallViews.swift')
  ])

  assert.match(app, /static let padListWidth: CGFloat = 340/)
  assert.match(app, /static let splitWorkspaceMinimumWidth: CGFloat = 900/)
  assert.match(app, /UIDevice\.current\.userInterfaceIdiom == \.pad/)
  assert.match(app, /ModemDeckPadNavigationRail/)
  assert.match(app, /floating: true[\s\S]*\.frame\(width: 390, height: 700\)/)
  assert.match(communication, /selectedContactID/)
  assert.match(communication, /selectedThreadID/)
  assert.ok(
    (communication.match(/\.frame\(width: ModemDeckLayout\.padListWidth\)/g) || []).length >= 2
  )
  assert.ok(
    (communication.match(/showsBackButton: false/g) || []).length >= 2
  )
  assert.match(calls, /selectedCallID/)
  assert.match(calls, /selectedRecordingID/)
  assert.ok(
    (calls.match(/\.frame\(width: ModemDeckLayout\.padListWidth\)/g) || []).length >= 2
  )
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

test('native dialer uses true circular keys and Web-style fixed actions', async () => {
  const calls = await source('ios/App/App/ModemDeckCallViews.swift')

  assert.match(calls, /ModemDeckDialerPanelShape/)
  assert.match(calls, /roundsAllCorners: floating/)
  assert.match(calls, /let compact = geometry\.size\.height < 620/)
  assert.match(calls, /let keySize: CGFloat = compact \? 56 : 62/)
  assert.match(calls, /ModemDeckDialKeyButton\([\s\S]*size: keySize/)
  assert.match(calls, /ModemDeckDialKeyStyle[\s\S]*\.clipShape\(Circle\(\)\)/)
  assert.match(calls, /\.scaleEffect\(configuration\.isPressed \? 0\.9 : 1\)/)
  assert.match(calls, /LongPressGesture\(minimumDuration: 0\.5\)/)
  assert.match(calls, /ModemDeckDTMFTonePlayer\.shared\.play\(digit\)/)
  assert.match(calls, /UIImpactFeedbackGenerator/)
  assert.match(calls, /private var suggestions: \[ModemDeckDialSuggestion\]/)
  assert.match(calls, /normalizeModemDeckDialTarget/)
  assert.match(calls, /controller\.api\.recordingSettings\(\)/)
  assert.match(calls, /controller\.text\("通话线路", "Calling line"\)/)
  assert.match(calls, /controller\.text\("默认", "Default"\)/)
  assert.match(calls, /controller\.text\("录音", "Record"\)/)
  assert.match(calls, /controller\.text\("拨打", "Call"\)/)
  assert.match(calls, /ModemDeckInCallKeyStyle[\s\S]*\.clipShape\(Circle\(\)\)/)
  assert.match(calls, /pressedDigits: dtmfDigits/)
  assert.match(calls, /\.frame\(width: 238\)/)
  assert.doesNotMatch(calls, /\.frame\(width: 66, height: 58\)/)
})

test('CallKit foreground reconciliation opens the native active-call surface', async () => {
  const [app, session, native] = await Promise.all([
    source('ios/App/App/ModemDeckApp.swift'),
    source('ios/App/App/ModemDeckSession.swift'),
    source('ios/App/App/ModemDeckNative.swift')
  ])

  assert.match(app, /if let call = callController\.call[\s\S]*ModemDeckActiveCallView/)
  assert.match(app, /UIApplication\.didBecomeActiveNotification[\s\S]*callController\.refreshFromCallKit\(\)/)
  assert.match(session, /func refreshFromCallKit\(\)[\s\S]*currentCallState/)
  assert.match(native, /answerRequestedCallUUIDs\.insert\(uuid\)\n\s*publishCallState\(\)/)
  assert.match(native, /state = "connecting"/)
})

test('native call reconciliation consumes only newer state for the exact call', async () => {
  const native = await source('ios/App/App/ModemDeckNative.swift')

  assert.match(native, /guard eventName == "call_state"/)
  assert.match(native, /runtime\/events\?call_id=\\\(encodedCallID\)/)
  assert.match(native, /guard state\.id == callID,[\s\S]*state\.revision > self\.runtimeCallRevision/)
  assert.doesNotMatch(native, /state\.revision >= self\.runtimeCallRevision/)
})

test('unresolved native writes retain one operation identity across ambiguous responses', async () => {
  const api = await source('ios/App/App/ModemDeckAPI.swift')

  assert.match(api, /An unresolved operation identity must survive indefinitely/)
  assert.doesNotMatch(api, /retentionInterval|updatedAt <|addingTimeInterval/)
  assert.match(api, /status == 408 \|\| status == 409 \|\| status == 425/)
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
  assert.match(communication, /actionIcon: "square\.and\.pencil"/)
  assert.match(communication, /private var messageBatchBar/)
  assert.match(communication, /mutateSelectedThreads\(hasUnread \? \.read : \.unread\)/)
  assert.match(communication, /mutateSelectedThreads\(allFavorite \? \.unfavorite : \.favorite\)/)
  assert.match(
    communication,
    /private var selectedContacts:[\s\S]*?filteredContacts\.filter \{ selectedIDs\.contains\(\$0\.id\) \}/
  )
  assert.match(
    communication,
    /private var selectedThreads:[\s\S]*?filteredThreads\.filter \{ selectedIDs\.contains\(\$0\.id\) \}/
  )
  assert.match(calls, /private var callBatchBar/)
  assert.match(calls, /private var recordingBatchBar/)
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
  assert.match(home, /ForEach\(items\)/)
  assert.match(home, /\.refreshable \{ await controller\.refresh\(\) \}/)
  assert.match(home, /await messages\.mutate\(action, threads: \[thread\]\)/)
  assert.match(home, /await calls\.mutate\(action, calls: \[call\]\)/)
  assert.match(session, /recordings\.removeAll \{ ids\.contains\(\$0\.call\.id\) \}/)
  assert.match(session, /messagesStore\.apply\(\.read, to: \[thread\.id\]\)/)
})
