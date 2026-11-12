<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatDateTime } from '../utils/format'

const props = defineProps<{
  notBefore: string
  notAfter: string
  subject: string
  issuer: string
  dnsNames: string[]
  ipAddresses?: string[]
  fingerprint: string
}>()

const { t } = useI18n()
const fingerprintParts = computed(() => {
  const groups = props.fingerprint.split(':')
  if (groups.length < 2) return [props.fingerprint]
  const midpoint = Math.ceil(groups.length / 2)
  return [groups.slice(0, midpoint).join(':'), groups.slice(midpoint).join(':')]
})

function formatTimestamp(value: string): string {
  return value ? formatDateTime(value, 'numeric') : t('tls.notProvided')
}
</script>

<template>
  <dl class="tls-facts">
    <div>
      <dt>{{ t('tls.validFrom') }}</dt>
      <dd>{{ formatTimestamp(notBefore) }}</dd>
    </div>
    <div>
      <dt>{{ t('tls.expiresAt') }}</dt>
      <dd>{{ formatTimestamp(notAfter) }}</dd>
    </div>
    <div class="tls-facts__wide">
      <dt>{{ t('tls.subject') }}</dt>
      <dd>{{ subject || t('tls.notProvided') }}</dd>
    </div>
    <div class="tls-facts__wide">
      <dt>{{ t('tls.issuer') }}</dt>
      <dd>{{ issuer || t('tls.notProvided') }}</dd>
    </div>
    <div class="tls-facts__wide">
      <dt>DNS SAN</dt>
      <dd>{{ dnsNames.join(t('common.listSeparator')) || t('common.none') }}</dd>
    </div>
    <div v-if="ipAddresses !== undefined" class="tls-facts__wide">
      <dt>IP SAN</dt>
      <dd>
        {{ ipAddresses?.join(t('common.listSeparator')) || t('common.none') }}
      </dd>
    </div>
    <div class="tls-facts__wide">
      <dt>{{ t('tls.fingerprint') }}</dt>
      <dd>
        <code class="tls-fingerprint">
          <span>{{ fingerprintParts[0] }}</span>
          <template v-if="fingerprintParts[1]">
            <span aria-hidden="true">:</span><wbr />
            <span>{{ fingerprintParts[1] }}</span>
          </template>
        </code>
      </dd>
    </div>
  </dl>
</template>
