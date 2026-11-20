// 与后端契约一一对应的类型定义（字段名严格一致）

/** 兴趣点（POI） */
export interface POI {
  id: string
  name: string
  address: string
  lng: number
  lat: number
  city: string
  category: 'airport' | 'railway' | 'office' | 'mall' | 'residential' | 'landmark' | 'hotel'
}

/** 路线上的一个可选方案 */
export interface RouteOption {
  label: '推荐' | '少拥堵' | '少收费'
  distance_m: number
  duration_s: number
  toll_cents: number
  steps: string[]
}

/** 计划路线 */
export interface Route {
  route_id: string
  pickup: POI
  dropoff: POI
  options: RouteOption[]
  departure: string
}

/** 报价明细（价格单位：分） */
export interface Breakdown {
  flagfall_cents: number
  distance_cents: number
  time_cents: number
  remote_cents: number
  toll_cents: number
  discount_cents: number
  surge: number
}

/** 报价 */
export interface Quote {
  quote_id: string
  route_id: string
  vehicle_class: string
  vehicle_name: string
  price_cents: number
  surge: number
  expires_at: string
  breakdown: Breakdown
}

/** 司机信息（未派单时为 null） */
export interface Driver {
  name: string
  plate: string
  car: string
  rating: number
  eta_s: number
}

/** 订单状态 */
export type OrderStatus =
  | 'created'
  | 'dispatched'
  | 'accepted'
  | 'arriving'
  | 'in_trip'
  | 'completed'
  | 'canceled'

/** 订单 */
export interface Order {
  order_id: string
  session_id: string
  quote_id: string
  pickup: POI
  dropoff: POI
  vehicle_class: string
  vehicle_name: string
  price_cents: number
  status: OrderStatus
  driver: Driver | null
  cancel_fee_cents: number
  distance_m: number
  duration_s: number
  created_at: string
  updated_at: string
}

/* ---------- 聊天 SSE 事件 ---------- */

/** 文本增量 */
export interface DeltaEvent {
  text: string
}

/** 工具调用 */
export interface ToolCallEvent {
  id: string
  name: string
  args: Record<string, unknown>
}

/** 工具结果 */
export interface ToolResultEvent {
  id: string
  ok: boolean
  summary: string
  data?: Record<string, unknown>
}

/** 错误 */
export interface ErrorEvent {
  message: string
}

/** 结束 */
export interface DoneEvent {
  session_id: string
}

/* ---------- 聊天消息模型（前端本地） ---------- */

export type MessageRole = 'user' | 'assistant'

/** 一条工具调用卡片的数据（call + 可选 result） */
export interface ToolEntry {
  call: ToolCallEvent
  result?: ToolResultEvent
}

/** 聊天消息 */
export interface ChatMessage {
  id: string
  role: MessageRole
  /** 文本内容（流式时追加） */
  text: string
  /** 该消息关联的工具卡片（按到达顺序） */
  tools?: ToolEntry[]
  /** 是否正在流式输出 */
  streaming?: boolean
}

/* ---------- 订单事件 SSE ---------- */

export interface OrderEvent {
  seq: number
  event: string
  prev_status?: string
  order: Order
}

/* ---------- REST 响应 ---------- */

export interface ConfirmResponse {
  ok: boolean
  quote_id: string
}

export interface OrdersResponse {
  orders: Order[]
}

export interface CancelResponse {
  order?: Order
  error?: string
}

export interface HealthResponse {
  ok: boolean
  engine: 'offline' | 'llm'
}

/** 会话（本地 localStorage 记忆） */
export interface SessionItem {
  id: string
  title: string
  updatedAt: number
}

/** 状态中文名（供状态 Tag / 面板展示） */
export const STATUS_TEXT: Record<OrderStatus, string> = {
  created: '已下单',
  dispatched: '派单中',
  accepted: '司机已接单',
  arriving: '前往上车点',
  in_trip: '行程中',
  completed: '已完成',
  canceled: '已取消',
}

/** 状态 Tag 颜色（Element Plus type） */
export const STATUS_TYPE: Record<OrderStatus, string> = {
  created: 'info',
  dispatched: 'warning',
  accepted: 'warning',
  arriving: 'primary',
  in_trip: 'primary',
  completed: 'success',
  canceled: 'danger',
}
