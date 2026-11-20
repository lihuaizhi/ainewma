<script setup lang="ts">
// 右栏订单面板：小地图 + 订单卡片列表（事件驱动 + 轮询兜底）
import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { Order, OrderStatus } from '../types'
import { STATUS_TEXT, STATUS_TYPE } from '../types'
import { cancelOrder, fetchOrders, openOrderEvents } from '../api'
import { mapPois } from '../store'
import MiniMap from './MiniMap.vue'

const sessionId = inject<ReturnType<typeof ref<string>>>('sessionId')!

/** 订单列表（按更新时间倒序） */
const orders = ref<Order[]>([])
/** 轮询句柄 */
let pollTimer: number | undefined
/** 事件流句柄 */
let eventSource: EventSource | null = null

/** 进行中订单（用于地图车辆动画） */
const activeOrder = computed<Order | null>(() => {
  const live = orders.value.filter((o) =>
    ['created', 'dispatched', 'accepted', 'arriving', 'in_trip'].includes(o.status),
  )
  return live[0] ?? null
})

/** 刷新订单列表 */
async function refreshOrders() {
  if (!sessionId.value) return
  try {
    const resp = await fetchOrders(sessionId.value)
    if (resp.orders) orders.value = [...resp.orders].reverse()
  } catch {
    // 静默失败（轮询兜底）
  }
}

/** 用事件里的订单就地更新（实时刷新） */
function upsertOrder(o: Order) {
  const idx = orders.value.findIndex((x) => x.order_id === o.order_id)
  if (idx >= 0) orders.value.splice(idx, 1, o)
  else orders.value.unshift(o)
  orders.value = [...orders.value]
}

/** 打开订单事件流（session 变化时重建） */
function openEvents() {
  closeEvents()
  if (!sessionId.value) return
  eventSource = openOrderEvents(
    sessionId.value,
    (ev) => {
      if (ev.order) upsertOrder(ev.order)
    },
    () => {
      // 断开时依赖轮询兜底
    },
  )
}

function closeEvents() {
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
}

/** 取消订单：失败时把 error（如 GUARD:NOT_CANCELLABLE）原样展示 */
async function onCancel(order: Order) {
  try {
    const resp = await cancelOrder(order.order_id, sessionId.value, '用户主动取消')
    if (resp.error) {
      ElMessage.error(resp.error) // 如 GUARD:NOT_CANCELLABLE
    } else if (resp.order) {
      upsertOrder(resp.order)
      ElMessage.success('订单已取消')
    }
  } catch (e) {
    ElMessage.error(`取消失败：${e instanceof Error ? e.message : String(e)}`)
  }
}

/** 状态 Tag 类型（cast 成 element-plus 支持的类型） */
function statusType(s: OrderStatus): any {
  return STATUS_TYPE[s] ?? 'info'
}

// session 变化：重新拉单 + 重开事件流
watch(
  () => sessionId.value,
  () => {
    orders.value = []
    refreshOrders()
    openEvents()
  },
)

onMounted(() => {
  refreshOrders()
  openEvents()
  pollTimer = window.setInterval(refreshOrders, 5000) // 轮询兜底
})

onBeforeUnmount(() => {
  closeEvents()
  if (pollTimer) window.clearInterval(pollTimer)
})
</script>

<template>
  <div class="order-panel">
    <!-- 顶部小地图 -->
    <div class="map-wrap">
      <MiniMap :pois="mapPois" :active-order="activeOrder" />
    </div>

    <!-- 订单列表 -->
    <div class="order-list">
      <div class="order-title">我的订单（{{ orders.length }}）</div>

      <div v-if="orders.length === 0" class="order-empty">暂无订单</div>

      <div v-for="o in orders" :key="o.order_id" class="order-card">
        <div class="order-head">
          <span class="order-id">#{{ o.order_id }}</span>
          <el-tag :type="statusType(o.status)" size="small" effect="light">
            {{ STATUS_TEXT[o.status] ?? o.status }}
          </el-tag>
        </div>

        <div class="order-route">
          <div class="route-line"><span class="dot pickup"></span>{{ o.pickup?.name }}</div>
          <div class="route-line"><span class="dot dropoff"></span>{{ o.dropoff?.name }}</div>
        </div>

        <div class="order-meta">
          <span class="order-price">💰 {{ (o.price_cents / 100).toFixed(2) }} 元</span>
          <span v-if="o.vehicle_name">🚗 {{ o.vehicle_name }}</span>
        </div>

        <!-- 司机信息 -->
        <div v-if="o.driver" class="order-driver">
          <span>👤 {{ o.driver.name }}</span>
          <span>🚙 {{ o.driver.plate }} · {{ o.driver.car }}</span>
          <span>⭐ {{ o.driver.rating.toFixed(1) }}</span>
          <span v-if="o.driver.eta_s > 0">ETA {{ Math.ceil(o.driver.eta_s / 60) }} 分钟</span>
        </div>

        <!-- 进行中订单：取消按钮 -->
        <div v-if="['created', 'dispatched', 'accepted', 'arriving'].includes(o.status)" class="order-actions">
          <el-button
            size="small"
            type="danger"
            plain
            @click="onCancel(o)"
          >
            取消订单
          </el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.order-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.map-wrap {
  flex: none;
  padding: 10px;
  border-bottom: 1px solid var(--border);
}
.order-list {
  flex: 1;
  overflow-y: auto;
  padding: 10px;
}
.order-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-2);
  margin-bottom: 8px;
}
.order-empty {
  color: var(--text-2);
  font-size: 13px;
  text-align: center;
  padding: 30px 0;
}
.order-card {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 10px;
  margin-bottom: 10px;
  font-size: 13px;
}
.order-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}
.order-id {
  font-weight: 600;
  color: var(--text);
}
.order-route {
  margin-bottom: 6px;
}
.route-line {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--text);
  line-height: 1.7;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
}
.dot.pickup {
  background: #67c23a;
}
.dot.dropoff {
  background: #f56c6c;
}
.order-meta {
  display: flex;
  gap: 12px;
  margin-bottom: 6px;
}
.order-price {
  color: #f56c6c;
  font-weight: 700;
}
.order-driver {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 10px;
  background: #f8f8f9;
  border-radius: 6px;
  padding: 6px 8px;
  color: var(--text-2);
  font-size: 12px;
  margin-bottom: 6px;
}
.order-actions {
  display: flex;
  justify-content: flex-end;
}
</style>
