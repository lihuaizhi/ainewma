<script setup lang="ts">
// 报价卡：车型、价格、里程/时长（若可得）、溢价倍数、60 秒倒计时、确认下单按钮
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { Quote, Route, ToolEntry } from '../types'
import { confirmQuote, formatDistance, formatDuration, formatYuan } from '../api'

const props = defineProps<{
  quote: Quote
  sessionId: string
  /** 该消息里所有工具卡片，用于回查 route（里程/时长） */
  tools: ToolEntry[]
}>()

const emit = defineEmits<{
  /** 确认成功（父级据此自动发"确认下单"消息） */
  confirmed: []
}>()

/** 是否已确认 */
const confirmed = ref(false)
const confirming = ref(false)
/** 剩余秒数 */
const remain = ref(60)
let timer: number | undefined

/** 从同消息的 tool_result 里找 route（quote.route_id 匹配） */
const route = computed<Route | null>(() => {
  for (const t of props.tools) {
    const data = t.result?.data
    if (!data) continue
    const r = (data.route ?? data) as Route
    if (r && r.route_id === props.quote.route_id && Array.isArray(r.options)) return r
  }
  return null
})

/** 路线默认（推荐）选项，用于展示里程/时长 */
const option = computed(() => route.value?.options?.[0] ?? null)

/** 起终点名（优先从 route 拿） */
const pickupName = computed(() => route.value?.pickup?.name ?? '')
const dropoffName = computed(() => route.value?.dropoff?.name ?? '')

/** 溢价文案 */
const surgeText = computed(() => {
  const s = props.quote.surge
  if (!s || Math.abs(s - 1) < 0.01) return '无溢价'
  return `${s.toFixed(1)}x 溢价`
})

/** 启动 60 秒倒计时（按 expires_at 校准） */
function startCountdown() {
  const end = props.quote.expires_at ? new Date(props.quote.expires_at).getTime() : 0
  const tick = () => {
    if (end) {
      remain.value = Math.max(0, Math.ceil((end - Date.now()) / 1000))
    } else {
      remain.value = Math.max(0, remain.value - 1)
    }
  }
  tick()
  timer = window.setInterval(tick, 1000)
}

/** 确认下单：POST /v1/quotes/{quote_id}/confirm */
async function onConfirm() {
  if (confirmed.value || confirming.value || remain.value <= 0) return
  confirming.value = true
  try {
    const resp = await confirmQuote(props.quote.quote_id, props.sessionId)
    if (resp.ok) {
      confirmed.value = true
      ElMessage.success('已确认下单')
      emit('confirmed') // 通知父级自动发"确认下单"消息触发后端下单
    } else {
      ElMessage.error('确认下单失败')
    }
  } catch (e) {
    ElMessage.error(`确认下单失败：${e instanceof Error ? e.message : String(e)}`)
  } finally {
    confirming.value = false
  }
}

onMounted(startCountdown)
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<template>
  <div class="quote-card">
    <div class="quote-head">
      <span class="quote-vehicle">🚗 {{ quote.vehicle_name }}</span>
      <span v-if="pickupName" class="quote-route">{{ pickupName }} → {{ dropoffName }}</span>
    </div>

    <div class="quote-price-row">
      <span class="quote-price">{{ formatYuan(quote.price_cents) }}</span>
      <el-tag size="small" :type="quote.surge > 1 ? 'warning' : 'info'" effect="plain">
        {{ surgeText }}
      </el-tag>
    </div>

    <!-- 里程/时长（拿得到 route 才显示） -->
    <div v-if="option" class="quote-meta">
      <span>📏 {{ formatDistance(option.distance_m) }}</span>
      <span>⏱ {{ formatDuration(option.duration_s) }}</span>
      <span v-if="option.toll_cents > 0">🎫 过路费 {{ formatYuan(option.toll_cents) }}</span>
    </div>

    <div class="quote-foot">
      <span class="quote-countdown" :class="{ warn: remain <= 10 }">
        {{ remain > 0 ? `${remain}s 后失效` : '已失效' }}
      </span>
      <el-button
        size="small"
        type="primary"
        :loading="confirming"
        :disabled="confirmed || remain <= 0"
        @click="onConfirm"
      >
        {{ confirmed ? '已确认 ✓' : '确认下单' }}
      </el-button>
    </div>
  </div>
</template>

<style scoped>
.quote-card {
  margin: 8px 0;
  padding: 10px 12px;
  border: 1px solid #b3d8ff;
  border-radius: 10px;
  background: linear-gradient(135deg, #f0f9ff 0%, #ffffff 60%);
}
.quote-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.quote-vehicle {
  font-weight: 600;
  font-size: 14px;
}
.quote-route {
  font-size: 12px;
  color: var(--text-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.quote-price-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin: 8px 0 4px;
}
.quote-price {
  font-size: 22px;
  font-weight: 700;
  color: #f56c6c;
}
.quote-meta {
  display: flex;
  gap: 14px;
  font-size: 12px;
  color: var(--text-2);
  margin-bottom: 8px;
}
.quote-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.quote-countdown {
  font-size: 12px;
  color: var(--text-2);
}
.quote-countdown.warn {
  color: #f56c6c;
  font-weight: 600;
}
</style>
