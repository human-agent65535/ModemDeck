import { spawn } from 'node:child_process'
import { access } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const iosRoot = path.dirname(path.dirname(fileURLToPath(import.meta.url)))
const project = path.join(iosRoot, 'ios/App/App.xcodeproj')
const derivedData = path.join(iosRoot, 'DerivedData')
const developerDirectory = '/Applications/Xcode-beta.app/Contents/Developer'

function run(command, args) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: iosRoot,
      env: { ...process.env, DEVELOPER_DIR: developerDirectory },
      stdio: 'inherit'
    })
    child.once('error', reject)
    child.once('exit', code => {
      if (code === 0) resolve()
      else reject(new Error(`${command} exited with status ${code}`))
    })
  })
}

await access(project)
await run('xcodebuild', [
  '-project', project,
  '-scheme', 'App',
  '-configuration', 'Debug',
  '-sdk', 'iphonesimulator',
  '-destination', 'generic/platform=iOS Simulator',
  '-derivedDataPath', derivedData,
  'build'
])
