<script setup lang="ts">
// 顶部小地图：纯 SVG 自绘，坐标用 lng/lat 线性映射到画布
import { computed } from 'vue'
import type { Order, POI } from '../types'

const props = defineProps<{
  /** 收集到的 POI 点（来自最近 tool_result） */
  pois: POI[]
  /** 进行中的订单（有则画路线 + 移动圆点） */
  activeOrder: Order | null
}>()

const W = 300
const H = 160

/** 按订单状态推算车辆进度 0..1 */
const progress = computed(() => {
  const o = props.activeOrder
  if (!o) return 0
  switch (o.status) {
    case 'created':
    case 'dispatched':
      return 0 // 停在 pickup
    case 'accepted':
    case 'arriving':
      return 0.35 // 从途中向 pickup 移动（示意值，逐步靠近起点）
    case 'in_trip':
      return 0.6 // 沿线从 pickup 向 dropoff 移动（基础进度）
    case 'completed':
      return 1 // 到达 dropoff
    default:
      return 0
  }
})

/** 需要展示的点：POI 列表 + 当前订单起终点 */
const allPois = computed<POI[]>(() => {
  const list = [...props.pois]
  const o = props.activeOrder
  if (o) {
    for (const p of [o.pickup, o.dropoff]) {
      if (p && !list.some((x) => x.id === p.id)) list.push(p)
    }
  }
  return list.slice(0, 12) // 最多画 12 个点，避免拥挤
})

/** 经纬度 -> 画布坐标（线性映射，带留白） */
const bounds = computed(() => {
  const pts = allPois.value
  if (pts.length === 0) return null
  const lngs = pts.map((p) => p.lng)
  const lats = pts.map((p) => p.lat)
  let minLng = Math.min(...lngs)
  let maxLng = Math.max(...lngs)
  let minLat = Math.min(...lats)
  let maxLat = Math.max(...lats)
  // 防止所有点重合（单点/范围为 0）
  if (maxLng - minLng < 1e-6) {
    minLng -= 0.01
    maxLng += 0.01
  }
  if (maxLat - minLat < 1e-6) {
    minLat -= 0.01
    maxLat += 0.01
  }
  return { minLng, maxLng, minLat, maxLat }
})

/** POI 点坐标列表（含标签） */
const dots = computed(() => {
  const b = bounds.value
  if (!b) return []
  const pad = 18
  return allPois.value.map((p) => ({
    ...p,
    x: pad + ((p.lng - b.minLng) / (b.maxLng - b.minLng)) * (W - pad * 2),
    y: H - pad - ((p.lat - b.minLat) / (b.maxLat - b.minLat)) * (H - pad * 2),
  }))
})

/** 订单起终点坐标 */
const routePts = computed(() => {
  const o = props.activeOrder
  if (!o) return null
  const pickup = dots.value.find((d) => d.id === o.pickup?.id)
  const dropoff = dots.value.find((d) => d.id === o.dropoff?.id)
  if (!pickup || !dropoff) return null
  return { pickup, dropoff }
})

/** 车辆圆点位置（沿 pickup->dropoff 直线插值） */
const vehiclePos = computed(() => {
  const r = routePts.value
  if (!r) return null
  const t = progress.value
  return {
    x: r.pickup.x + (r.dropoff.x - r.pickup.x) * t,
    y: r.pickup.y + (r.dropoff.y - r.pickup.y) * t,
  }
})

/** 进行中订单状态决定是否显示车辆（completed/canceled 不动） */
const vehicleVisible = computed(() => {
  const o = props.activeOrder
  return !!o && !['completed', 'canceled'].includes(o.status)
})
</script>

<template>
  <div class="mini-map">
    <svg :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="xMidYMid meet" class="map-svg">
      <!-- 背景网格 -->
      <defs>
        <pattern id="grid" width="20" height="20" patternUnits="userSpaceOnUse">
          <path d="M20 0 L0 0 0 20" fill="none" stroke="#eef1f6" stroke-width="1" />
        </pattern>
      </defs>
      <rect :width="W" :height="H" fill="#f8fafc" />
      <rect :width="W" :height="H" fill="url(#grid)" />

      <!-- 订单路线：pickup -> dropoff -->
      <line
        v-if="routePts"
        :x1="routePts.pickup.x"
        :y1="routePts.pickup.y"
        :x2="routePts.dropoff.x"
        :y2="routePts.dropoff.y"
        stroke="#409eff"
        stroke-width="2.5"
        stroke-dasharray="6 4"
      />

      <!-- POI 点 -->
      <g v-for="d in dots" :key="d.id">
        <circle :cx="d.x" :cy="d.y" r="4.5" fill="#67c23a" stroke="#fff" stroke-width="1.5" />
        <text :x="d.x + 7" :y="d.y + 3" class="poi-label">{{ d.name }}</text>
      </g>

      <!-- 车辆移动圆点 -->
      <g v-if="vehicleVisible && vehiclePos">
        <circle :cx="vehiclePos.x" :cy="vehiclePos.y" r="7" fill="#ff9800" stroke="#fff" stroke-width="2" />
        <text :x="vehiclePos.x" :y="vehiclePos.y + 3.5" class="car-label">🚕</text>
      </g>
    </svg>
    <div v-if="allPois.length === 0" class="map-empty">暂无位置数据</div>
  </div>
</template>

<style scoped>
.mini-map {
  position: relative;
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
  background: #f8fafc;
}
.map-svg {
  display: block;
  width: 100%;
  height: 160px;
}
.poi-label {
  font-size: 9px;
  fill: #606266;
  paint-order: stroke;
  stroke: #f8fafc;
  stroke-width: 2px;
}
.car-label {
  font-size: 8px;
  text-anchor: middle;
}
.map-empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-2);
  font-size: 12px;
}
</style>
