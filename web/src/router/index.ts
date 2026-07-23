import { createRouter, createWebHashHistory } from 'vue-router'
import AppShell from '../components/AppShell.vue'
import { ensureSession } from '../state/session'

const router = createRouter({
  history: createWebHashHistory(),
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
          path: 'messages/:threadKey?',
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
