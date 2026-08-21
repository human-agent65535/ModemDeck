import { spawn } from 'node:child_process'
import { access } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const iosRoot = path.dirname(path.dirname(fileURLToPath(import.meta.url)))
const developerDirectory = '/Applications/Xcode-beta.app/Contents/Developer'
const simulatorApplications = [
  path.join(
    path.dirname(developerDirectory),
    'Applications/DeviceHub.app'
  ),
  path.join(developerDirectory, 'Applications/Simulator.app')
]
const applicationPath = path.join(
  iosRoot,
  'DerivedData/Build/Products/Debug-iphonesimulator/App.app'
)

function run(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: options.cwd ?? iosRoot,
      env: { ...process.env, DEVELOPER_DIR: developerDirectory },
      stdio: options.capture ? ['ignore', 'pipe', 'inherit'] : 'inherit'
    })
    let output = ''
    if (options.capture) {
      child.stdout.setEncoding('utf8')
      child.stdout.on('data', chunk => { output += chunk })
    }
    child.once('error', reject)
    child.once('exit', code => {
      if (code === 0 || options.allowFailure) resolve(output)
      else reject(new Error(`${command} exited with status ${code}`))
    })
  })
}

const devicesJSON = await run(
  'xcrun',
  ['simctl', 'list', 'devices', 'available', '--json'],
  { capture: true }
)
const runtimes = JSON.parse(devicesJSON).devices
const requestedFamily = (process.env.MODEMDECK_SIMULATOR_FAMILY || 'iphone').toLowerCase()
if (!['iphone', 'ipad'].includes(requestedFamily)) {
  throw new Error('MODEMDECK_SIMULATOR_FAMILY must be iphone or ipad')
}
const familyPrefix = requestedFamily === 'ipad' ? 'iPad' : 'iPhone'
const devices = Object.entries(runtimes)
  .flatMap(([runtime, runtimeDevices]) =>
    runtimeDevices.map(device => ({ ...device, runtime }))
  )
  .filter(device => device.isAvailable && device.name.startsWith(familyPrefix))

const preferredNames = requestedFamily === 'ipad'
  ? [
      'iPad Pro 13-inch (M5)',
      'iPad Pro 13-inch (M4)',
      'iPad Pro 12.9-inch (6th generation)',
      'iPad Pro 11-inch (M5)',
      'iPad Pro 11-inch (M4)',
      'iPad Air 13-inch (M3)',
      'iPad Air 13-inch (M2)'
    ]
  : [
      'iPhone 17 Pro Max',
      'iPhone 16 Pro Max',
      'iPhone 15 Pro Max',
      'iPhone 17 Pro',
      'iPhone 16 Pro',
      'iPhone 15 Pro',
      'iPhone 17',
      'iPhone 16',
      'iPhone 15'
    ]
const selectedDevice = devices
  .slice()
  .sort((left, right) => {
    const leftRank = preferredNames.indexOf(left.name)
    const rightRank = preferredNames.indexOf(right.name)
    const normalizedLeft = leftRank === -1 ? preferredNames.length : leftRank
    const normalizedRight = rightRank === -1 ? preferredNames.length : rightRank
    if (normalizedLeft !== normalizedRight) return normalizedLeft - normalizedRight
    return right.runtime.localeCompare(left.runtime)
  })[0]

if (!selectedDevice) throw new Error(`No available ${familyPrefix} simulator was found`)

if (selectedDevice.state !== 'Booted') {
  await run('xcrun', ['simctl', 'boot', selectedDevice.udid])
}
for (const application of simulatorApplications) {
  try {
    await access(application)
    await run('open', [application], { allowFailure: true })
    break
  } catch {
    // A simulator UI is optional; simctl still installs and launches the app.
  }
}
await run('xcrun', ['simctl', 'bootstatus', selectedDevice.udid, '-b'])
await run('npm', ['run', 'build:ios'])
const bundleIdentifier = (
  await run(
    '/usr/bin/plutil',
    ['-extract', 'CFBundleIdentifier', 'raw', '-o', '-', path.join(applicationPath, 'Info.plist')],
    { capture: true }
  )
).trim()
if (!bundleIdentifier) {
  throw new Error('The built iOS application has no bundle identifier')
}
await run('xcrun', ['simctl', 'install', selectedDevice.udid, applicationPath])
await run(
  'xcrun',
  ['simctl', 'terminate', selectedDevice.udid, bundleIdentifier],
  { allowFailure: true }
)
await run('xcrun', ['simctl', 'launch', selectedDevice.udid, bundleIdentifier])

process.stdout.write(
  `Launched ${bundleIdentifier} on ${selectedDevice.name} (${selectedDevice.udid})\n`
)
