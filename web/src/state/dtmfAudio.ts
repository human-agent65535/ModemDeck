import { audioState } from './audio'

const frequencies: Record<string, readonly [number, number]> = {
  '1': [697, 1209],
  '2': [697, 1336],
  '3': [697, 1477],
  '4': [770, 1209],
  '5': [770, 1336],
  '6': [770, 1477],
  '7': [852, 1209],
  '8': [852, 1336],
  '9': [852, 1477],
  '*': [941, 1209],
  '0': [941, 1336],
  '#': [941, 1477]
}

let audioContext: AudioContext | undefined

export function dtmfFrequencies(digit: string): readonly [number, number] | undefined {
  return frequencies[digit]
}

export async function playDTMFTone(digit: string): Promise<void> {
  const pair = dtmfFrequencies(digit)
  if (!pair) return
  const peak = 0.055 * (audioState.callVolume / 100)
  if (peak <= 0) return

  try {
    if (!audioContext || audioContext.state === 'closed') audioContext = new AudioContext()
    if (audioContext.state === 'suspended') await audioContext.resume()

    const start = audioContext.currentTime
    const stop = start + 0.12
    const gain = audioContext.createGain()
    gain.gain.setValueAtTime(0.0001, start)
    gain.gain.exponentialRampToValueAtTime(peak, start + 0.008)
    gain.gain.setValueAtTime(peak, start + 0.085)
    gain.gain.exponentialRampToValueAtTime(0.0001, stop)
    gain.connect(audioContext.destination)

    let remainingOscillators = pair.length
    for (const frequency of pair) {
      const oscillator = audioContext.createOscillator()
      oscillator.frequency.setValueAtTime(frequency, start)
      oscillator.connect(gain)
      oscillator.addEventListener(
        'ended',
        () => {
          oscillator.disconnect()
          remainingOscillators -= 1
          if (remainingOscillators === 0) gain.disconnect()
        },
        { once: true }
      )
      oscillator.start(start)
      oscillator.stop(stop)
    }
  } catch {
    // The modem command remains authoritative when local audio output is unavailable.
  }
}

export function shutdownDTMFAudio(): void {
  const context = audioContext
  audioContext = undefined
  if (context && context.state !== 'closed') void context.close()
}
