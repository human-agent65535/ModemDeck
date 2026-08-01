<script setup lang="ts">
import SkeletonBlock from '../SkeletonBlock.vue'

withDefaults(
  defineProps<{
    label: string
    framed?: boolean
  }>(),
  {
    framed: false
  }
)
</script>

<template>
  <div
    class="conversation-skeleton"
    :class="{ 'conversation-skeleton--framed': framed }"
    role="status"
    aria-live="polite"
  >
    <span class="sr-only">{{ label }}</span>
    <header v-if="framed" class="conversation-skeleton__header" aria-hidden="true">
      <SkeletonBlock class="conversation-skeleton__avatar" />
      <span class="conversation-skeleton__identity">
        <SkeletonBlock />
        <SkeletonBlock />
      </span>
      <span class="conversation-skeleton__actions">
        <SkeletonBlock v-for="action in 3" :key="action" />
      </span>
    </header>
    <div class="conversation-skeleton__viewport">
      <div class="conversation-skeleton__messages" aria-hidden="true">
        <span
          v-for="message in 6"
          :key="message"
          class="conversation-skeleton__row"
          :class="{ 'conversation-skeleton__row--outgoing': message % 3 === 0 }"
        >
          <SkeletonBlock />
        </span>
      </div>
    </div>
    <footer v-if="framed" class="conversation-skeleton__composer" aria-hidden="true">
      <SkeletonBlock class="conversation-skeleton__input" />
      <SkeletonBlock class="conversation-skeleton__send" />
    </footer>
  </div>
</template>

<style scoped>
.conversation-skeleton {
  display: flex;
  width: 100%;
  min-width: 0;
  min-height: 100%;
  flex: 1;
  flex-direction: column;
}

.conversation-skeleton__viewport {
  display: flex;
  min-height: 100%;
  flex: 1;
  flex-direction: column;
  justify-content: flex-end;
  padding: 20px;
  overflow: hidden;
  background: #fbfcfd;
}

.conversation-skeleton--framed .conversation-skeleton__viewport {
  min-height: 0;
}

.conversation-skeleton__header {
  display: flex;
  min-height: 60px;
  flex: 0 0 auto;
  align-items: center;
  gap: 12px;
  padding: 8px 20px;
  border-bottom: 1px solid var(--border);
}

.conversation-skeleton__avatar {
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  border-radius: 50%;
}

.conversation-skeleton__identity {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 7px;
}

.conversation-skeleton__identity > span:first-child {
  width: min(190px, 36%);
  height: 13px;
}

.conversation-skeleton__identity > span:last-child {
  width: min(260px, 52%);
  height: 9px;
}

.conversation-skeleton__actions {
  display: flex;
  gap: var(--detail-action-gap);
}

.conversation-skeleton__actions > span {
  width: var(--detail-action-size);
  height: var(--detail-action-size);
  border-radius: var(--radius-control);
}

.conversation-skeleton__composer {
  display: flex;
  min-height: 70px;
  flex: 0 0 auto;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-top: 1px solid var(--border);
}

.conversation-skeleton__input {
  height: 42px;
  flex: 1;
  border-radius: var(--radius-control);
}

.conversation-skeleton__send {
  width: 42px;
  height: 42px;
  flex: 0 0 42px;
  border-radius: 50%;
}

.conversation-skeleton__messages {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.conversation-skeleton__row {
  display: flex;
  justify-content: flex-start;
}

.conversation-skeleton__row--outgoing {
  justify-content: flex-end;
}

.conversation-skeleton__row > span {
  width: clamp(150px, 42%, 360px);
  height: 52px;
  border-radius: 14px;
}

.conversation-skeleton__row:nth-child(2) > span,
.conversation-skeleton__row:nth-child(5) > span {
  width: clamp(200px, 58%, 480px);
  height: 70px;
}

.conversation-skeleton__row--outgoing > span {
  width: clamp(130px, 36%, 310px);
}

@media (max-width: 560px) {
  .conversation-skeleton__header {
    min-height: 62px;
    padding: 9px 12px;
  }

  .conversation-skeleton__actions > span:nth-child(n + 2) {
    display: none;
  }

  .conversation-skeleton__viewport {
    padding: 16px;
  }

  .conversation-skeleton__composer {
    min-height: 64px;
    padding: 8px 10px;
  }
}
</style>
