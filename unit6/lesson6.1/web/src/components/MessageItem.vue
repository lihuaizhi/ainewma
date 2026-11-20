<script setup lang="ts">
// 单条消息：用户/助手气泡 + 工具卡片 + 报价卡
// 简单 markdown：仅渲染 **加粗** 与换行
import { computed } from 'vue'
import type { ChatMessage, Quote, ToolEntry } from '../types'
import ToolCard from './ToolCard.vue'
import QuoteCard from './QuoteCard.vue'

const props = defineProps<{
  message: ChatMessage
  sessionId: string
}>()

const emit = defineEmits<{
  /** 报价卡确认成功后，由父组件自动发"确认下单"消息 */
  'quote-confirmed': []
}>()

/** 从工具结果中提取报价对象（data.quote 或 data 本身即 quote） */
const quotes = computed<Quote[]>(() => {
  const list: Quote[] = []
  for (const t of props.message.tools ?? []) {
    const d = t.result?.data
    if (!d) continue
    const cand = (d.quote ?? d) as Quote
    if (cand && typeof cand.quote_id === 'string' && typeof cand.price_cents === 'number') {
      list.push(cand)
    }
  }
  return list
})

/** 该报价所属的工具卡片集合（用于回查 route 的里程/时长） */
const toolsRef = computed<ToolEntry[]>(() => props.message.tools ?? [])

/**
 * 简单 markdown 渲染：
 * 1. 先 HTML 转义，防止注入
 * 2. **加粗** -> <strong>
 * 3. 换行 -> <br>
 */
const html = computed(() => {
  const esc = props.message.text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
  const bolded = esc.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
  return bolded.replace(/\n/g, '<br>')
})
</script>

<template>
  <div class="msg" :class="message.role">
    <div class="avatar">{{ message.role === 'user' ? '🧑' : '🤖' }}</div>
    <div class="bubble-wrap">
      <!-- 文本气泡 -->
      <div v-if="message.text || message.streaming" class="bubble" v-html="html"></div>

      <!-- 工具卡片（跟在文本之后） -->
      <template v-for="(t, i) in message.tools" :key="i">
        <ToolCard :entry="t" />
        <!-- 报价卡：当该 tool_result.data 中存在 quote 对象时渲染 -->
        <QuoteCard
          v-if="t.result?.data && (t.result.data.quote || (t.result.data.quote_id && t.result.data.price_cents))"
          :quote="(t.result.data.quote ?? t.result.data) as Quote"
          :session-id="sessionId"
          :tools="toolsRef"
          @confirmed="emit('quote-confirmed')"
        />
      </template>
    </div>
  </div>
</template>

<style scoped>
.msg {
  display: flex;
  gap: 8px;
  margin: 12px 16px;
}
.msg.user {
  flex-direction: row-reverse;
}
.avatar {
  width: 30px;
  height: 30px;
  flex: none;
  border-radius: 50%;
  background: #eef1f6;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
}
.bubble-wrap {
  max-width: 78%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.bubble {
  padding: 9px 12px;
  border-radius: 10px;
  font-size: 14px;
  line-height: 1.6;
  word-break: break-word;
}
.msg.assistant .bubble {
  background: #f0f2f5;
  border-top-left-radius: 3px;
}
.msg.user .bubble {
  background: #ecf5ff;
  border-top-right-radius: 3px;
}
.bubble :deep(strong) {
  font-weight: 700;
  color: #303133;
}
</style>
