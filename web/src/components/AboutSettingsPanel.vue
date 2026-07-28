<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertCircle,
  BadgeCheck,
  CheckCircle2,
  Code2,
  ExternalLink,
  FileText,
  Info,
  LoaderCircle,
  Scale
} from '@lucide/vue'
import { gateway } from '../api/client'
import type { AboutInfo, UpdateCheck, UpdateStatus } from '../api/types'

const { locale, t } = useI18n()
const repositoryURL = 'https://github.com/human-agent65535/ModemDeck'
const licenseURL = `${repositoryURL}/blob/modemdeck/LICENSE`
const noticesURL = `${repositoryURL}/blob/modemdeck/THIRD_PARTY_NOTICES.md`
const licenseName = 'PolyForm Noncommercial 1.0.0'
const about = ref<AboutInfo | null>(null)
const update = ref<UpdateCheck | null>(null)
const checking = ref(false)
const loadError = ref('')

const statusIcon = computed(() => {
  switch (update.value?.status) {
    case 'up_to_date':
      return CheckCircle2
    case 'update_available':
      return BadgeCheck
    case 'development':
      return Info
    default:
      return AlertCircle
  }
})

const statusTitle = computed(() => {
  if (checking.value && !update.value) return t('about.checkingTitle')
  return t(`about.updateStatus.${update.value?.status || 'unavailable'}.title`)
})

const statusDescription = computed(() => {
  if (checking.value && !update.value) return t('about.checkingDescription')
  return t(`about.updateStatus.${update.value?.status || 'unavailable'}.description`, {
    current: update.value?.current_version || about.value?.version || '—',
    latest: update.value?.latest_version || '—'
  })
})

function formatDate(value?: string): string {
  if (!value || value === 'unknown') return t('about.notAvailable')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  }).format(date)
}

function compactCommit(value?: string): string {
  return !value || value === 'unknown' ? t('about.notAvailable') : value.slice(0, 12)
}

async function checkForUpdates(): Promise<void> {
  if (checking.value) return
  checking.value = true
  try {
    update.value = await gateway.checkForUpdates()
  } catch {
    update.value = {
      status: 'unavailable',
      current_version: about.value?.version || '—',
      checked_at: new Date().toISOString()
    }
  } finally {
    checking.value = false
  }
}

async function load(): Promise<void> {
  loadError.value = ''
  try {
    about.value = await gateway.getAbout()
  } catch (error) {
    loadError.value =
      error instanceof Error && error.message ? error.message : t('about.loadFailed')
  }
}

function statusClass(status?: UpdateStatus): string {
  if (checking.value && !update.value) return 'about-status--checking'
  return status ? `about-status--${status}` : 'about-status--unavailable'
}

onMounted(() => {
  void load()
  void checkForUpdates()
})
</script>

<template>
  <section class="about-settings" aria-labelledby="about-product-title">
    <section class="about-card about-product">
        <header class="about-product__header">
          <span class="about-product__mark" aria-hidden="true">M</span>
          <div>
            <h3 id="about-product-title">{{ about?.name || 'ModemDeck' }}</h3>
            <p>{{ t('about.productDescription') }}</p>
          </div>
          <span class="about-version">{{ about?.version || '—' }}</span>
        </header>

        <p v-if="loadError" class="about-product__error" role="alert">{{ loadError }}</p>

        <dl class="about-facts">
          <div>
            <dt>{{ t('about.version') }}</dt>
            <dd>{{ about?.version || t('about.notAvailable') }}</dd>
          </div>
          <div>
            <dt>{{ t('about.commit') }}</dt>
            <dd><code>{{ compactCommit(about?.commit) }}</code></dd>
          </div>
          <div>
            <dt>{{ t('about.buildDate') }}</dt>
            <dd>{{ formatDate(about?.build_date) }}</dd>
          </div>
          <div>
            <dt>{{ t('about.sourceCode') }}</dt>
            <dd>
              <a
                :href="about?.repository_url || repositoryURL"
                target="_blank"
                rel="noopener noreferrer"
              >
                <Code2 :size="14" />
                GitHub
                <ExternalLink :size="12" />
              </a>
            </dd>
          </div>
        </dl>
    </section>

    <section class="about-card about-update">
        <header class="about-card__header">
          <div>
            <h3>{{ t('about.updateTitle') }}</h3>
            <p>{{ t('about.updateDescription') }}</p>
          </div>
        </header>

        <div class="about-status" :class="statusClass(update?.status)">
          <LoaderCircle v-if="checking" class="spin" :size="21" />
          <component :is="statusIcon" v-else :size="21" />
          <div>
            <strong>{{ statusTitle }}</strong>
            <p>{{ statusDescription }}</p>
            <small v-if="update?.checked_at">
              {{ t('about.checkedAt', { date: formatDate(update.checked_at) }) }}
            </small>
          </div>
          <a
            v-if="update?.release_url"
            class="about-status__link"
            :href="update.release_url"
            target="_blank"
            rel="noopener noreferrer"
          >
            {{ t('about.viewRelease') }}
            <ExternalLink :size="13" />
          </a>
        </div>

    </section>

    <section class="about-card about-legal">
        <header class="about-card__header">
          <div>
            <h3>{{ t('about.legalTitle') }}</h3>
            <p>{{ t('about.legalDescription') }}</p>
          </div>
        </header>

        <div class="about-legal__links">
          <a
            :href="about?.license_url || licenseURL"
            target="_blank"
            rel="noopener noreferrer"
          >
            <span class="about-legal__icon"><Scale :size="18" /></span>
            <span>
              <strong>{{ t('about.projectLicense') }}</strong>
              <small>{{ about?.license_name || licenseName }}</small>
            </span>
            <ExternalLink :size="14" />
          </a>
          <a
            :href="about?.notices_url || noticesURL"
            target="_blank"
            rel="noopener noreferrer"
          >
            <span class="about-legal__icon"><FileText :size="18" /></span>
            <span>
              <strong>{{ t('about.thirdPartyNotices') }}</strong>
              <small>{{ t('about.thirdPartySummary') }}</small>
            </span>
            <ExternalLink :size="14" />
          </a>
        </div>
    </section>
  </section>
