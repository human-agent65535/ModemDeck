<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertCircle,
  BadgeCheck,
  CheckCircle2,
  ChevronDown,
  Code2,
  ExternalLink,
  FileText,
  Info,
  RefreshCw,
  Scale
} from '@lucide/vue'
import { gateway } from '../api/client'
import type {
  AboutInfo,
  UpdateCheck,
  UpdateComponent,
  UpdateComponentName,
  UpdateOperation,
  UpdateOperationComponentState,
  UpdateStatus
} from '../api/types'
import { useInitialLoadBarrier } from '../composables/useInitialLoadBarrier'
import { requestConfirmation } from '../state/confirmation'
import { showError, showSuccess } from '../state/feedback'
import { releaseNoteRemainder, releaseNoteSummary } from '../utils/releaseNotes'
import SettingsLoadBoundary from './settings/SettingsLoadBoundary.vue'
import SettingsModuleCard from './settings/SettingsModuleCard.vue'

const { t } = useI18n()
const repositoryURL = 'https://github.com/human-agent65535/ModemDeck'
const licenseURL = `${repositoryURL}/blob/modemdeck/LICENSE`
const noticesURL = `${repositoryURL}/blob/modemdeck/THIRD_PARTY_NOTICES.md`
const licenseName = 'PolyForm Noncommercial 1.0.0'
const about = ref<AboutInfo | null>(null)
const update = ref<UpdateCheck | null>(null)
const checking = ref(false)
const applying = ref(false)
const startingUpdate = ref(false)
const loadError = ref('')
const { loading: initialLoading, waitFor: waitForInitialLoad } = useInitialLoadBarrier()
let closeUpdateEvents: (() => void) | undefined
let recoveringUpdateOperation = false

const displayedVersion = computed(
  () => update.value?.current_version || about.value?.version || t('about.notAvailable')
)

const changedComponents = computed(
  () => update.value?.components?.filter(component => component.changed) || []
)
const activeOperation = computed<UpdateOperation | undefined>(() => {
  const currentUpdate = update.value
  const operation = currentUpdate?.operation
  if (
    !applying.value ||
    startingUpdate.value ||
    !operation ||
    operation.target_version !== currentUpdate?.latest_version
  ) {
    return undefined
  }
  return operation
})
const displayedComponents = computed<UpdateComponent[]>(() => {
  const currentUpdate = update.value
  const operationComponents = activeOperation.value?.components
  if (!applying.value || !operationComponents?.length) return changedComponents.value
  return operationComponents.map(component =>
    currentUpdate?.components?.find(candidate => candidate.name === component.name) || {
      name: component.name,
      changed: true
    }
  )
})
const releaseSummary = computed(() =>
  update.value?.status === 'update_available'
    ? releaseNoteSummary(update.value.release_notes)
    : []
)
const releaseRemainder = computed(() =>
  update.value?.status === 'update_available'
    ? releaseNoteRemainder(update.value.release_notes)
    : []
)
const componentStates = computed(() => {
  const states = new Map<UpdateComponentName, UpdateOperationComponentState>()
  for (const component of activeOperation.value?.components || []) {
    states.set(component.name, component.state)
  }
  return states
})
const componentProgressWeights: Record<UpdateOperationComponentState, number> = {
  pending: 0,
  pulling: 0.2,
  staged: 0.45,
  restarting: 0.72,
  rolling_back: 0.78,
  rolled_back: 1,
  ready: 1,
  failed: 1
}
const updateProgress = computed(() => {
  const components = displayedComponents.value
  if (!applying.value || components.length === 0) return 0
  const progress = components.reduce((total, component) => {
    const state = componentStates.value.get(component.name) || 'pending'
    return total + componentProgressWeights[state]
  }, 0)
  return Math.round((progress / components.length) * 100)
})
const componentLabels = {
  api: 'API',
  web: 'Web',
  hardware: 'Hardware',
  updater: 'Updater',
  cloudflared: 'Cloudflared'
} as const

function componentLabel(name: keyof typeof componentLabels): string {
  return componentLabels[name]
}

function componentState(name: UpdateComponentName): UpdateOperationComponentState | undefined {
  return componentStates.value.get(name)
}

function componentStateLabel(state?: UpdateOperationComponentState): string {
  return state ? t(`about.componentState.${state}`) : ''
}

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

async function checkForUpdates(refresh = false): Promise<void> {
  if (checking.value) return
  checking.value = true
  try {
    const result = await gateway.checkForUpdates(refresh)
    update.value = result
    if (result.operation?.state === 'running') {
      applying.value = true
      startUpdateEvents()
    } else {
      applying.value = false
      stopUpdateEvents()
    }
  } catch {
    update.value = {
      status: 'unavailable',
      current_version: about.value?.version || '—',
      checked_at: new Date().toISOString(),
      apply_available: false,
      hardware_confirmation_required: false
    }
    applying.value = false
    stopUpdateEvents()
  } finally {
    checking.value = false
  }
}

