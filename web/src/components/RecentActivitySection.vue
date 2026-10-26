<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessageSquareText, PhoneIncoming, PhoneMissed, PhoneOutgoing } from '@lucide/vue'
import { communicationActivities, type CommunicationActivity } from '../composables/useCommunicationActivity'
import { messageThreadRoute } from '../router/messageRoute'
import { callsResource, ensureSearchData, threadsResource } from '../state/workspace'
import { formatDuration, formatRelativeDate } from '../utils/format'
import { normalizedPhoneIdentity } from '../utils/lineIdentity'

// The latest calls and messages with the same numbers, so a detail page answers
// "what happened with this person recently" the way iOS Phone and Google Contacts do.
const props = withDefaults(
  defineProps<{
    numbers: readonly string[]
    excludeCallId?: string
    limit?: number
  }>(),
  { excludeCallId: '', limit: 5 }
)
const { t } = useI18n()

onMounted(() => void ensureSearchData())

const identities = computed(
  () => new Set(props.numbers.map(number => normalizedPhoneIdentity(number)).filter(Boolean))
)
const activities = computed(() => {
  const matches = (number: string) => identities.value.has(normalizedPhoneIdentity(number))
  return communicationActivities(
    callsResource.data.filter(call => call.id !== props.excludeCallId && matches(call.remote_number)),
    threadsResource.data.filter(thread => matches(thread.peer)),
    props.limit
  )
})

function title(activity: CommunicationActivity): string {
  if (activity.kind === 'message') return t('dashboard.message')
  if (activity.call.missed) return t('dashboard.missedCall')
  return activity.call.direction === 'incoming' ? t('dashboard.incoming') : t('dashboard.outgoing')
}

function detail(activity: CommunicationActivity): string {
  if (activity.kind === 'message') return activity.thread.last_content || ''
  return activity.call.duration_seconds > 0 ? formatDuration(activity.call.duration_seconds) : ''
}

function destination(activity: CommunicationActivity) {
  return activity.kind === 'message'
    ? messageThreadRoute(activity.thread.key)
    : { name: 'calls', query: { selected: activity.call.id } }
}
</script>

<template>
  <section v-if="activities.length" class="detail-section recent-activity">
    <h3>{{ t('dashboard.recentActivity') }}</h3>
    <ul>
      <li v-for="activity in activities" :key="activity.key">
        <RouterLink :to="destination(activity)" class="recent-activity__item">
          <span
            class="recent-activity__icon"
            :class="{ 'is-missed': activity.kind === 'call' && activity.call.missed }"
            aria-hidden="true"
          >
            <MessageSquareText v-if="activity.kind === 'message'" :size="16" />
            <PhoneMissed v-else-if="activity.call.missed" :size="16" />
            <PhoneIncoming v-else-if="activity.call.direction === 'incoming'" :size="16" />
            <PhoneOutgoing v-else :size="16" />
          </span>
          <span class="recent-activity__copy">
            <strong>{{ title(activity) }}</strong>
            <small v-if="detail(activity)">{{ detail(activity) }}</small>
          </span>
          <time :datetime="activity.timestamp">{{ formatRelativeDate(activity.timestamp) }}</time>
        </RouterLink>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.recent-activity ul {
  display: grid;
  margin: 0;
  padding: 0;
  list-style: none;
}

.recent-activity__item {
  display: grid;
  min-height: 52px;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-2);
  border-radius: var(--radius-sm);
  grid-template-columns: auto minmax(0, 1fr) auto;
}

.recent-activity__item:hover {
  background: var(--surface-hover);
}

.recent-activity__icon {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  color: var(--muted-strong);
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 50%;
}

.recent-activity__icon.is-missed {
  color: var(--danger);
}

.recent-activity__copy {
  display: grid;
  min-width: 0;
}

.recent-activity__copy strong {
  font-size: var(--text-base);
}

.recent-activity__copy small,
.recent-activity__item time {
  overflow: hidden;
  color: var(--muted);
  font-size: var(--text-sm);
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
