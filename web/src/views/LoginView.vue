<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { LoaderCircle, LogIn, UserRoundPlus } from '@lucide/vue'
import { login, sessionState, setup as setupAdministrator } from '../state/session'
import {
  minimumPasswordCharacters,
  passwordCharacterCount
} from '../utils/password'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const username = ref('')
const password = ref('')
const confirmation = ref('')
const submitting = ref(false)
const submitError = ref('')
const isSetup = computed(() => sessionState.setupRequired)
const visibleError = computed(() => submitError.value || sessionState.error)
const visibleNotice = computed(() =>
  !isSetup.value && route.query.passwordChanged === '1' && !visibleError.value
    ? t('auth.passwordChanged')
    : ''
)

function destination(): string {
  const redirect = route.query.redirect
  if (
    typeof redirect === 'string' &&
    redirect.startsWith('/') &&
    !redirect.startsWith('//') &&
    redirect !== '/login'
  ) {
    return redirect
  }
  return '/contacts'
}

async function submit(): Promise<void> {
  const normalizedUsername = username.value.trim()
  if (!normalizedUsername || !password.value) {
    submitError.value = isSetup.value
      ? t('auth.enterSetupCredentials')
      : t('auth.enterCredentials')
    return
  }
  if (
    isSetup.value &&
    passwordCharacterCount(password.value) < minimumPasswordCharacters
  ) {
    submitError.value = t('auth.setupPasswordTooShort', {
      count: minimumPasswordCharacters
    })
    return
  }
  if (isSetup.value && password.value !== confirmation.value) {
    submitError.value = t('auth.passwordMismatch')
    return
  }

  submitting.value = true
  submitError.value = ''
  try {
    if (isSetup.value) {
      await setupAdministrator(normalizedUsername, password.value)
    } else {
      await login(normalizedUsername, password.value)
    }
    await router.replace(destination())
  } catch (error) {
    submitError.value =
      error instanceof Error
        ? error.message
        : isSetup.value
          ? t('auth.setupFailed')
          : t('auth.loginFailed')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <main class="login-page">
    <header class="login-brand" aria-label="ModemDeck">
      <span>M</span>
      <strong>ModemDeck</strong>
    </header>

    <section class="login-panel" aria-labelledby="login-title">
      <header>
        <h1 id="login-title">{{ isSetup ? t('auth.quickStart') : t('auth.title') }}</h1>
        <p v-if="isSetup">{{ t('auth.quickStartDescription') }}</p>
      </header>

      <form class="login-form" @submit.prevent="submit">
        <label class="field">
          <span>{{ t('auth.username') }}</span>
          <input
            v-model="username"
            name="username"
            type="text"
            autocomplete="username"
            autocapitalize="none"
            spellcheck="false"
            autofocus
            :disabled="submitting"
          />
        </label>

        <label class="field">
          <span>{{ t('auth.password') }}</span>
          <input
            v-model="password"
            name="password"
            type="password"
            :autocomplete="isSetup ? 'new-password' : 'current-password'"
            :disabled="submitting"
          />
        </label>

        <label v-if="isSetup" class="field">
          <span>{{ t('auth.confirmPassword') }}</span>
          <input
            v-model="confirmation"
            name="password-confirmation"
            type="password"
            autocomplete="new-password"
            :disabled="submitting"
          />
        </label>

        <p v-if="visibleError" class="login-error" role="alert">{{ visibleError }}</p>
        <p v-else-if="visibleNotice" class="login-notice" role="status">
          {{ visibleNotice }}
        </p>

        <button class="primary-button login-submit" type="submit" :disabled="submitting">
          <LoaderCircle v-if="submitting" class="spin" :size="18" />
          <UserRoundPlus v-else-if="isSetup" :size="18" />
          <LogIn v-else :size="18" />
          {{
            submitting
              ? isSetup
                ? t('auth.settingUp')
                : t('auth.loggingIn')
              : isSetup
                ? t('auth.completeSetup')
                : t('auth.login')
          }}
        </button>
      </form>
    </section>
  </main>
</template>
