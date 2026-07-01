<script setup lang="ts">
import { computed, defineAsyncComponent, ref, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from './stores/auth'
import LoadingScreen from './components/LoadingScreen.vue'
import { ElMessage } from 'element-plus'
import { shouldShowDisclaimer } from './disclaimer'

const DISCLAIMER_AGREED_AT_KEY = 'vohive_disclaimer_agreed_at'

const route = useRoute()
const auth = useAuthStore()

const isDark = ref(localStorage.getItem('theme') === 'dark')
const showDisclaimer = ref(false)
const confirmText = ref('')
const expectedConfirmText = '我同意并确认'
const canAccept = computed(() => confirmText.value === expectedConfirmText)

function toggleTheme() {
  isDark.value = !isDark.value
  const mode = isDark.value ? 'dark' : 'light'
  localStorage.setItem('theme', mode)
  updateHtmlClass(mode)
}

function updateHtmlClass(mode: 'dark' | 'light') {
  if (mode === 'dark') {
    document.documentElement.classList.add('dark')
  } else {
    document.documentElement.classList.remove('dark')
  }
}

onMounted(() => {
  if (isDark.value) {
    updateHtmlClass('dark')
  }
})

// 监听登录状态，每周首次登录弹一次（同意状态持久化在 localStorage，
// 跨会话/跨标签页生效，距上次同意满一周后再次登录才会重新弹出）
watch(() => auth.isAuthenticated, (isAuthenticated) => {
  if (isAuthenticated) {
    const agreedAtRaw = localStorage.getItem(DISCLAIMER_AGREED_AT_KEY)
    const agreedAt = agreedAtRaw === null ? null : Number(agreedAtRaw)
    if (shouldShowDisclaimer(agreedAt, Date.now())) {
      confirmText.value = ''
      showDisclaimer.value = true
    }
  } else {
    showDisclaimer.value = false
  }
}, { immediate: true })

function acceptDisclaimer() {
  if (!canAccept.value) return
  localStorage.setItem(DISCLAIMER_AGREED_AT_KEY, String(Date.now()))
  showDisclaimer.value = false
}

function rejectDisclaimer() {
  ElMessage.warning('正在退出并清理软件...')
  const token = localStorage.getItem('token') || ''
  fetch('/api/system/uninstall', {
    method: 'POST',
    headers: token ? { Authorization: `Bearer ${token}` } : undefined
  })
    .finally(() => {
      document.body.innerHTML = '<div style="display:flex;height:100vh;background:#0a0a0a;align-items:center;justify-content:center;font-size:24px;color:#ef4444;font-weight:bold;font-family:sans-serif;flex-direction:column;gap:16px;"><div><svg style="width:64px;height:64px;" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" /></svg></div><div>软件已被卸载 / 服务已终止</div></div>'
    })
}

const AuthenticatedShell = defineAsyncComponent(() => import('./layouts/AuthenticatedShell.vue'))
const UnauthenticatedShell = defineAsyncComponent(() => import('./layouts/UnauthenticatedShell.vue'))
const shell = computed(() =>
  auth.isAuthenticated && route.name !== 'Login' ? AuthenticatedShell : UnauthenticatedShell
)
</script>

<template>
  <div class="h-screen w-screen overflow-hidden bg-gray-50 dark:bg-[#101014] text-gray-900 dark:text-gray-100 font-sans selection:bg-indigo-500 selection:text-white transition-colors duration-300">
    <Suspense>
      <template #default>
        <component :is="shell" :is-dark="isDark" @toggle-theme="toggleTheme" />
      </template>
      <template #fallback>
        <LoadingScreen />
      </template>
    </Suspense>

    <!-- 高级感全屏免责声明弹窗 -->
    <Transition name="fade-slide">
      <div v-if="showDisclaimer" class="fixed inset-0 z-[9999] flex items-center justify-center bg-black/60 backdrop-blur-md">
        <div class="relative w-full max-w-lg p-8 mx-4 overflow-hidden bg-white/90 dark:bg-gray-900/90 backdrop-blur-2xl rounded-3xl shadow-2xl border border-white/20 dark:border-gray-700/50">
          <!-- 装饰性渐变背景 -->
          <div class="absolute top-0 left-0 w-full h-32 bg-gradient-to-b from-indigo-500/20 to-transparent pointer-events-none"></div>
          
          <div class="relative z-10">
            <div class="flex items-center justify-center w-16 h-16 mx-auto mb-6 bg-gradient-to-br from-[#5b5bd6] to-[#4a4ac2] rounded-2xl shadow-lg shadow-indigo-500/30">
              <svg class="w-8 h-8 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
            </div>
            
            <h2 class="mb-5 text-2xl font-extrabold text-center text-gray-900 dark:text-white tracking-tight">VoHive 最终用户许可与免责声明</h2>
            
            <div class="space-y-4 text-[14px] text-gray-600 dark:text-gray-300 leading-relaxed font-medium">
              <div class="flex items-start">
                <div class="flex-shrink-0 flex items-center justify-center w-6 h-6 mt-0.5 mr-3 text-xs font-bold text-indigo-700 bg-indigo-100 rounded-full dark:text-indigo-300 dark:bg-indigo-900/60 shadow-sm">1</div>
                <p>本软件（VoHive）属于个人开发者业余时间开发的工具软件，仅供技术研究、学习交流和个人内部测试使用。<strong class="text-indigo-600 dark:text-indigo-400">严禁用于任何商业用途</strong>，严禁作为生产环境的基础设施。</p>
              </div>
              <div class="flex items-start">
                <div class="flex-shrink-0 flex items-center justify-center w-6 h-6 mt-0.5 mr-3 text-xs font-bold text-indigo-700 bg-indigo-100 rounded-full dark:text-indigo-300 dark:bg-indigo-900/60 shadow-sm">2</div>
                <p>使用者承诺将严格遵守所在国家或地区的相关法律法规。<strong class="text-red-500 dark:text-red-400">严禁将本软件用于电信诈骗、垃圾短信发送、非法网络代理、渗透测试等任何非法或违规场景</strong>。</p>
              </div>
              <div class="flex items-start">
                <div class="flex-shrink-0 flex items-center justify-center w-6 h-6 mt-0.5 mr-3 text-xs font-bold text-indigo-700 bg-indigo-100 rounded-full dark:text-indigo-300 dark:bg-indigo-900/60 shadow-sm">3</div>
                <p>本软件涉及底层 Modem 通信操作，可能包含未知的缺陷。对于因使用本软件引发的硬件损坏、通信资费异常、隐私泄露等直接或间接损失，<strong>由使用者自行承担所有责任</strong>。</p>
              </div>
              <div class="flex items-start">
                <div class="flex-shrink-0 flex items-center justify-center w-6 h-6 mt-0.5 mr-3 text-xs font-bold text-indigo-700 bg-indigo-100 rounded-full dark:text-indigo-300 dark:bg-indigo-900/60 shadow-sm">4</div>
                <p>一旦点击继续即表示无条件接受本协议。如果您拒绝，本软件将立即触发自毁与环境清理机制以确保设备安全。</p>
              </div>
            </div>
            
            <div class="mt-6 pt-5 border-t border-gray-100 dark:border-gray-800">
              <p class="mb-3 text-xs font-bold text-center text-gray-500 dark:text-gray-400">
                请输入「<span class="text-indigo-600 dark:text-indigo-400 select-all">{{ expectedConfirmText }}</span>」以解锁按钮
              </p>
              
              <div class="mb-5">
                <input 
                  type="text" 
                  v-model="confirmText" 
                  class="w-full px-4 py-3 text-center text-sm font-semibold bg-gray-50 border border-gray-200 rounded-xl focus:ring-2 focus:ring-indigo-500/50 focus:border-indigo-500 outline-none transition-all dark:bg-gray-800/80 dark:border-gray-700 dark:text-white dark:focus:border-indigo-500 placeholder-gray-400 dark:placeholder-gray-500"
                  :placeholder="`请输入：${expectedConfirmText}`"
                  @paste.prevent
                  autocomplete="off"
                />
              </div>

              <div class="flex gap-4">
                <button @click="rejectDisclaimer" class="flex-1 px-4 py-3 text-sm font-bold tracking-wide text-gray-500 transition-all duration-300 bg-gray-50 border border-gray-200 rounded-xl hover:bg-red-50 hover:text-red-600 hover:border-red-200 dark:bg-gray-800 dark:text-gray-400 dark:border-gray-700 dark:hover:bg-red-900/20 dark:hover:text-red-400 dark:hover:border-red-900/50">
                  拒绝并卸载
                </button>
                <button 
                  @click="acceptDisclaimer" 
                  :disabled="!canAccept"
                  :class="[
                    'flex-[1.5] px-4 py-3 text-sm font-bold tracking-wide transition-all duration-300 rounded-xl',
                    canAccept 
                      ? 'text-white bg-gradient-to-r from-[#5b5bd6] to-[#4a4ac2] shadow-lg shadow-indigo-500/30 hover:shadow-indigo-500/50 hover:-translate-y-0.5 active:translate-y-0 cursor-pointer'
                      : 'text-gray-400 dark:text-gray-500 bg-gray-200 dark:bg-gray-800 shadow-none cursor-not-allowed border border-gray-300 dark:border-gray-700 opacity-60'
                  ]"
                >
                  同意并继续
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style>
.fade-slide-enter-active,
.fade-slide-leave-active {
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.fade-slide-enter-from {
  opacity: 0;
  transform: translateY(20px);
}

.fade-slide-leave-to {
  opacity: 0;
  transform: translateY(-20px);
}

/* Custom Scrollbar */
::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}
::-webkit-scrollbar-track {
  background: transparent;
}
::-webkit-scrollbar-thumb {
  background: #cbd5e1;
  border-radius: 4px;
}
.dark ::-webkit-scrollbar-thumb {
  background: #334155;
}
::-webkit-scrollbar-thumb:hover {
  background: #94a3b8;
}
.dark ::-webkit-scrollbar-thumb:hover {
  background: #475569;
}
</style>
