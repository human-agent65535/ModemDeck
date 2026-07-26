const MAX_SOURCE_BYTES = 10 << 20
const MAX_AVATAR_BYTES = 256 << 10
const OUTPUT_SIZES = [384, 320, 256, 192]
const OUTPUT_QUALITIES = [0.86, 0.8, 0.74, 0.68]

function loadImage(file: File): Promise<{ image: HTMLImageElement; release: () => void }> {
  const url = URL.createObjectURL(file)
  const image = new Image()
  image.decoding = 'async'
  return new Promise((resolve, reject) => {
    image.onload = () => resolve({ image, release: () => URL.revokeObjectURL(url) })
    image.onerror = () => {
      URL.revokeObjectURL(url)
      reject(new Error('无法读取这张图片'))
    }
    image.src = url
  })
}

function encodeCanvas(canvas: HTMLCanvasElement, quality: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      blob => {
        if (!blob) {
          reject(new Error('浏览器无法处理这张图片'))
          return
        }
        resolve(blob)
      },
      'image/webp',
      quality
    )
  })
}

function blobDataURL(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(new Error('无法读取处理后的头像'))
    reader.readAsDataURL(blob)
  })
}

export async function createContactAvatar(file: File): Promise<string> {
  if (!file.type.startsWith('image/')) throw new Error('请选择图片文件')
  if (file.size > MAX_SOURCE_BYTES) throw new Error('原图不能超过 10 MB')

  const { image, release } = await loadImage(file)
  try {
    const sourceWidth = image.naturalWidth
    const sourceHeight = image.naturalHeight
    if (!sourceWidth || !sourceHeight) throw new Error('图片尺寸无效')

    const cropSize = Math.min(sourceWidth, sourceHeight)
    const sourceX = Math.floor((sourceWidth - cropSize) / 2)
    const sourceY = Math.floor((sourceHeight - cropSize) / 2)
    const canvas = document.createElement('canvas')
    const context = canvas.getContext('2d')
    if (!context) throw new Error('浏览器无法处理这张图片')

    for (let index = 0; index < OUTPUT_SIZES.length; index += 1) {
      const size = Math.min(OUTPUT_SIZES[index] || 192, cropSize)
      canvas.width = size
      canvas.height = size
      context.clearRect(0, 0, size, size)
      context.drawImage(
        image,
        sourceX,
        sourceY,
        cropSize,
        cropSize,
        0,
        0,
        size,
        size
      )
      const blob = await encodeCanvas(canvas, OUTPUT_QUALITIES[index] || 0.68)
      if (blob.size <= MAX_AVATAR_BYTES) {
        const dataURL = await blobDataURL(blob)
        if (
          dataURL.startsWith('data:image/jpeg;base64,') ||
          dataURL.startsWith('data:image/png;base64,') ||
          dataURL.startsWith('data:image/webp;base64,')
        ) {
          return dataURL
        }
      }
    }
  } finally {
    release()
  }
  throw new Error('图片内容过于复杂，请选择更小的图片')
}