function stopUpdateEvents(): void {
  closeUpdateEvents?.()
  closeUpdateEvents = undefined
}

function startUpdateEvents(): void {
  if (closeUpdateEvents) return
  closeUpdateEvents = gateway.subscribeSoftwareUpdateEvents({
    onOperation: operation => void handleUpdateOperation(operation),
    onError: () => void recoverUpdateOperation()
  })
}

async function recoverUpdateOperation(): Promise<void> {
  if (!applying.value || recoveringUpdateOperation) return
  recoveringUpdateOperation = true
  try {
    await handleUpdateOperation(await gateway.getSoftwareUpdateStatus())
  } catch {
    // The EventSource reconnects itself and the API emits the current operation on open.
  } finally {
    recoveringUpdateOperation = false
  }
}

async function handleUpdateOperation(operation: UpdateOperation): Promise<void> {
  const trackedOperationID = update.value?.operation?.id
  if (trackedOperationID && trackedOperationID !== operation.id) return
  if (update.value) update.value = { ...update.value, operation }
  startingUpdate.value = false
  if (operation.state === 'running') {
    applying.value = true
    return
  }
  stopUpdateEvents()
  if (operation.state === 'succeeded') {
    if (about.value) {
      about.value = { ...about.value, version: operation.target_version }
    }
    if (update.value) {
      update.value = {
        ...update.value,
        current_version: operation.target_version,
        operation
      }
    }
    showSuccess(t('about.updateSucceeded'))
    await Promise.all([load(), checkForUpdates(true)])
  } else {
    applying.value = false
    showError(t('about.updateFailed'))
  }
}

async function applyUpdate(): Promise<void> {
  const current = update.value
  if (!current?.apply_available || !current.latest_version || applying.value) return
  let confirmHardware = false
  if (current.hardware_confirmation_required) {
    confirmHardware = await requestConfirmation({
      title: t('about.hardwareUpdateTitle'),
      message: t('about.hardwareUpdateWarning'),
      confirmLabel: t('about.confirmUpdate')
    })
    if (!confirmHardware) return
  }
  startingUpdate.value = true
  applying.value = true
  try {
    const operation = await gateway.applySoftwareUpdate(
      current.latest_version,
      confirmHardware
    )
    update.value = { ...current, operation }
    startingUpdate.value = false
    startUpdateEvents()
  } catch {
    startingUpdate.value = false
    applying.value = false
    showError(t('about.updateStartFailed'))
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
  void waitForInitialLoad([() => load(), () => checkForUpdates()])
})

onBeforeUnmount(() => {
  stopUpdateEvents()
})
</script>

