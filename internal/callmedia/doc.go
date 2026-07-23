// Package callmedia bridges one active consumer call between a browser
// WebRTC peer and a host-owned PCM media endpoint.
//
// Opus is the only network codec. Signed 16-bit little-endian PCM exists only
// at the MediaEndpoint boundary and is never exposed as a browser transport.
package callmedia
