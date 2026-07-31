export const minimumPasswordCharacters = 8
export const maximumPasswordBytes = 1024

export function passwordCharacterCount(value: string): number {
  return Array.from(value).length
}

export function passwordByteCount(value: string): number {
  return new TextEncoder().encode(value).length
}
