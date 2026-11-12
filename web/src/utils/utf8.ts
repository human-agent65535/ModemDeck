/** Size of a string once sent as UTF-8, which is what server byte limits count. */
export function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength
}
