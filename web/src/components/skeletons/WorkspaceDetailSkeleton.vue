<script setup lang="ts">
import SkeletonBlock from '../SkeletonBlock.vue'

withDefaults(
  defineProps<{
    label: string
    shape?: 'contact' | 'communication' | 'recording' | 'dashboard'
  }>(),
  {
    shape: 'communication'
  }
)
</script>

<template>
  <div
    class="workspace-detail-skeleton"
    :class="`workspace-detail-skeleton--${shape}`"
    role="status"
    aria-live="polite"
  >
    <span class="sr-only">{{ label }}</span>
    <header class="workspace-detail-skeleton__header" aria-hidden="true">
      <SkeletonBlock class="workspace-detail-skeleton__avatar" />
      <span class="workspace-detail-skeleton__identity">
        <SkeletonBlock />
        <SkeletonBlock />
      </span>
      <span class="workspace-detail-skeleton__actions">
        <SkeletonBlock v-for="action in 3" :key="action" />
      </span>
    </header>

    <div
      v-if="shape === 'dashboard'"
      class="workspace-detail-skeleton__dashboard"
      aria-hidden="true"
    >
      <div class="workspace-detail-skeleton__summary-grid">
        <SkeletonBlock v-for="card in 4" :key="card" />
      </div>
      <section v-for="section in 3" :key="section" class="workspace-detail-skeleton__section">
        <span class="workspace-detail-skeleton__section-heading">
          <SkeletonBlock />
          <SkeletonBlock />
        </span>
        <div class="workspace-detail-skeleton__card-grid">
          <SkeletonBlock v-for="card in section === 3 ? 3 : 2" :key="card" />
        </div>
      </section>
    </div>

    <div
      v-else-if="shape === 'contact'"
      class="workspace-detail-skeleton__facts"
      aria-hidden="true"
    >
      <section v-for="section in 3" :key="section" class="workspace-detail-skeleton__section">
        <SkeletonBlock class="workspace-detail-skeleton__section-title" />
        <span v-for="row in 2" :key="row" class="workspace-detail-skeleton__fact-row">
          <SkeletonBlock />
          <SkeletonBlock />
        </span>
      </section>
    </div>

    <div
      v-else
      class="workspace-detail-skeleton__communication"
      aria-hidden="true"
    >
      <SkeletonBlock class="workspace-detail-skeleton__hero" />
      <span v-for="row in 4" :key="row" class="workspace-detail-skeleton__fact-row">
        <SkeletonBlock />
        <SkeletonBlock />
      </span>
      <SkeletonBlock
        v-if="shape === 'recording'"
        class="workspace-detail-skeleton__waveform"
      />
    </div>
  </div>
</template>

<style scoped>
.workspace-detail-skeleton {
  display: flex;
  width: 100%;
  min-width: 0;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  overflow: hidden;
  background: var(--surface);
}

.workspace-detail-skeleton__header {
  display: flex;
  min-height: 60px;
  flex: 0 0 auto;
  align-items: center;
  gap: 12px;
  padding: 8px 20px;
  border-bottom: 1px solid var(--border);
}

.workspace-detail-skeleton__avatar {
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  border-radius: 50%;
}

.workspace-detail-skeleton__identity,
.workspace-detail-skeleton__section-heading,
.workspace-detail-skeleton__fact-row {
  display: flex;
  min-width: 0;
}

.workspace-detail-skeleton__identity {
  flex: 1;
  flex-direction: column;
  gap: 7px;
}

.workspace-detail-skeleton__identity > span:first-child {
  width: min(190px, 36%);
  height: 13px;
}

.workspace-detail-skeleton__identity > span:last-child {
  width: min(260px, 52%);
  height: 9px;
}

.workspace-detail-skeleton__actions {
  display: flex;
  gap: var(--detail-action-gap);
}

.workspace-detail-skeleton__actions > span {
  width: var(--detail-action-size);
  height: var(--detail-action-size);
  border-radius: var(--radius-control);
}

.workspace-detail-skeleton__communication,
.workspace-detail-skeleton__facts,
.workspace-detail-skeleton__dashboard {
  width: min(100%, 920px);
  margin-inline: auto;
  padding: 24px;
}

.workspace-detail-skeleton__communication {
  display: grid;
  gap: 0;
}

.workspace-detail-skeleton__hero {
  width: 100%;
  height: 116px;
  margin-bottom: 18px;
  border-radius: 10px;
}

.workspace-detail-skeleton__fact-row {
  min-height: 64px;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  border-bottom: 1px solid var(--border);
}

.workspace-detail-skeleton__fact-row > span:first-child {
  width: min(190px, 34%);
  height: 11px;
}

.workspace-detail-skeleton__fact-row > span:last-child {
  width: min(290px, 44%);
  height: 38px;
  border-radius: var(--radius-control);
}

.workspace-detail-skeleton__waveform {
  width: 100%;
  height: 74px;
  margin-top: 24px;
}

.workspace-detail-skeleton__facts,
.workspace-detail-skeleton__dashboard {
  display: grid;
  gap: 22px;
}

.workspace-detail-skeleton__section {
  display: grid;
  gap: 12px;
  padding-bottom: 20px;
  border-bottom: 1px solid var(--border);
}

.workspace-detail-skeleton__section-title {
  width: min(180px, 34%);
  height: 13px;
}

.workspace-detail-skeleton__summary-grid,
.workspace-detail-skeleton__card-grid {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.workspace-detail-skeleton__summary-grid {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.workspace-detail-skeleton__summary-grid > span {
  height: 94px;
  border-radius: 10px;
}

.workspace-detail-skeleton__section-heading {
  align-items: center;
  justify-content: space-between;
  gap: 18px;
}

.workspace-detail-skeleton__section-heading > span:first-child {
  width: 150px;
  height: 13px;
}

.workspace-detail-skeleton__section-heading > span:last-child {
  width: 88px;
  height: 10px;
}

.workspace-detail-skeleton__card-grid > span {
  height: 86px;
  border-radius: 10px;
}

@media (max-width: 560px) {
  .workspace-detail-skeleton__header {
    min-height: 62px;
    padding: 9px 12px;
  }

  .workspace-detail-skeleton__actions > span:nth-child(n + 2) {
    display: none;
  }

  .workspace-detail-skeleton__communication,
  .workspace-detail-skeleton__facts,
  .workspace-detail-skeleton__dashboard {
    padding: 16px;
  }

  .workspace-detail-skeleton__summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
