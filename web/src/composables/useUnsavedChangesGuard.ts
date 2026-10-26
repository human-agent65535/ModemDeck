import { onBeforeUnmount, onMounted, toValue, type MaybeRefOrGetter } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { translate } from '../i18n'
import { requestConfirmation } from '../state/confirmation'

/**
 * Asks before navigation or a tab close throws away an edited, unsaved form, as GitHub and
 * Stripe settings do. Route updates count too, since choosing another resource is a query change.
 */
export function useUnsavedChangesGuard(dirty: MaybeRefOrGetter<boolean>): void {
  async function confirmDiscard(): Promise<boolean> {
    if (!toValue(dirty)) return true
    return requestConfirmation({
      title: translate('common.discardChangesTitle'),
      message: translate('common.discardChangesMessage'),
      confirmLabel: translate('common.discard'),
      tone: 'danger'
    })
  }

  onBeforeRouteLeave(confirmDiscard)
  onBeforeRouteUpdate(confirmDiscard)

  function onBeforeUnload(event: BeforeUnloadEvent): void {
    if (!toValue(dirty)) return
    event.preventDefault()
  }

  onMounted(() => window.addEventListener('beforeunload', onBeforeUnload))
  onBeforeUnmount(() => window.removeEventListener('beforeunload', onBeforeUnload))
}
