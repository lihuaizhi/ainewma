// 跨组件共享的轻量状态：聊天收集到的 POI（供右栏小地图绘制）
import { reactive } from 'vue'
import type { POI } from './types'

/** 地图 POI 集合（去重），由 ChatPanel 在工具结果里收集 */
export const mapPois = reactive<POI[]>([])

/** 追加 POI（按 id 去重，最多保留 20 个） */
export function addPois(list: (POI | undefined)[] | undefined) {
  if (!list) return
  for (const p of list) {
    if (!p || typeof p.lng !== 'number' || typeof p.lat !== 'number') continue
    if (mapPois.some((x) => x.id === p.id)) continue
    mapPois.push(p)
    if (mapPois.length > 20) mapPois.shift()
  }
}

/** 从任意 tool_result.data 中递归收集 POI（route/pickup/dropoff/places/orders 等结构） */
export function collectPoisFromData(data: Record<string, unknown> | undefined) {
  if (!data) return
  // 常见位置：route.pickup / route.dropoff / pickup / dropoff / poi / places
  const d = data as Record<string, any>
  const route = d.route as Record<string, any> | undefined
  addPois([d.pickup, d.dropoff, d.poi, route?.pickup, route?.dropoff])
  for (const key of ['places', 'results', 'orders', 'options']) {
    const arr = d[key]
    if (Array.isArray(arr)) {
      for (const item of arr) {
        if (item && typeof item === 'object') {
          addPois([item as POI, item.pickup, item.dropoff])
        }
      }
    }
  }
}
