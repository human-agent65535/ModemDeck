<script setup lang="ts">
import SkeletonBlock from '../SkeletonBlock.vue'
import type { SettingsSkeletonShape } from './settingsSkeleton'

defineProps<{
  label: string
  shape: SettingsSkeletonShape
}>()
</script>

<template>
  <div
    class="settings-skeleton"
    :class="`settings-skeleton--${shape}`"
    role="status"
    aria-live="polite"
  >
    <span class="sr-only">{{ label }}</span>

    <div
      v-if="shape === 'preferences'"
      class="settings-skeleton__preferences"
      aria-hidden="true"
    >
      <section v-for="section in 3" :key="section" class="settings-skeleton__section">
        <header class="settings-skeleton__section-header">
          <SkeletonBlock class="settings-skeleton__icon" />
          <span class="settings-skeleton__copy">
            <SkeletonBlock class="settings-skeleton__title" />
            <SkeletonBlock class="settings-skeleton__description" />
          </span>
        </header>
        <div v-for="row in 2" :key="row" class="settings-skeleton__control-row">
          <span class="settings-skeleton__row-copy">
            <SkeletonBlock class="settings-skeleton__row-title" />
            <SkeletonBlock class="settings-skeleton__row-description" />
          </span>
          <SkeletonBlock class="settings-skeleton__control" />
        </div>
      </section>
    </div>

    <div
      v-else-if="shape === 'modules'"
      class="settings-skeleton__modules"
      aria-hidden="true"
    >
      <section v-for="card in 3" :key="card" class="settings-skeleton__card">
        <header class="settings-skeleton__card-header">
          <SkeletonBlock class="settings-skeleton__card-icon" />
          <span class="settings-skeleton__copy">
            <SkeletonBlock class="settings-skeleton__title" />
            <SkeletonBlock class="settings-skeleton__description" />
          </span>
          <SkeletonBlock class="settings-skeleton__badge" />
        </header>
        <div class="settings-skeleton__card-body">
          <span v-for="fact in 2" :key="fact" class="settings-skeleton__fact">
            <SkeletonBlock />
            <SkeletonBlock />
          </span>
        </div>
      </section>
    </div>

    <div
      v-else-if="shape === 'master-detail'"
      class="settings-skeleton__master-detail"
      aria-hidden="true"
    >
      <aside class="settings-skeleton__rail">
        <header class="settings-skeleton__rail-header">
          <span class="settings-skeleton__copy">
            <SkeletonBlock class="settings-skeleton__title" />
            <SkeletonBlock class="settings-skeleton__description" />
          </span>
          <SkeletonBlock class="settings-skeleton__round-action" />
        </header>
        <div class="settings-skeleton__rail-toolbar">
          <SkeletonBlock />
        </div>
        <div class="settings-skeleton__rail-list">
          <div v-for="row in 5" :key="row" class="settings-skeleton__resource-row">
            <SkeletonBlock class="settings-skeleton__avatar" />
            <span class="settings-skeleton__copy">
              <SkeletonBlock class="settings-skeleton__row-title" />
              <SkeletonBlock class="settings-skeleton__row-description" />
            </span>
            <SkeletonBlock class="settings-skeleton__status-dot" />
          </div>
        </div>
      </aside>
      <main class="settings-skeleton__editor">
        <header class="settings-skeleton__editor-header">
          <SkeletonBlock class="settings-skeleton__avatar" />
          <span class="settings-skeleton__copy">
            <SkeletonBlock class="settings-skeleton__title" />
            <SkeletonBlock class="settings-skeleton__description" />
          </span>
          <SkeletonBlock class="settings-skeleton__badge" />
        </header>
        <SkeletonBlock class="settings-skeleton__field" />
        <div v-for="row in 4" :key="row" class="settings-skeleton__editor-row">
          <span class="settings-skeleton__row-copy">
            <SkeletonBlock class="settings-skeleton__row-title" />
            <SkeletonBlock class="settings-skeleton__row-description" />
          </span>
          <SkeletonBlock class="settings-skeleton__control" />
        </div>
      </main>
    </div>

    <div
      v-else-if="shape === 'workbench'"
      class="settings-skeleton__workbench"
      aria-hidden="true"
    >
      <section class="settings-skeleton__selector">
        <header class="settings-skeleton__toolbar">
          <SkeletonBlock class="settings-skeleton__title" />
          <SkeletonBlock class="settings-skeleton__description" />
        </header>
        <div class="settings-skeleton__module-grid">
          <article v-for="card in 2" :key="card" class="settings-skeleton__module-card">
            <SkeletonBlock class="settings-skeleton__card-icon" />
            <span class="settings-skeleton__copy">
              <SkeletonBlock class="settings-skeleton__title" />
              <SkeletonBlock class="settings-skeleton__description" />
              <SkeletonBlock class="settings-skeleton__description settings-skeleton__description--short" />
            </span>
            <SkeletonBlock class="settings-skeleton__status-dot" />
          </article>
        </div>
      </section>
      <section class="settings-skeleton__workbench-detail">
        <nav class="settings-skeleton__tabs">
          <SkeletonBlock v-for="tab in 5" :key="tab" />
        </nav>
        <div class="settings-skeleton__workbench-body">
          <header class="settings-skeleton__detail-heading">
            <SkeletonBlock class="settings-skeleton__title" />
            <SkeletonBlock class="settings-skeleton__description" />
          </header>
          <div class="settings-skeleton__facts-grid">
            <span v-for="fact in 6" :key="fact" class="settings-skeleton__fact">
              <SkeletonBlock />
              <SkeletonBlock />
            </span>
          </div>
        </div>
      </section>
    </div>

    <div
      v-else-if="shape === 'detail-form'"
      class="settings-skeleton__detail-form"
      aria-hidden="true"
    >
      <header class="settings-skeleton__summary">
        <SkeletonBlock class="settings-skeleton__card-icon" />
        <span class="settings-skeleton__copy">
          <SkeletonBlock class="settings-skeleton__title" />
          <SkeletonBlock class="settings-skeleton__description" />
        </span>
        <SkeletonBlock class="settings-skeleton__badge" />
      </header>
      <div class="settings-skeleton__facts-grid">
        <span v-for="fact in 6" :key="fact" class="settings-skeleton__fact">
          <SkeletonBlock />
          <SkeletonBlock />
        </span>
      </div>
      <section class="settings-skeleton__form-section">
        <header class="settings-skeleton__detail-heading">
          <SkeletonBlock class="settings-skeleton__title" />
          <SkeletonBlock class="settings-skeleton__description" />
        </header>
        <div class="settings-skeleton__file-grid">
          <SkeletonBlock v-for="field in 2" :key="field" />
        </div>
        <SkeletonBlock class="settings-skeleton__button" />
      </section>
    </div>

    <div v-else class="settings-skeleton__diagnostics" aria-hidden="true">
      <section v-for="section in 3" :key="section" class="settings-skeleton__diagnostic-section">
        <header class="settings-skeleton__detail-heading">
          <SkeletonBlock class="settings-skeleton__title" />
          <SkeletonBlock class="settings-skeleton__badge" />
        </header>
        <div v-if="section < 3" class="settings-skeleton__facts-grid">
          <span v-for="fact in 6" :key="fact" class="settings-skeleton__fact">
            <SkeletonBlock />
            <SkeletonBlock />
          </span>
        </div>
        <div v-else class="settings-skeleton__log-lines">
          <SkeletonBlock v-for="line in 4" :key="line" />
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.settings-skeleton {
  width: 100%;
  min-width: 0;
  min-height: 0;
  flex: 1;
  overflow: hidden;
}