<template>
  <SettingsLoadBoundary
    :loading="initialLoading"
    :loading-title="t('about.loading')"
    loading-shape="modules"
  >
  <section class="about-settings" aria-labelledby="about-product-title">
    <SettingsModuleCard
      class="about-card about-product"
      :title="about?.name || 'ModemDeck'"
      title-id="about-product-title"
      :description="t('about.productDescription')"
      icon-tone="brand"
    >
      <template #icon>M</template>

      <p v-if="loadError" class="about-product__error" role="alert">{{ loadError }}</p>

      <dl class="about-facts">
        <div>
          <dt>{{ t('about.version') }}</dt>
          <dd>{{ displayedVersion }}</dd>
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
    </SettingsModuleCard>

    <SettingsModuleCard
      class="about-card about-update"
      :class="statusClass(update?.status)"
      :title="t('about.updateTitle')"
      title-id="about-update-title"
      :description="t('about.updateDescription')"
    >
      <template #status>
        <button
          class="icon-button about-update__check"
          type="button"
          :disabled="checking || applying"
          :aria-label="t('about.checkAgain')"
          :title="t('about.checkAgain')"
          @click="checkForUpdates(true)"
        >
          <RefreshCw :size="17" :class="{ spin: checking }" />
        </button>
      </template>

      <div class="about-status">
        <div class="about-status__summary">
          <span class="about-status__icon">
            <component :is="statusIcon" :size="20" />
          </span>
          <div class="about-status__copy">
            <strong>{{ statusTitle }}</strong>
            <p>{{ statusDescription }}</p>
          </div>
        </div>

        <section
          v-if="displayedComponents.length || update?.apply_available || applying"
          class="about-update__targets"
          :class="{ 'about-update__targets--action-only': !displayedComponents.length }"
          aria-labelledby="about-update-targets-title"
        >
          <div v-if="displayedComponents.length" class="about-update__target-copy">
            <div class="about-update__target-heading">
              <strong id="about-update-targets-title">
                {{ t(applying ? 'about.containersUpdating' : 'about.componentsToUpdate', { count: displayedComponents.length }) }}
              </strong>
              <small v-if="applying">{{ updateProgress }}%</small>
            </div>
            <div
              v-if="applying"
              class="about-update__progress"
              role="progressbar"
              :aria-label="t('about.updateProgress')"
              aria-valuemin="0"
              aria-valuemax="100"
              :aria-valuenow="updateProgress"
            >
              <span :style="{ width: `${updateProgress}%` }" />
            </div>
            <div
              class="about-update__components"
              :class="{ 'about-update__components--progress': applying }"
              role="list"
            >
              <div
                v-for="component in displayedComponents"
                :key="component.name"
                class="about-update__component-row"
                :class="componentState(component.name) ? `about-update__component-row--${componentState(component.name)}` : ''"
                role="listitem"
              >
                <span class="about-update__component-tag">
                  {{ componentLabel(component.name) }}
                </span>
                <small
                  v-if="componentState(component.name)"
                  class="about-update__component-state"
                >
                  {{ componentStateLabel(componentState(component.name)) }}
                </small>
              </div>
            </div>
          </div>
          <button
            v-if="update?.apply_available || applying"
            class="primary-button about-update__apply"
            type="button"
            :disabled="applying"
            @click="applyUpdate"
          >
            {{ applying ? t('about.updating') : t('about.updateAction') }}
          </button>
        </section>

        <section
          v-if="releaseSummary.length && !applying"
          class="about-release__summary"
          aria-labelledby="about-release-summary-title"
        >
          <div class="about-update__section-heading">
            <div class="about-update__section-title">
              <strong id="about-release-summary-title">{{ t('about.releaseSummary') }}</strong>
              <small v-if="update?.release_name">{{ update.release_name }}</small>
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
          <ul>
            <li v-for="line in releaseSummary" :key="line">{{ line }}</li>
          </ul>
          <details v-if="releaseRemainder.length" class="about-release__more">
            <summary>
              <span>{{ t('about.fullReleaseNotes') }}</span>
              <ChevronDown :size="15" />
            </summary>
            <div class="about-release__content">
              <section
                v-for="(section, sectionIndex) in releaseRemainder"
                :key="`${section.title}-${sectionIndex}`"
              >
                <h4 v-if="section.title">{{ section.title }}</h4>
                <ul>
                  <li
                    v-for="(line, lineIndex) in section.lines"
                    :key="`${line}-${lineIndex}`"
                  >
                    {{ line }}
                  </li>
                </ul>
              </section>
            </div>
          </details>
        </section>
      </div>
    </SettingsModuleCard>

    <SettingsModuleCard
      class="about-card about-legal"
      :title="t('about.legalTitle')"
      title-id="about-legal-title"
      :description="t('about.legalDescription')"
    >
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
    </SettingsModuleCard>
  </section>
  </SettingsLoadBoundary>
</template>

<style scoped>
.about-settings {
  display: grid;
  gap: 14px;
}

.about-product__error {
  margin: 0 0 12px;
  color: var(--danger);
  font-size: 11px;
}

.about-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0;
  margin: 0;
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

.about-update {
  --about-status-color: var(--muted);
  --about-status-background: var(--surface-subtle);
  --about-status-border: var(--border);

  container-type: inline-size;
  background: var(--about-status-background);
  border-color: var(--about-status-border);
}

.about-update__check {
  flex: 0 0 auto;
  color: var(--muted);
}

.about-update.about-status--up_to_date {
  --about-status-color: var(--accent-strong);
  --about-status-background: var(--accent-soft);
  --about-status-border: var(--success-border);
}

.about-update.about-status--checking {
  --about-status-color: var(--muted);
  --about-status-background: var(--surface-subtle);
  --about-status-border: var(--border);
}

.about-update.about-status--update_available {
  --about-status-color: var(--warning);
  --about-status-background: var(--warning-soft);
  --about-status-border: var(--warning-border);
}

.about-update.about-status--unavailable {
  --about-status-color: var(--danger);
  --about-status-background: var(--danger-soft);
  --about-status-border: color-mix(in srgb, var(--danger) 28%, var(--border));
}

.about-status {
  display: grid;
  gap: 14px;
  margin: 0;
  color: var(--about-status-color);
}

.about-status__summary {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 11px;
}

.about-status__icon {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  place-items: center;
  background: color-mix(in srgb, var(--surface) 58%, transparent);
  border: 1px solid color-mix(in srgb, currentColor 18%, transparent);
  border-radius: 9px;
}

.about-status__copy {
  min-width: 0;
  padding-top: 1px;
}

.about-status__copy strong {
  color: var(--text);
  font-size: 14px;
}

.about-status__copy p {
  margin: 3px 0 0;
  font-size: 12px;
  line-height: 1.5;
}

