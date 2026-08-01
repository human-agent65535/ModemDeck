import { readFile, readdir } from 'node:fs/promises'
import { gzipSync } from 'node:zlib'

const kibibyte = 1024
const limits = {
  initialRequests: 12,
  initialGzip: 230 * kibibyte,
  javascriptGzip: 100 * kibibyte,
  stylesheetGzip: 20 * kibibyte
}

const distURL = new URL('../dist/', import.meta.url)
const assetsURL = new URL('assets/', distURL)

function formatKibibytes(bytes) {
  return `${(bytes / kibibyte).toFixed(1)} KiB`
}

async function compressedSize(url) {
  return gzipSync(await readFile(url)).byteLength
}

const assetNames = await readdir(assetsURL)
const measuredAssets = await Promise.all(
  assetNames
    .filter(name => /\.(?:css|js)$/.test(name))
    .map(async name => ({
      name,
      type: name.endsWith('.css') ? 'css' : 'js',
      gzip: await compressedSize(new URL(name, assetsURL))
    }))
)

const indexHTML = await readFile(new URL('index.html', distURL), 'utf8')
const initialPaths = [
  ...indexHTML.matchAll(/(?:src|href)="([^"]+\.(?:css|js))"/g)
].map(match => match[1])
const uniqueInitialPaths = [...new Set(initialPaths)]
const initialGzip = (
  await Promise.all(
    uniqueInitialPaths.map(path => compressedSize(new URL(`.${path}`, distURL)))
  )
).reduce((total, bytes) => total + bytes, 0)

const lazyLocalePrefixes = [
  'de-DE',
  'es-ES',
  'fr-FR',
  'ja-JP',
  'pt-BR',
  'vi-VN',
  'zh-CN',
  'zh-TW'
]
const largestStartupLocale = measuredAssets
  .filter(
    asset =>
      asset.type === 'js' &&
      lazyLocalePrefixes.some(prefix => asset.name.startsWith(`${prefix}-`))
  )
  .sort((left, right) => right.gzip - left.gzip)[0]
const startupRequests = uniqueInitialPaths.length + (largestStartupLocale ? 1 : 0)
const startupGzip = initialGzip + (largestStartupLocale?.gzip || 0)

const largestJavaScript = measuredAssets
  .filter(asset => asset.type === 'js')
  .sort((left, right) => right.gzip - left.gzip)[0]
const largestStylesheet = measuredAssets
  .filter(asset => asset.type === 'css')
  .sort((left, right) => right.gzip - left.gzip)[0]

const violations = []
if (startupRequests > limits.initialRequests) {
  violations.push(
    `startup needs ${startupRequests} JS/CSS assets including one locale (limit ${limits.initialRequests})`
  )
}
if (startupGzip > limits.initialGzip) {
  violations.push(
    `worst-case startup JS/CSS is ${formatKibibytes(startupGzip)} gzip (limit ${formatKibibytes(limits.initialGzip)})`
  )
}
if (largestJavaScript && largestJavaScript.gzip > limits.javascriptGzip) {
  violations.push(
    `${largestJavaScript.name} is ${formatKibibytes(largestJavaScript.gzip)} gzip (JS limit ${formatKibibytes(limits.javascriptGzip)})`
  )
}
if (largestStylesheet && largestStylesheet.gzip > limits.stylesheetGzip) {
  violations.push(
    `${largestStylesheet.name} is ${formatKibibytes(largestStylesheet.gzip)} gzip (CSS limit ${formatKibibytes(limits.stylesheetGzip)})`
  )
}

console.log(
  `bundle budget: ${uniqueInitialPaths.length} shell assets, ${formatKibibytes(initialGzip)} gzip; ` +
    `${startupRequests} assets and ${formatKibibytes(startupGzip)} gzip with the largest startup locale; ` +
    `largest JS ${formatKibibytes(largestJavaScript?.gzip || 0)}, ` +
    `largest CSS ${formatKibibytes(largestStylesheet?.gzip || 0)}`
)

if (violations.length > 0) {
  throw new Error(`Bundle budget exceeded:\n- ${violations.join('\n- ')}`)
}