.settings-skeleton__copy,
.settings-skeleton__row-copy,
.settings-skeleton__fact {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 8px;
}

.settings-skeleton__copy,
.settings-skeleton__row-copy {
  flex: 1;
}

.settings-skeleton__title {
  width: min(42%, 180px);
  height: 13px;
}

.settings-skeleton__description {
  width: min(74%, 320px);
  height: 9px;
}

.settings-skeleton__description--short {
  width: min(46%, 150px);
}

.settings-skeleton__row-title {
  width: min(38%, 160px);
  height: 11px;
}

.settings-skeleton__row-description {
  width: min(68%, 280px);
  height: 9px;
}

.settings-skeleton__icon,
.settings-skeleton__avatar,
.settings-skeleton__round-action,
.settings-skeleton__status-dot {
  flex: 0 0 auto;
  border-radius: 50%;
}

.settings-skeleton__icon,
.settings-skeleton__avatar {
  width: 36px;
  height: 36px;
}

.settings-skeleton__card-icon {
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  border-radius: 9px;
}

.settings-skeleton__round-action {
  width: 36px;
  height: 36px;
}

.settings-skeleton__status-dot {
  width: 8px;
  height: 8px;
}

.settings-skeleton__badge {
  width: 72px;
  height: 24px;
  flex: 0 0 72px;
  border-radius: 999px;
}

.settings-skeleton__control {
  width: min(220px, 34%);
  height: 40px;
  flex: 0 0 auto;
  border-radius: var(--radius-control);
}

.settings-skeleton__preferences {
  display: grid;
  max-width: 680px;
  gap: 26px;
}

.settings-skeleton__section-header,
.settings-skeleton__card-header,
.settings-skeleton__editor-header,
.settings-skeleton__summary {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 11px;
}

