<script setup lang="ts">
// 中栏聊天区：消息列表（流式）+ 工具卡片 + 报价卡 + 输入框
import { computed, inject, nextTick, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { ChatMessage, SessionItem, ToolEntry } from '../types'
import { sendChat } from '../api'
import { collectPoisFromData } from '../store'
import MessageItem from './MessageItem.vue'

// 由 App 提供：当前会话 id、会话列表、登记/更新会话
const sessionId = inject<ReturnType<typeof ref<string>>>('sessionId')!
const sessions = inject<ReturnType<typeof ref<SessionItem[]>>>('sessions')!
const onSessionReady = inject<(id: string, title: string) => void>('onSessionReady')!
const touchSession = inject<(id: string, title: string) => void>('touchSession')!

/** 每个会话的本地消息缓存：sessionKey -> messages（新会话用临时 key） */
const messagesMap = ref<Record<string, ChatMessage[]>>({})
/** 临时 key（新会话未拿到 session_id 前） */
let tempKey = 0

/** 当前会话的消息列表 */
const currentKey = computed(() => sessionId.value || `temp-${tempKey}`)
const messages = computed<ChatMessage[]>(() => messagesMap.value[currentKey.value] ?? [])

/** 输入内容 */
const input = ref('')
/** 是否正在流式请求 */
const sending = ref(false)
/** 当前流式助手消息 id */
let streamingId = ''

const listRef = ref<HTMLElement | null>(null)

/** 切换会话时滚到底 */
watch(
  () => currentKey.value,
  () => scrollBottom(),
)

/** 滚动到底 */
function scrollBottom() {
  nextTick(() => {
    const el = listRef.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

/** 生成消息 id */
let msgSeq = 0
function newId(prefix = 'm'): string {
  return `${prefix}${++msgSeq}`
}

/** 发送一条消息 */
function send(text?: string) {
  const content = (text ?? input.value).trim()
  if (!content || sending.value) return

  // 若是新会话，登记临时 key 对应的消息数组
  const key = currentKey.value
  if (!messagesMap.value[key]) messagesMap.value[key] = []

  // 追加用户气泡
  messagesMap.value[key].push({ id: newId('u'), role: 'user', text: content })
  // 用首条用户消息作为会话标题
  if (sessionId.value) touchSession(sessionId.value, content.slice(0, 20))
  input.value = ''
  sending.value = true
  scrollBottom()

  // 追加助手占位消息，后续 delta / tool 事件都挂到它上面
  const assistant: ChatMessage = { id: newId('a'), role: 'assistant', text: '', tools: [], streaming: true }
  messagesMap.value[key].push(assistant)
  streamingId = assistant.id
  scrollBottom()

  sendChat(sessionId.value, content, {
    onDelta: (t) => {
      const m = findMsg(key, assistant.id)
      if (m) m.text += t
      scrollBottom()
    },
    onToolCall: (call) => {
      const m = findMsg(key, assistant.id)
      if (m) {
        if (!m.tools) m.tools = []
        const entry: ToolEntry = { call }
        m.tools.push(entry)
      }
      scrollBottom()
    },
    onToolResult: (result) => {
      const m = findMsg(key, assistant.id)
      if (m && m.tools) {
        // 找到同 id 的 call 并挂上 result；找不到则单独成卡
        const entry = m.tools.find((t) => t.call.id === result.id && !t.result)
        if (entry) entry.result = result
        else m.tools.push({ call: { id: result.id, name: result.summary, args: {} }, result })
      }
      // 收集 POI 供地图绘制
      collectPoisFromData(result.data)
      scrollBottom()
    },
    onError: (err) => {
      ElMessage.error(err.message)
      const m = findMsg(key, assistant.id)
      if (m && !m.text) m.text = `⚠️ ${err.message}`
      finishSend()
    },
    onDone: (done) => {
      const m = findMsg(key, assistant.id)
      if (m) m.streaming = false
      // 拿到真实 session_id：登记会话；并把临时 key 的消息迁移过去
      if (done.session_id) {
        const firstUser = messagesMap.value[key]?.find((x) => x.role === 'user')
        if (!sessionId.value) {
          // 新会话：把 temp 消息搬到真实 session key
          if (key.startsWith('temp-') && messagesMap.value[key]) {
            messagesMap.value[done.session_id] = messagesMap.value[key]
            delete messagesMap.value[key]
          }
          sessionId.value = done.session_id
          onSessionReady(done.session_id, firstUser?.text.slice(0, 20) || '新会话')
        } else {
          touchSession(sessionId.value, firstUser?.text.slice(0, 20) || '')
        }
      }
      finishSend()
    },
  })
}

/** 报价卡确认成功后自动发"确认下单"触发后端下单 */
function onQuoteConfirmed() {
  setTimeout(() => send('确认下单'), 300)
}

/** 按 id 找消息 */
function findMsg(key: string, id: string): ChatMessage | undefined {
  return messagesMap.value[key]?.find((x) => x.id === id)
}

/** 结束一次发送 */
function finishSend() {
  sending.value = false
  const m = Object.values(messagesMap.value)
    .flat()
    .find((x) => x.id === streamingId)
  if (m) m.streaming = false
  scrollBottom()
}

/** 回车发送，Shift+回车换行 */
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    send()
  }
}
</script>

<template>
  <div class="chat-panel">
    <!-- 消息列表 -->
    <div ref="listRef" class="chat-list">
      <div v-if="messages.length === 0" class="chat-empty">
        <div class="empty-icon">🚕</div>
        <div>你好，我是打车 Agent。试试：</div>
        <div class="empty-hint">“帮我从首都机场打车到国贸，多少钱？”</div>
      </div>
      <MessageItem
        v-for="m in messages"
        :key="m.id"
        :message="m"
        :session-id="sessionId"
        @quote-confirmed="onQuoteConfirmed"
      />
    </div>

    <!-- 输入区 -->
    <div class="chat-input">
      <el-input
        v-model="input"
        type="textarea"
        :rows="2"
        resize="none"
        placeholder="输入出行需求，回车发送，Shift+回车换行"
        :disabled="sending"
        @keydown="onKeydown"
      />
      <el-button
        type="primary"
        class="send-btn"
        :loading="sending"
        :disabled="!input.trim()"
        @click="send()"
      >
        发送
      </el-button>
    </div>
  </div>
</template>

<style scoped>
.chat-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  width: 100%;
  background: var(--bg);
}
.chat-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px 0 16px;
}
.chat-empty {
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--text-2);
  font-size: 14px;
}
.empty-icon {
  font-size: 40px;
}
.empty-hint {
  padding: 6px 14px;
  background: #fff;
  border: 1px solid var(--border);
  border-radius: 16px;
  font-size: 13px;
}
.chat-input {
  flex: none;
  display: flex;
  gap: 8px;
  padding: 10px 16px;
  background: var(--panel);
  border-top: 1px solid var(--border);
  align-items: flex-end;
}
.chat-input :deep(.el-textarea__inner) {
  resize: none;
}
.send-btn {
  height: 40px;
}
</style>
