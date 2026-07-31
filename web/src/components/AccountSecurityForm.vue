<script setup lang="ts">
import { computed, ref } from 'vue'
import { Eye, EyeOff, KeyRound, LoaderCircle, Save } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ApiError } from '../api/types'
import { changePassword } from '../state/session'
import {
  maximumPasswordBytes,
  minimumPasswordCharacters,
  passwordByteCount,
  passwordCharacterCount
} from '../utils/password'

const router = useRouter()
const { t } = useI18n()
const currentPassword = ref('')
const newPassword = ref('')
const confirmation = ref('')
const showCurrent = ref(false)
const showNew = ref(false)
const saving = ref(false)
const error = ref('')

const canSubmit = computed(
  () =>
    Boolean(currentPassword.value && newPassword.value && confirmation.value) &&
    !saving.value
)

function validationError(): string {
  if (!currentPassword.value || !newPassword.value || !confirmation.value) {
    return t('account.completePasswordFields')
  }
  if (passwordCharacterCount(newPassword.value) < minimumPasswordCharacters) {
    return t('account.passwordTooShort', { count: minimumPasswordCharacters })
  }
  if (passwordByteCount(newPassword.value) > maximumPasswordBytes) {
    return t('account.passwordTooLong', { count: maximumPasswordBytes })
  }
  if (newPassword.value.includes('\0')) {
    return t('account.passwordInvalid')
  }
  if (newPassword.value === currentPassword.value) {
    return t('account.passwordUnchanged')
  }
  if (newPassword.value !== confirmation.value) {
    return t('account.passwordMismatch')
  }
  return ''
}

function apiErrorMessage(cause: unknown): string {
  if (!(cause instanceof ApiError)) {
    return cause instanceof Error ? cause.message : t('account.passwordChangeFailed')
  }
  const messages: Record<string, string> = {
    invalid_current_password: t('account.currentPasswordIncorrect'),
    password_too_short: t('account.passwordTooShort', {
      count: minimumPasswordCharacters
    }),
    password_too_long: t('account.passwordTooLong', { count: maximumPasswordBytes }),
    password_invalid: t('account.passwordInvalid'),
    password_unchanged: t('account.passwordUnchanged')
  }
  return (cause.code && messages[cause.code]) || cause.message
}

async function submit(): Promise<void> {
  const invalid = validationError()
  if (invalid) {
    error.value = invalid
    return
  }

  saving.value = true
  error.value = ''
  try {
    await changePassword({
      current_password: currentPassword.value,
      new_password: newPassword.value
    })
    currentPassword.value = ''
    newPassword.value = ''
    confirmation.value = ''
    await router.replace({ name: 'login', query: { passwordChanged: '1' } })
  } catch (cause) {
    error.value = apiErrorMessage(cause)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="account-security" aria-labelledby="account-password-title">
    <header>
      <span class="account-security__icon"><KeyRound :size="20" /></span>
      <div>
        <h3 id="account-password-title">{{ t('account.changePassword') }}</h3>
        <p>{{ t('account.changePasswordDescription') }}</p>
      </div>
    </header>

    <form class="account-security__form" @submit.prevent="submit">
      <label class="field">
        <span>{{ t('account.currentPassword') }}</span>
        <span class="password-input">
          <input
            v-model="currentPassword"
            :type="showCurrent ? 'text' : 'password'"
            autocomplete="current-password"
            :disabled="saving"
          />
          <button
            class="icon-button"
            type="button"
            :title="showCurrent ? t('account.hidePassword') : t('account.showPassword')"
            :aria-label="showCurrent ? t('account.hidePassword') : t('account.showPassword')"
            :disabled="saving"
            @click="showCurrent = !showCurrent"
          >
            <EyeOff v-if="showCurrent" :size="18" />
            <Eye v-else :size="18" />
          </button>
        </span>
      </label>

      <label class="field">
        <span>{{ t('account.newPassword') }}</span>
        <span class="password-input">
          <input
            v-model="newPassword"
            :type="showNew ? 'text' : 'password'"
            autocomplete="new-password"
            :disabled="saving"
          />
          <button
            class="icon-button"
            type="button"
            :title="showNew ? t('account.hidePassword') : t('account.showPassword')"
            :aria-label="showNew ? t('account.hidePassword') : t('account.showPassword')"
            :disabled="saving"
            @click="showNew = !showNew"
          >
            <EyeOff v-if="showNew" :size="18" />
            <Eye v-else :size="18" />
          </button>
        </span>
      </label>

      <label class="field">
        <span>{{ t('account.confirmPassword') }}</span>
        <input
          v-model="confirmation"
          :type="showNew ? 'text' : 'password'"
          autocomplete="new-password"
          :disabled="saving"
        />
      </label>

      <p v-if="error" class="account-security__feedback" role="alert">{{ error }}</p>

      <button class="primary-button account-security__submit" type="submit" :disabled="!canSubmit">
        <LoaderCircle v-if="saving" class="spin" :size="18" />
        <Save v-else :size="18" />
        {{ saving ? t('account.changingPassword') : t('account.changePassword') }}
      </button>
    </form>
  </section>
</template>

<style scoped>
.account-security {
  max-width: 680px;
}

.account-security > header {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 11px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.account-security__icon {
  display: inline-grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.account-security h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
  text-transform: none;
}

.account-security header p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 11px;
}

.account-security__form {
  display: grid;
  max-width: 520px;
  gap: 14px;
  padding-top: 18px;
}

.password-input {
  position: relative;
  display: block;
}

.password-input input {
  padding-right: 46px;
}

.password-input .icon-button {
  position: absolute;
  top: 50%;
  right: 4px;
  transform: translateY(-50%);
}

.account-security__feedback {
  margin: 0;
  color: var(--danger);
  font-size: 12px;
  line-height: 1.4;
}

.account-security__submit {
  width: fit-content;
  min-width: 142px;
}
</style>
