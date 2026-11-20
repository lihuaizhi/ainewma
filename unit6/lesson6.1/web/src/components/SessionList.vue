<script setup lang="ts">
// 左栏：会话列表 + 新建会话按钮
import type { SessionItem } from '../types'

defineProps<{
  sessions: SessionItem[]
  activeId: string
}>()

const emit = defineEmits<{
  'new-session': []
  select: [id: string]
}>()

/** 相对时间展示 */
function timeAgo(ts: number): string {
  const diff = Date.now() - ts
  const m = Math.floor(diff / 60000)
  if (m < 1) return '刚刚'
  if (m < 60) return `${m} 分钟前`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} 小时前`
  return `${Math.floor(h / 24)} 天前`
}
</script>

<template>
  <div class="session-list">
    <div class="session-head">
      <el-button type="primary" size="small" class="btn-new" @click="emit('new-session')">
        ＋ 新建会话
      </el-button>
    </div>
    <div class="session-items">
      <div
        v-for="s in sessions"
        :key="s.id"
        class="session-item"
        :class="{ active: s.id === activeId }"
        @click="emit('select', s.id)"
      >
        <div class="session-title" :title="s.title">{{ s.title }}</div>
        <div class="session-time">{{ timeAgo(s.updatedAt) }}</div>
      </div>
      <el-empty
        v-if="sessions.length === 0"
        description="暂无会话"
        :image-size="48"
      />
    </div>
  </div>
</template>

<style scoped>
.session-list {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.session-head {
  padding: 10px;
  border-bottom: 1px solid var(--border);
}
.btn-new {
  width: 100%;
}
.session-items {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}
.session-item {
  padding: 8px 10px;
  border-radius: 6px;
  cursor: pointer;
  margin-bottom: 4px;
}
.session-item:hover {
  background: #f2f6fc;
}
.session-item.active {
  background: #ecf5ff;
}
.session-title {
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.session-time {
  font-size: 11px;
  color: var(--text-2);
  margin-top: 2px;
}
</style>
