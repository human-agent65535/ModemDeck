import type { InjectionKey } from 'vue'

export type NativeIOSMobileBackHandler = () => void

export const nativeIOSMobileBackKey: InjectionKey<NativeIOSMobileBackHandler> =
  Symbol('modemdeck-native-ios-mobile-back')