</template>

<style scoped>
.about-settings {
  display: grid;
  gap: 14px;
}

.about-card {
  padding: 18px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 10px;
}

.about-card__header,
.about-product__header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.about-card__header > div,
.about-product__header > div {
  min-width: 0;
  flex: 1;
}

.about-card h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
  text-transform: none;
}

.about-card__header p,
.about-product__header p {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.45;
}

.about-product__mark {
  display: grid;
  width: 42px;
  height: 42px;
  flex: 0 0 42px;
  place-items: center;
  color: #fff;
  font-size: 21px;
  font-weight: 750;
  background: linear-gradient(145deg, var(--accent), var(--accent-strong));
  border-radius: 11px;
  box-shadow: 0 6px 18px rgb(11 120 102 / 18%);
}

.about-version {
  padding: 5px 9px;
  color: var(--accent-strong);
  font-size: 12px;
  font-weight: 700;
  background: var(--accent-soft);
  border: 1px solid var(--accent);
  border-radius: 999px;
}

.about-product__error {
  margin: 12px 0 0;
  color: var(--danger);
  font-size: 11px;
}

.about-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0;
  margin: 16px 0 0;
  border-top: 1px solid var(--border);
}

.about-facts > div {
  min-width: 0;
  padding: 12px 0 0;
}

.about-facts dt {
  color: var(--muted);
  font-size: 11px;
}

.about-facts dd {
  margin: 4px 0 0;
  color: var(--text);
  font-size: 12px;
}

.about-facts a {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--accent-strong);
}

.about-status {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin-top: 14px;
  padding: 13px;
  color: var(--muted);
  background: var(--surface-subtle);
  border-left: 3px solid var(--border-strong);
  border-radius: 7px;
}

.about-status > svg {
  flex: 0 0 auto;
  margin-top: 1px;
}

.about-status > div {
  min-width: 0;
  flex: 1;
}

.about-status strong {
  color: var(--text);
  font-size: 13px;
}

.about-status p {
  margin: 3px 0 0;
  font-size: 12px;
  line-height: 1.45;
}

.about-status small {
  display: block;
  margin-top: 5px;
  color: var(--faint);
  font-size: 10px;
}

.about-status--up_to_date {
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-left-color: var(--accent);
}

.about-status--checking {
  color: var(--muted);
  background: var(--surface-subtle);
  border-left-color: var(--border-strong);
}

.about-status--update_available {
  color: #8a5b00;
  background: #fff8e8;
  border-left-color: #d99a18;
}

.about-status--unavailable {
  color: var(--danger);
  background: var(--danger-soft);
  border-left-color: var(--danger);
}

.about-status__link {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 4px;
  color: currentColor;
  font-size: 11px;
  font-weight: 700;
}

.about-legal__links {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin-top: 14px;
}

.about-legal__links > a {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
  padding: 12px;
  color: var(--text);
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 8px;
}

.about-legal__links > a:hover {
  border-color: var(--accent);
}

.about-legal__links > a > span:nth-child(2) {
  min-width: 0;
  flex: 1;
}

.about-legal__links strong,
.about-legal__links small {
  display: block;
}

.about-legal__links strong {
  font-size: 12px;
}

.about-legal__links small {
  overflow: hidden;
  margin-top: 3px;
  color: var(--muted);
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.about-legal__icon {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 7px;
}

@media (max-width: 720px) {
  .about-card__header {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .about-facts,
  .about-legal__links {
    grid-template-columns: 1fr;
  }

  .about-status {
    flex-wrap: wrap;
  }

  .about-status__link {
    width: 100%;
    margin-left: 31px;
  }
}
</style>
