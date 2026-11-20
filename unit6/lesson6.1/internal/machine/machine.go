// Package machine 是订单状态机与模拟司机行为：created → dispatched →
// accepted → arriving → in_trip → completed，任意未上车状态可被用户取消
// （取消费规则在 RideProvider 服务端判定）。每次推进：
//  1. 更新 SQLite  2. 写 order_event 审计
//  3. 追加事件队列（seq 单调）供 gateway 轮询 /internal/events 广播前端
//
// 延时按 DEMO_SPEED 倍速压缩（默认 10x：整单 ~20s 走完）。
package machine

import (
	"fmt"
	"sync"
	"time"

	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/audit"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/provider"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/store"
)

// 基础延时（真实比例秒；实际 = base / speed）。
const (
	baseDispatchS = 5
	baseAcceptS   = 15
	baseArriveS   = 60
	baseTripS     = 120
)

// EventQueue 进程内事件环（gateway 轮询取增量；断线不丢事件）。
type EventQueue struct {
	mu   sync.Mutex
	buf  []provider.OrderEvent
	seq  int64
}

// NewEventQueue 环形缓冲（保 512 条）。
func NewEventQueue() *EventQueue { return &EventQueue{buf: make([]provider.OrderEvent, 0, 64)} }

// Push 追加事件并返回其 seq。
func (q *EventQueue) Push(ev provider.OrderEvent) int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seq++
	ev.Seq = q.seq
	q.buf = append(q.buf, ev)
	if len(q.buf) > 512 {
		q.buf = q.buf[len(q.buf)-512:]
	}
	return q.seq
}

// Since 取 seq 之后的事件。
func (q *EventQueue) Since(seq int64) []provider.OrderEvent {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []provider.OrderEvent
	for _, ev := range q.buf {
		if ev.Seq > seq {
			out = append(out, ev)
		}
	}
	return out
}

// Engine 驱动状态推进。
type Engine struct {
	store *store.Store
	audit *audit.Logger
	queue *EventQueue
	speed float64

	mu    sync.Mutex
	stops map[string]chan struct{}
}

// New 构造状态机引擎。
func New(st *store.Store, aud *audit.Logger, queue *EventQueue, speed float64) *Engine {
	if speed <= 0 {
		speed = 10
	}
	return &Engine{store: st, audit: aud, queue: queue, speed: speed,
		stops: map[string]chan struct{}{}}
}

// Launch 为新订单启动推进 goroutine（幂等：同一订单只启动一次）。
func (e *Engine) Launch(o *provider.Order) {
	e.mu.Lock()
	if _, ok := e.stops[o.OrderID]; ok {
		e.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	e.stops[o.OrderID] = stop
	e.mu.Unlock()

	go e.run(o, stop)
}

// StopAll 停止全部推进（进程退出）。
func (e *Engine) StopAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, ch := range e.stops {
		close(ch)
		delete(e.stops, id)
	}
}

func (e *Engine) dur(base int) time.Duration {
	return time.Duration(float64(base)/e.speed*1e9) * time.Nanosecond
}

// run 按时序推进 created→…→completed。
func (e *Engine) run(o *provider.Order, stop chan struct{}) {
	step := func(d time.Duration, to string, mutate func(*provider.Order)) bool {
		select {
		case <-stop:
			return false
		case <-time.After(d):
		}
		return e.transition(o.OrderID, to, mutate)
	}

	if !step(e.dur(baseDispatchS), provider.StatusDispatched, nil) {
		return
	}
	// 派单成功 → 分配司机
	drv := randomDriver()
	if !step(e.dur(baseAcceptS), provider.StatusAccepted, func(ord *provider.Order) {
		now := time.Now()
		ord.Driver = &drv
		ord.AcceptedAt = &now
	}) {
		return
	}
	if !step(e.dur(baseArriveS), provider.StatusArriving, func(ord *provider.Order) {
		ord.Driver.ETAS = 60
	}) {
		return
	}
	if !step(e.dur(baseTripS), provider.StatusInTrip, func(ord *provider.Order) {
		ord.Driver.ETAS = 0
	}) {
		return
	}
	_ = step(e.dur(3), provider.StatusCompleted, nil)
}

