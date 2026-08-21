export function nativeResponseBody(
  status: number,
  method: string,
  body: BodyInit
): BodyInit | null {
  if (
    method.toUpperCase() === 'HEAD' ||
    status === 204 ||
    status === 205 ||
    status === 304
  ) {
    return null
  }
  return body
}
