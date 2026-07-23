import { createRouter, createWebHashHistory } from 'vue-router'
import AppShell from '../components/AppShell.vue'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/',
      component: AppShell,
      children: [
        { path: '', redirect: '/contacts' },
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
          path: 'settings/:section?',
          name: 'settings',
          component: () => import('../views/SettingsView.vue')
        }
      ]
    },
    { path: '/:pathMatch(.*)*', redirect: '/contacts' }
  ]
})

export default router
