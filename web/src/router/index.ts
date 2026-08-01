import { createRouter, createWebHistory } from 'vue-router'
import AppShell from '../components/AppShell.vue'
import { ensureSession } from '../state/session'
import {
  messageComposeContextFromReference,
  messageThreadKeyFromReference
} from './messageRoute'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/LoginView.vue')
    },
    {
      path: '/',
      component: AppShell,
      children: [
        {
          path: '',
          name: 'dashboard',
          component: () => import('../views/DashboardView.vue'),
          meta: { communication: true }
        },
        {
          path: 'contacts/:contactId?',
          name: 'contacts',
          component: () => import('../views/ContactsView.vue'),
          meta: { communication: true }
        },
        {
          path: 'messages/:threadRef?',
          name: 'messages',
          component: () => import('../views/MessagesView.vue'),
          meta: { communication: true }
        },
        {
          path: 'calls',
          name: 'calls',
          component: () => import('../views/CallsView.vue'),
          meta: { communication: true }
        },
        {
          path: 'recordings',
          name: 'recordings',
          component: () => import('../views/RecordingsView.vue'),
          meta: { communication: true }
        },
        {
          path: 'traffic',
          name: 'traffic',
          component: () => import('../views/TrafficView.vue')
        },
        {
          path: 'settings/:section?',
          name: 'settings',
          component: () => import('../views/SettingsView.vue')
        }
      ]
    },
    { path: '/:pathMatch(.*)*', redirect: '/' }
  ]
})

router.beforeEach(async to => {
  if (to.name === 'messages') {
    const threadReference = to.params.threadRef
    const hasCompose = Object.prototype.hasOwnProperty.call(
      to.query,
      'compose'
    )
    const invalidThread =
      typeof threadReference === 'string' &&
      !messageThreadKeyFromReference(threadReference)
    const invalidCompose =
      hasCompose && !messageComposeContextFromReference(to.query.compose)
    if (invalidThread || invalidCompose) {
      const query = { ...to.query }
      if (invalidCompose) delete query.compose
      return { name: 'messages', query, replace: true }
    }
  }

  if (
    to.name === 'dashboard' &&
    typeof to.query.item === 'string' &&
    to.query.item.startsWith('message:') &&
    !messageThreadKeyFromReference(to.query.item.slice('message:'.length))
  ) {
    const query = { ...to.query }
    delete query.item
    return { name: 'dashboard', query, replace: true }
  }

  const authenticated = await ensureSession()

  if (to.name === 'login') {
    return authenticated ? { name: 'dashboard' } : true
  }
  if (!authenticated) {
    return {
      name: 'login',
      query: { redirect: to.fullPath }
    }
  }
  return true
})

export default router
