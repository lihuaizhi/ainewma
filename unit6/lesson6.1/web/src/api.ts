// 后端 API 封装：REST + 聊天 SSE（fetch + ReadableStream 自解析）+ 订单事件 SSE
import type {
  CancelResponse,
  ConfirmResponse,
  DoneEvent,
  ErrorEvent,
  HealthResponse,
  OrderEvent,
  OrdersResponse,
  ToolCallEvent,
  ToolResultEvent,
} from './types'

const JSON_HEADERS = { 'Content-Type': 'application/json' }

/** 聊天流式回调集合 */
export interface ChatHandlers {
  /** 文本增量 */
  onDelta?: (text: string) => void
  /** 工具调用 */
  onToolCall?: (call: ToolCallEvent) => void
  /** 工具结果 */
  onToolResult?: (result: ToolResultEvent) => void
  /** 错误 */
  onError?: (err: ErrorEvent) => void
  /** 结束（带回 session_id） */
  onDone?: (done: DoneEvent) => void
}

/**
 * 发送聊天消息并解析 SSE 流。
 * session_id 首次可为空串，从 done 事件取回。
 * 返回 abort 方法用于中途取消。
 */
export function sendChat(
  sessionId: string,
  message: string,
  handlers: ChatHandlers,
): { abort: () => void } {
  const controller = new AbortController()

  // 后台执行：解析 SSE 流
  ;(async () => {
    try {
      const resp = await fetch('/v1/chat', {
        method: 'POST',
        headers: JSON_HEADERS,
        body: JSON.stringify({ session_id: sessionId, message }),
        signal: controller.signal,
      })
      if (!resp.ok || !resp.body) {
        handlers.onError?.({ message: `请求失败：HTTP ${resp.status}` })
        return
      }

      const reader = resp.body.getReader()
      const decoder = new TextDecoder('utf-8')
      let buf = ''

      // 循环读取 chunk，按 SSE 规范（\n\n 分隔）逐条处理
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })

        let sep: number
        // 事件之间以空行分隔
        while ((sep = buf.indexOf('\n\n')) >= 0) {
          const raw = buf.slice(0, sep)
          buf = buf.slice(sep + 2)
          handleSSEBlock(raw, handlers)
        }
      }
      // 剩余缓冲（无结尾空行时兜底）
      if (buf.trim()) handleSSEBlock(buf, handlers)
    } catch (e) {
      // 主动 abort 不算错误
      if (!controller.signal.aborted) {
        handlers.onError?.({ message: `连接异常：${e instanceof Error ? e.message : String(e)}` })
      }
    }
  })()

  return { abort: () => controller.abort() }
}

/** 解析单个 SSE 块：event 行 + data 行 */
function handleSSEBlock(block: string, handlers: ChatHandlers): void {
  let event = 'message'
  const dataLines: string[] = []
  for (const line of block.split('\n')) {
    if (line.startsWith('event:')) event = line.slice(6).trim()
    else if (line.startsWith('data:')) dataLines.push(line.slice(5).trimStart())
  }
  if (dataLines.length === 0) return

  const dataStr = dataLines.join('\n')
  let data: any
  try {
    data = JSON.parse(dataStr)
  } catch {
    return // JSON 解析失败则忽略该条
  }

  switch (event) {
    case 'delta':
      if (typeof data.text === 'string') handlers.onDelta?.(data.text)
      break
    case 'tool_call':
      handlers.onToolCall?.(data as ToolCallEvent)
      break
    case 'tool_result':
      handlers.onToolResult?.(data as ToolResultEvent)
      break
    case 'error':
      handlers.onError?.((data?.message ? data : { message: String(data) }) as ErrorEvent)
      break
    case 'done':
      handlers.onDone?.(data as DoneEvent)
      break
  }
}

/** 订单事件流：GET /v1/events?session_id=xxx（EventSource） */
export function openOrderEvents(
  sessionId: string,
  onEvent: (e: OrderEvent) => void,
  onError?: () => void,
): EventSource {
  const es = new EventSource(`/v1/events?session_id=${encodeURIComponent(sessionId)}`)
  es.addEventListener('order', (ev) => {
    try {
      onEvent(JSON.parse((ev as MessageEvent).data) as OrderEvent)
    } catch {
      // 忽略坏数据
    }
  })
  es.onerror = () => onError?.()
  return es
}

/** GET /v1/health */
export async function fetchHealth(): Promise<HealthResponse> {
  const resp = await fetch('/v1/health', { headers: JSON_HEADERS })
  return (await resp.json()) as HealthResponse
}

/** POST /v1/quotes/{quote_id}/confirm 确认下单 */
export async function confirmQuote(quoteId: string, sessionId: string): Promise<ConfirmResponse> {
  const resp = await fetch(`/v1/quotes/${encodeURIComponent(quoteId)}/confirm`, {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ session_id: sessionId }),
  })
  return (await resp.json()) as ConfirmResponse
}

/** GET /v1/orders?session_id=xxx */
export async function fetchOrders(sessionId: string): Promise<OrdersResponse> {
  const resp = await fetch(`/v1/orders?session_id=${encodeURIComponent(sessionId)}`, {
    headers: JSON_HEADERS,
  })
  return (await resp.json()) as OrdersResponse
}

/** POST /v1/orders/{id}/cancel 取消订单 */
export async function cancelOrder(
  orderId: string,
  sessionId: string,
  reason: string,
): Promise<CancelResponse> {
  const resp = await fetch(`/v1/orders/${encodeURIComponent(orderId)}/cancel`, {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ session_id: sessionId, reason }),
  })
  return (await resp.json()) as CancelResponse
}

/** 把分转成 ¥ 展示文本（如 1234 -> ¥12.34） */
export function formatYuan(cents: number): string {
  return `¥${(cents / 100).toFixed(2)}`
}

/** 秒数转可读时长（如 3675 -> 1 小时 1 分） */
export function formatDuration(seconds: number): string {
  const m = Math.round(seconds / 60)
  if (m < 60) return `${m} 分钟`
  const h = Math.floor(m / 60)
  const rm = m % 60
  return rm ? `${h} 小时 ${rm} 分钟` : `${h} 小时`
}

/** 米转可读里程（如 3200 -> 3.2 公里） */
export function formatDistance(meters: number): string {
  if (meters < 1000) return `${Math.round(meters)} 米`
  return `${(meters / 1000).toFixed(1)} 公里`
}
