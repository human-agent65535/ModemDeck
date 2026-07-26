<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { LoaderCircle, LogIn } from '@lucide/vue'
import { login, sessionState } from '../state/session'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const username = ref('')
const password = ref('')
const submitting = ref(false)
const submitError = ref('')
const visibleError = computed(() => submitError.value || sessionState.error)

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
    submitError.value = t('auth.enterCredentials')
    return
  }

  submitting.value = true
  submitError.value = ''
  try {
    await login(normalizedUsername, password.value)
    await router.replace(destination())
  } catch (error) {
    submitError.value = error instanceof Error ? error.message : t('auth.loginFailed')
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
        <h1 id="login-title">{{ t('auth.title') }}</h1>
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
            autocomplete="current-password"
            :disabled="submitting"
          />
        </label>

        <p v-if="visibleError" class="login-error" role="alert">{{ visibleError }}</p>

        <button class="primary-button login-submit" type="submit" :disabled="submitting">
          <LoaderCircle v-if="submitting" class="spin" :size="18" />
          <LogIn v-else :size="18" />
          {{ submitting ? t('auth.loggingIn') : t('auth.login') }}
        </button>
      </form>
    </section>
  </main>
</template>