.settings-skeleton__section-header {
  min-height: 58px;
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.settings-skeleton__control-row,
.settings-skeleton__editor-row {
  display: flex;
  min-height: 68px;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  border-bottom: 1px solid var(--border);
}

.settings-skeleton__modules {
  display: grid;
  gap: 14px;
}

.settings-skeleton__card {
  padding: 18px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 10px;
}

.settings-skeleton__card-body {
  display: grid;
  margin-top: 16px;
  gap: 18px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.settings-skeleton__fact > :first-child {
  width: min(36%, 110px);
  height: 9px;
}

.settings-skeleton__fact > :last-child {
  width: min(78%, 260px);
  height: 12px;
}

.settings-skeleton__master-detail {
  display: grid;
  height: 100%;
  min-height: 520px;
  grid-template-columns: clamp(240px, 25%, 280px) minmax(0, 1fr);
  background: var(--surface);
  border-top: 1px solid var(--border);
}

.settings-skeleton__rail {
  min-width: 0;
  background: var(--surface-subtle);
  border-right: 1px solid var(--border);
}

.settings-skeleton__rail-header {
  display: flex;
  min-height: 64px;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}

.settings-skeleton__rail-toolbar {
  padding: 10px 12px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}

.settings-skeleton__rail-toolbar > span {
  width: 100%;
  height: 36px;
  border-radius: var(--radius-control);
}

.settings-skeleton__resource-row {
  display: flex;
  min-height: 64px;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}

.settings-skeleton__editor {
  min-width: 0;
  padding: 20px 24px;
  overflow: hidden;
}

.settings-skeleton__editor-header {
  min-height: 64px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
}

.settings-skeleton__field {
  width: min(680px, 76%);
  height: 42px;
  margin: 22px 0;
  border-radius: var(--radius-control);
}

.settings-skeleton__editor-row {
  max-width: 760px;
}

.settings-skeleton__workbench {
  min-height: 520px;
  background: var(--surface);
  border-top: 1px solid var(--border);
}

.settings-skeleton__selector {
  padding-inline: clamp(20px, 2.8vw, 42px);
  border-bottom: 1px solid var(--border);
}

.settings-skeleton__toolbar {
  display: flex;
  min-height: 46px;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid var(--border);
}

.settings-skeleton__toolbar .settings-skeleton__title {
  width: 100px;
}

.settings-skeleton__toolbar .settings-skeleton__description {
  width: 44px;
}

.settings-skeleton__module-grid {
  display: grid;
  padding: 14px 1px 16px;
  gap: 12px;
  grid-template-columns: repeat(auto-fill, minmax(320px, 420px));
}

.settings-skeleton__module-card {
  display: flex;
  min-height: 104px;
  align-items: flex-start;
  gap: 12px;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 10px;
}

.settings-skeleton__tabs {
  display: flex;
  height: 50px;
  align-items: stretch;
  gap: 2px;
  padding-inline: clamp(20px, 2.8vw, 42px);
  border-bottom: 1px solid var(--border);
}

.settings-skeleton__tabs > span {
  width: 112px;
  height: 100%;
  border-radius: 0;
}

.settings-skeleton__workbench-body,
.settings-skeleton__detail-form,
.settings-skeleton__diagnostics {
  padding: 22px clamp(20px, 2.8vw, 42px);
}

.settings-skeleton__detail-heading {
  display: flex;
  min-height: 40px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding-bottom: 11px;
  border-bottom: 1px solid var(--border);
}

.settings-skeleton__facts-grid {
  display: grid;
  padding: 20px 0;
  gap: 22px 28px;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.settings-skeleton__detail-form {
  padding-top: 0;
}

.settings-skeleton__summary {
  min-height: 72px;
  border-bottom: 1px solid var(--border);
}

.settings-skeleton__form-section,
.settings-skeleton__diagnostic-section {
  padding-top: 22px;
  border-top: 1px solid var(--border);
}

.settings-skeleton__file-grid {
  display: grid;
  margin: 20px 0;
  gap: 12px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.settings-skeleton__file-grid > span {
  height: 76px;
  border-radius: var(--radius-control);
}

.settings-skeleton__button {
  width: 132px;
  height: 42px;
  border-radius: var(--radius-control);
}

.settings-skeleton__diagnostics {
  display: grid;
  padding-top: 0;
  gap: 26px;
}

.settings-skeleton__diagnostic-section:first-child {
  border-top: 0;
}

.settings-skeleton__log-lines {
  display: grid;
  padding-top: 16px;
  gap: 9px;
}

.settings-skeleton__log-lines > span {
  width: 100%;
  height: 13px;
}

.settings-skeleton__log-lines > span:nth-child(even) {
  width: 82%;
}

@media (max-width: 860px) {
  .settings-skeleton__preferences {
    max-width: none;
  }

  .settings-skeleton__control-row,
  .settings-skeleton__editor-row {
    align-items: flex-start;
    flex-direction: column;
    gap: 10px;
    padding: 13px 0;
  }

  .settings-skeleton__control {
    width: min(240px, 72%);
    align-self: flex-end;
  }

  .settings-skeleton__card-body,
  .settings-skeleton__facts-grid,
  .settings-skeleton__file-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .settings-skeleton__master-detail {
    min-height: 0;
    grid-template-columns: minmax(0, 1fr);
  }

  .settings-skeleton__rail {
    border-right: 0;
  }

  .settings-skeleton__editor,
  .settings-skeleton__workbench-detail {
    display: none;
  }

  .settings-skeleton__module-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