.about-update__section-heading {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.about-update__section-title {
  display: flex;
  min-width: 0;
  align-items: baseline;
  gap: 8px;
}

.about-update__section-heading strong,
.about-update__target-heading strong {
  color: var(--text);
  font-size: 11px;
  font-weight: 750;
}

.about-update__section-heading small {
  overflow: hidden;
  color: var(--faint);
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.about-release__summary ul,
.about-release__content ul {
  margin: 8px 0 0;
  padding-left: 18px;
}

.about-release__summary li,
.about-release__content li,
.about-release__content p {
  color: var(--text);
  font-size: 11px;
  line-height: 1.5;
}

.about-release__summary li + li,
.about-release__content li + li {
  margin-top: 4px;
}

.about-release__summary li::marker,
.about-release__content li::marker {
  color: var(--about-status-color);
}

.about-update__targets {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding-top: 14px;
  border-top: 1px solid color-mix(in srgb, var(--about-status-color) 18%, transparent);
}

.about-update__target-copy {
  min-width: 0;
  flex: 1;
}

.about-update__targets--action-only {
  justify-content: flex-end;
}

.about-update__target-heading {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.about-update__target-heading small {
  color: var(--muted);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
}

.about-update__progress {
  height: 5px;
  margin-top: 9px;
  overflow: hidden;
  background: color-mix(in srgb, var(--about-status-color) 13%, var(--surface));
  border-radius: 999px;
}

.about-update__progress > span {
  display: block;
  height: 100%;
  background: var(--success);
  border-radius: inherit;
  transition: width var(--motion-base) var(--ease-standard);
}

.about-update__apply {
  flex: 0 0 auto;
}

.about-release__summary {
  min-width: 0;
  padding-top: 14px;
  border-top: 1px solid color-mix(in srgb, var(--about-status-color) 18%, transparent);
}

.about-update__components {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 9px;
}

.about-update__components--progress {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  grid-auto-rows: minmax(0, 1fr);
  gap: 4px;
  height: 144px;
  max-width: 520px;
}

.about-update__component-row {
  display: inline-flex;
  align-items: center;
}

.about-update__component-tag {
  display: inline-flex;
  align-items: center;
  padding: 4px 8px;
  color: var(--success);
  font-size: 10px;
  font-weight: 750;
  background: var(--success-soft);
  border: 1px solid var(--success-border);
  border-radius: 999px;
}

.about-update__components--progress .about-update__component-row {
  width: 100%;
  min-height: 0;
  justify-content: space-between;
  gap: 14px;
  border-bottom: 1px solid color-mix(in srgb, var(--about-status-color) 12%, transparent);
}

.about-update__components--progress .about-update__component-row:last-child {
  border-bottom: 0;
}

.about-update__component-state {
  min-width: 88px;
  color: var(--muted);
  font-size: 9px;
  font-weight: 650;
  text-align: right;
}

.about-update__component-row--pulling .about-update__component-state,
.about-update__component-row--staged .about-update__component-state,
.about-update__component-row--restarting .about-update__component-state {
  color: var(--warning);
}

.about-update__component-row--ready .about-update__component-state {
  color: var(--success);
}

.about-update__component-row--rolling_back .about-update__component-state,
.about-update__component-row--rolled_back .about-update__component-state {
  color: var(--muted);
}

.about-update__component-row--failed .about-update__component-state {
  color: var(--danger);
}

.about-release__more {
  margin-top: 10px;
}

.about-release__more summary {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--about-status-color);
  font-size: 11px;
  font-weight: 750;
  cursor: pointer;
  list-style: none;
}

.about-release__more summary::-webkit-details-marker {
  display: none;
}

.about-release__more summary svg {
  transition: transform var(--motion-fast) var(--ease-standard);
}

.about-release__more[open] summary svg {
  transform: rotate(180deg);
}

.about-release__content {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid color-mix(in srgb, var(--about-status-color) 18%, transparent);
}

.about-release__content section + section {
  margin-top: 12px;
}

.about-release__content h4 {
  margin: 0;
  color: var(--text);
  font-size: 12px;
}

.about-status__link {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 4px;
  color: var(--about-status-color);
  font-size: 11px;
  font-weight: 750;
}

.about-legal__links {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin: 0;
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

@container (max-width: 460px) {
  .about-status {
    gap: 12px;
  }

  .about-status__icon {
    width: 32px;
    height: 32px;
    flex-basis: 32px;
  }

  .about-update__targets {
    align-items: stretch;
    flex-direction: column;
  }

  .about-update__components--progress {
    height: 192px;
  }

  .about-update__apply {
    width: 100%;
  }
}

@media (max-width: 860px) {
  .about-facts,
  .about-legal__links {
    grid-template-columns: 1fr;
  }
}

@media (prefers-reduced-motion: reduce) {
  .about-update__progress > span,
  .about-release__more summary svg {
    transition: none;
  }
}
</style>
