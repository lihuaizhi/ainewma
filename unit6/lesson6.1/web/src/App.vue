<script setup lang="ts">
// 主布局：顶部窄 header + 左（会话列表）/ 中（聊天）/ 右（订单面板）
import { onMounted, provide, ref } from 'vue'
import SessionList from './components/SessionList.vue'
import ChatPanel from './components/ChatPanel.vue'
import OrderPanel from './components/OrderPanel.vue'
import type { SessionItem } from './types'
import { fetchHealth } from './api'

// engine 徽标：offline | llm
const engine = ref<'offline' | 'llm' | ''>('')
// 当前会话 id（空串表示新会话，等后端 done 事件返回）
const sessionId = ref('')
// 会话列表（localStorage 记忆）
const sessions = ref<SessionItem[]>(loadSessions())

/** 读取本地会话列表 */
function loadSessions(): SessionItem[] {
  try {
    const raw = localStorage.getItem('ride.sessions')
    return raw ? (JSON.parse(raw) as SessionItem[]) : []
  } catch {
    return []
  }
}

/** 持久化会话列表 */
function persistSessions() {
  localStorage.setItem('ride.sessions', JSON.stringify(sessions.value))
}

/** 新建会话 */
function newSession() {
  sessionId.value = ''
  // 新会话不立即落列表，等首次拿到 session_id 再登记（见 onSessionReady）
}

/** 切换会话 */
function selectSession(id: string) {
  if (id !== sessionId.value) sessionId.value = id
}

/** 首次收到 done 事件、拿到真实 session_id 时登记会话 */
function onSessionReady(id: string, title: string) {
  const exists = sessions.value.some((s) => s.id === id)
  if (!exists) {
    sessions.value.unshift({ id, title: title || '新会话', updatedAt: Date.now() })
    persistSessions()
  }
}

/** 更新会话标题（用首条用户消息）与活跃时间 */
function touchSession(id: string, title: string) {
  const s = sessions.value.find((x) => x.id === id)
  if (s) {
    s.updatedAt = Date.now()
    if (title && s.title === '新会话') s.title = title
    persistSessions()
  }
}

/** 供子组件读取/更新当前 session_id */
provide('sessionId', sessionId)
provide('sessions', sessions)
provide('onSessionReady', onSessionReady)
provide('touchSession', touchSession)

onMounted(async () => {
  try {
    const h = await fetchHealth()
    engine.value = h.engine
  } catch {
    engine.value = ''
  }
})
</script>

<template>
  <div class="app">
    <!-- 顶部窄 header -->
    <header class="app-header">
      <div class="brand">
        <span class="brand-icon">🚕</span>
        <span class="brand-name">打车 Agent</span>
      </div>
      <span v-if="engine" class="engine-badge" :class="engine">
        engine: {{ engine }}
      </span>
    </header>

    <!-- 三栏 -->
    <main class="app-body">
      <aside class="col col-left">
        <SessionList
          :sessions="sessions"
          :active-id="sessionId"
          @new-session="newSession"
          @select="selectSession"
        />
      </aside>
      <section class="col col-center">
        <ChatPanel />
      </section>
      <aside class="col col-right">
        <OrderPanel />
      </aside>
    </main>
  </div>
</template>

<style>
/* 全局：浅色主题统一 */
:root {
  --bg: #f5f7fa;
  --panel: #ffffff;
  --border: #e4e7ed;
  --text: #303133;
  --text-2: #909399;
  --primary: #409eff;
}
* {
  box-sizing: border-box;
}
html,
body,
#app {
  height: 100%;
  margin: 0;
}
body {
  font-family: 'PingFang SC', 'Microsoft YaHei', 'Helvetica Neue', Arial, sans-serif;
  background: var(--bg);
  color: var(--text);
}
</style>

<style scoped>
.app {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.app-header {
  height: 48px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  background: var(--panel);
  border-bottom: 1px solid var(--border);
}
.brand {
  display: flex;
  align-items: center;
  gap: 8px;
}
.brand-icon {
  font-size: 20px;
}
.brand-name {
  font-size: 16px;
  font-weight: 600;
}
.engine-badge {
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 10px;
  background: #e6f7ff;
  color: #1890ff;
  border: 1px solid #91d5ff;
}
.engine-badge.offline {
  background: #fff7e6;
  color: #d46b08;
  border-color: #ffd591;
}
.engine-badge.llm {
  background: #f6ffed;
  color: #389e0d;
  border-color: #b7eb8f;
}
.app-body {
  flex: 1;
  min-height: 0;
  display: flex;
}
.col {
  min-height: 0;
  min-width: 0;
}
.col-left {
  width: 220px;
  flex: none;
  background: var(--panel);
  border-right: 1px solid var(--border);
}
.col-center {
  flex: 1;
  display: flex;
}
.col-right {
  width: 340px;
  flex: none;
  background: var(--panel);
  border-left: 1px solid var(--border);
}
</style>
