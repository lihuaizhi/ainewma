<script setup lang="ts">
// 工具调用卡片：🔧 工具名 + 参数摘要；结果折叠显示 summary；可展开看原始 JSON
import { computed, ref } from 'vue'
import type { ToolEntry } from '../types'

const props = defineProps<{ entry: ToolEntry }>()

const showData = ref(false)

/** 参数摘要：取前几个键值压缩成一行 */
const argsSummary = computed(() => {
  const args = props.entry.call?.args ?? {}
  const keys = Object.keys(args)
  if (keys.length === 0) return '{}'
  const parts = keys.slice(0, 4).map((k) => {
    let v = args[k]
    if (typeof v === 'object' && v !== null) {
      // 对象简写：{name:"xx"} 或 {name:"..."}
      const inner = (v as Record<string, unknown>)
      v = inner.name ?? inner.label ?? '{…}'
    }
    return `${k}=${String(v)}`
  })
  if (keys.length > 4) parts.push('…')
  return parts.join('  ')
})

/** 工具名 */
const name = computed(() => props.entry.call?.name ?? 'tool')

/** 结果原始 JSON（可展开） */
const dataJson = computed(() => {
  const r = props.entry.result
  if (!r) return ''
  return JSON.stringify(r.data ?? {}, null, 2)
})
</script>

<template>
  <div class="tool-card">
    <!-- 调用行 -->
    <div class="tool-head">
      <span class="tool-name">🔧 {{ name }}</span>
      <span class="tool-args" :title="argsSummary">{{ argsSummary }}</span>
    </div>

    <!-- 结果行（折叠） -->
    <div v-if="entry.result" class="tool-result" :class="{ err: !entry.result.ok }">
      <div class="result-summary" @click="showData = !showData">
        <span class="result-flag">{{ entry.result.ok ? '✓' : '✗' }}</span>
        <span class="result-text">{{ entry.result.summary }}</span>
        <span class="result-toggle">{{ showData ? '收起' : '展开' }}</span>
      </div>
      <pre v-if="showData" class="result-data">{{ dataJson }}</pre>
    </div>
    <div v-else class="tool-pending">执行中…</div>
  </div>
</template>

<style scoped>
.tool-card {
  margin: 6px 0;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: #fafbfc;
  font-size: 13px;
  overflow: hidden;
}
.tool-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
}
.tool-name {
  font-weight: 600;
  color: #e6a23c;
  flex: none;
}
.tool-args {
  color: var(--text-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tool-result {
  border-top: 1px dashed var(--border);
}
.result-summary {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  cursor: pointer;
}
.result-flag {
  color: #67c23a;
  font-weight: 700;
}
.tool-result.err .result-flag {
  color: #f56c6c;
}
.tool-result.err .result-text {
  color: #f56c6c;
}
.result-text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.result-toggle {
  color: var(--primary);
  font-size: 12px;
  flex: none;
}
.result-data {
  margin: 0;
  padding: 8px 10px;
  background: #f4f4f5;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 220px;
  overflow: auto;
}
.tool-pending {
  padding: 6px 10px;
  color: var(--text-2);
  border-top: 1px dashed var(--border);
  font-size: 12px;
}
</style>