// transition 校验转移合法性并落库/审计/事件；失败（如已取消）返回 false。
func (e *Engine) transition(orderID, to string, mutate func(*provider.Order)) bool {
	o, err := e.store.GetOrder(orderID)
	if err != nil || !canTransition(o.Status, to) {
		return false // 订单已被取消或不存在：静默停
	}
	prev := o.Status
	if mutate != nil {
		mutate(o)
	}
	o.Status = to
	o.UpdatedAt = time.Now()
	if err := e.store.UpdateOrder(o); err != nil {
		return false
	}
	e.emit(prev, o)
	return true
}

// emit 写审计 + 事件队列。
func (e *Engine) emit(prev string, o *provider.Order) {
	detail := fmt.Sprintf("%s→%s order=%s", prev, o.Status, o.OrderID)
	e.audit.OrderEvent("", o.SessionID, "status_changed", detail)
	e.queue.Push(provider.OrderEvent{
		Event: "status_changed", PrevStatus: prev, Order: o,
	})
}

// Cancel 用户取消（服务端护栏入口）：校验状态与取消费，落库并停掉推进。
// 返回更新后的订单；不可取消返回 error（GUARD:NOT_CANCELLABLE）。
func (e *Engine) Cancel(orderID, reason string, ride provider.RideProvider) (*provider.Order, error) {
	o, err := e.store.GetOrder(orderID)
	if err != nil {
		return nil, err
	}
	fee, err := ride.CancelFee(o.Status, o.AcceptedAt, time.Now())
	if err != nil {
		e.audit.Guardrail("", o.SessionID, "cancel_rejected",
			fmt.Sprintf("order=%s status=%s", orderID, o.Status))
		return nil, err
	}
	prev := o.Status
	o.Status = provider.StatusCanceled
	o.CancelFeeCents = fee
	o.UpdatedAt = time.Now()
	if err := e.store.UpdateOrder(o); err != nil {
		return nil, err
	}
	// 停掉推进 goroutine
	e.mu.Lock()
	if ch, ok := e.stops[orderID]; ok {
		close(ch)
		delete(e.stops, orderID)
	}
	e.mu.Unlock()

	e.audit.OrderEvent("", o.SessionID, "canceled",
		fmt.Sprintf("order=%s reason=%q fee=%d", orderID, reason, fee))
	e.queue.Push(provider.OrderEvent{
		Event: "status_changed", PrevStatus: prev, Order: o,
	})
	return o, nil
}

// canTransition 状态机转移表。
func canTransition(from, to string) bool {
	if to == provider.StatusCanceled {
		// 只有未上车状态可被状态机之外的取消改写
		return from == provider.StatusCreated || from == provider.StatusDispatched ||
			from == provider.StatusAccepted || from == provider.StatusArriving
	}
	switch from {
	case provider.StatusCreated:
		return to == provider.StatusDispatched
	case provider.StatusDispatched:
		return to == provider.StatusAccepted
	case provider.StatusAccepted:
		return to == provider.StatusArriving
	case provider.StatusArriving:
		return to == provider.StatusInTrip
	case provider.StatusInTrip:
		return to == provider.StatusCompleted
	}
	return false
}

// drivers mock 司机池。
var drivers = []provider.Driver{
	{Name: "王师傅", Plate: "京A·8F2K7", Car: "丰田卡罗拉 白", Rating: 4.9},
	{Name: "李师傅", Plate: "京N·5T9Q3", Car: "大众朗逸 银", Rating: 4.8},
	{Name: "赵师傅", Plate: "京P·2H6M8", Car: "比亚迪秦 黑", Rating: 4.9},
	{Name: "陈师傅", Plate: "京Q·7L1N4", Car: "本田思域 红", Rating: 4.7},
	{Name: "刘师傅", Plate: "京Y·3D5C9", Car: "日产轩逸 白", Rating: 4.8},
}

var drvMu sync.Mutex
var drvIdx int

func randomDriver() provider.Driver {
	drvMu.Lock()
	defer drvMu.Unlock()
	d := drivers[drvIdx%len(drivers)]
	drvIdx++
	d.ETAS = 180 // 初始 3 分钟到达
	return d
}
