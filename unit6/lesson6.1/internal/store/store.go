// Package store 是 ride-ops 侧的 SQLite 持久层（WAL，单写者 = ride-mcp 进程）：
// quotes（报价+确认令牌）、orders（订单+幂等键）、saved_places（常用地址）。
// gateway 不直接访问本库——数据主权归能力方（方案 §三）。
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/provider"
)

// ErrNotFound 记录不存在。
var ErrNotFound = errors.New("store: not found")

// ErrConflict 唯一键冲突（幂等：同一 quote 只能下一单）。
var ErrConflict = errors.New("store: conflict")

// Store SQLite 句柄。
type Store struct {
	db *sql.DB
}

// Open 打开（或初始化）数据库。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	schema := `
CREATE TABLE IF NOT EXISTS quotes (
  quote_id        TEXT PRIMARY KEY,
  session_id      TEXT NOT NULL,
  route_id        TEXT NOT NULL,
  vehicle_class   TEXT NOT NULL,
  price_cents     INTEGER NOT NULL,
  breakdown_json  TEXT NOT NULL,
  surge           REAL NOT NULL,
  pickup_json     TEXT NOT NULL,
  dropoff_json    TEXT NOT NULL,
  expires_at      INTEGER NOT NULL,   -- unix 秒
  confirmed_at    INTEGER,            -- 确认时刻（签发令牌）
  confirm_token   TEXT,
  token_expires   INTEGER,
  consumed_at     INTEGER,            -- 下单核销
  created_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_quotes_session ON quotes(session_id, created_at);

CREATE TABLE IF NOT EXISTS orders (
  order_id        TEXT PRIMARY KEY,
  session_id      TEXT NOT NULL,
  quote_id        TEXT NOT NULL UNIQUE,   -- 幂等：一个报价只能下一单
  pickup_json     TEXT NOT NULL,
  dropoff_json    TEXT NOT NULL,
  vehicle_class   TEXT NOT NULL,
  vehicle_name    TEXT NOT NULL,
  price_cents     INTEGER NOT NULL,
  status          TEXT NOT NULL,
  driver_json     TEXT,
  cancel_fee      INTEGER NOT NULL DEFAULT 0,
  distance_m      INTEGER NOT NULL DEFAULT 0,
  duration_s      INTEGER NOT NULL DEFAULT 0,
  accepted_at     INTEGER,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_orders_session ON orders(session_id, created_at);

CREATE TABLE IF NOT EXISTS saved_places (
  session_id TEXT NOT NULL,
  tag        TEXT NOT NULL,   -- home | work
  poi_json   TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (session_id, tag)
);`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: init schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭连接。
func (s *Store) Close() { _ = s.db.Close() }

func unix(t time.Time) int64        { return t.Unix() }
func ts(v int64) time.Time           { return time.Unix(v, 0) }
func nullTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := ts(v.Int64)
	return &t
}

// ---------- quotes ----------

// InsertQuote 落一条新报价。
func (s *Store) InsertQuote(q *provider.Quote) error {
	bd, _ := json.Marshal(q.Breakdown)
	pu, _ := json.Marshal(q.Pickup)
	do, _ := json.Marshal(q.Dropoff)
	_, err := s.db.Exec(`INSERT INTO quotes
	  (quote_id, session_id, route_id, vehicle_class, price_cents, breakdown_json,
	   surge, pickup_json, dropoff_json, expires_at, created_at)
	  VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		q.QuoteID, q.SessionID, q.RouteID, q.VehicleClass, q.PriceCents, string(bd),
		q.Surge, string(pu), string(do), unix(q.ExpiresAt), unix(time.Now()))
	return err
}

// scanQuote 行 → Quote（含服务端私有字段）。
func scanQuote(row interface{ Scan(...any) error }) (*provider.Quote, error) {
	var (
		q          provider.Quote
		bd, pu, do string
		expires    int64
		confirmed  sql.NullInt64
		token      sql.NullString
		tokExp     sql.NullInt64
		consumed   sql.NullInt64
		createdAt  int64
	)
	err := row.Scan(&q.QuoteID, &q.SessionID, &q.RouteID, &q.VehicleClass,
		&q.PriceCents, &bd, &q.Surge, &pu, &do, &expires,
		&confirmed, &token, &tokExp, &consumed, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(bd), &q.Breakdown)
	_ = json.Unmarshal([]byte(pu), &q.Pickup)
	_ = json.Unmarshal([]byte(do), &q.Dropoff)
	q.ExpiresAt = ts(expires)
	q.Confirmed = confirmed.Valid
	q.ConsumedAt = nullTime(consumed)
	if token.Valid {
		q.ConfirmToken = token.String
	}
	if tokExp.Valid {
		q.TokenExpiresAt = ts(tokExp.Int64)
	}
	return &q, nil
}

const quoteCols = `quote_id, session_id, route_id, vehicle_class, price_cents,
  breakdown_json, surge, pickup_json, dropoff_json, expires_at,
  confirmed_at, confirm_token, token_expires, consumed_at, created_at`

// GetQuote 按 id 取报价（含私有字段）。
func (s *Store) GetQuote(id string) (*provider.Quote, error) {
	return scanQuote(s.db.QueryRow(`SELECT `+quoteCols+` FROM quotes WHERE quote_id=?`, id))
}

// LatestActiveQuote 取会话最近一条未核销、未过期的报价（前端确认按钮用）。
func (s *Store) LatestActiveQuote(sessionID string) (*provider.Quote, error) {
	return scanQuote(s.db.QueryRow(`SELECT `+quoteCols+` FROM quotes
	  WHERE session_id=? AND consumed_at IS NULL
	  ORDER BY created_at DESC LIMIT 1`, sessionID))
}

// ConfirmQuote 确认报价并签发一次性令牌（前端确认按钮 → gateway 转发到此）。
func (s *Store) ConfirmQuote(id, sessionID, token string, tokenExp time.Time) error {
	res, err := s.db.Exec(`UPDATE quotes
	  SET confirmed_at=?, confirm_token=?, token_expires=?
	  WHERE quote_id=? AND session_id=? AND consumed_at IS NULL`,
		unix(time.Now()), token, unix(tokenExp), id, sessionID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ConsumeQuote 下单核销报价（幂等键生效点）。
func (s *Store) ConsumeQuote(id string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE quotes SET consumed_at=? WHERE quote_id=?`, unix(at), id)
	return err
}

// ---------- orders ----------

// InsertOrder 落单；quote_id UNIQUE 冲突 → ErrConflict（幂等）。
func (s *Store) InsertOrder(o *provider.Order) error {
	pu, _ := json.Marshal(o.Pickup)
	do, _ := json.Marshal(o.Dropoff)
	var drv any
	if o.Driver != nil {
		b, _ := json.Marshal(o.Driver)
		drv = string(b)
	}
	var acc any
	if o.AcceptedAt != nil {
		acc = unix(*o.AcceptedAt)
	}
	_, err := s.db.Exec(`INSERT INTO orders
	  (order_id, session_id, quote_id, pickup_json, dropoff_json, vehicle_class,
	   vehicle_name, price_cents, status, driver_json, cancel_fee, distance_m,
	   duration_s, accepted_at, created_at, updated_at)
	  VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.OrderID, o.SessionID, o.QuoteID, string(pu), string(do), o.VehicleClass,
		o.VehicleName, o.PriceCents, o.Status, drv, o.CancelFeeCents, o.DistanceM,
		o.DurationS, acc, unix(o.CreatedAt), unix(o.UpdatedAt))
	if err != nil && isUnique(err) {
		return ErrConflict
	}
	return err
}

func isUnique(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint")
}

// UpdateOrder 全量更新（状态机推进）。
func (s *Store) UpdateOrder(o *provider.Order) error {
	var drv any
	if o.Driver != nil {
		b, _ := json.Marshal(o.Driver)
		drv = string(b)
	}
	var acc any
	if o.AcceptedAt != nil {
		acc = unix(*o.AcceptedAt)
	}
	_, err := s.db.Exec(`UPDATE orders SET status=?, driver_json=?, cancel_fee=?,
	  accepted_at=?, updated_at=? WHERE order_id=?`,
		o.Status, drv, o.CancelFeeCents, acc, unix(o.UpdatedAt), o.OrderID)
	return err
}

// GetOrder 按 id 取订单。
func (s *Store) GetOrder(id string) (*provider.Order, error) {
	return scanOrder(s.db.QueryRow(`SELECT `+orderCols+` FROM orders WHERE order_id=?`, id))
}

const orderCols = `order_id, session_id, quote_id, pickup_json, dropoff_json,
  vehicle_class, vehicle_name, price_cents, status, driver_json, cancel_fee,
  distance_m, duration_s, accepted_at, created_at, updated_at`

func scanOrder(row interface{ Scan(...any) error }) (*provider.Order, error) {
	var (
		o          provider.Order
		pu, do     string
		drv        sql.NullString
		acc        sql.NullInt64
		created    int64
		updated    int64
	)
	err := row.Scan(&o.OrderID, &o.SessionID, &o.QuoteID, &pu, &do,
		&o.VehicleClass, &o.VehicleName, &o.PriceCents, &o.Status, &drv,
		&o.CancelFeeCents, &o.DistanceM, &o.DurationS, &acc, &created, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(pu), &o.Pickup)
	_ = json.Unmarshal([]byte(do), &o.Dropoff)
	if drv.Valid && drv.String != "" {
		var d provider.Driver
		if json.Unmarshal([]byte(drv.String), &d) == nil {
			o.Driver = &d
		}
	}
	o.AcceptedAt = nullTime(acc)
	o.CreatedAt = ts(created)
	o.UpdatedAt = ts(updated)
	return &o, nil
}

// ListOrders 会话订单列表（最近优先；status 空 = 全部）。
func (s *Store) ListOrders(sessionID, status string, limit int) ([]*provider.Order, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q := `SELECT ` + orderCols + ` FROM orders WHERE session_id=?`
	args := []any{sessionID}
	if status != "" {
		q += ` AND status=?`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*provider.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ---------- saved_places ----------

// PutSavedPlace 写常用地址（家/公司）。
func (s *Store) PutSavedPlace(sessionID, tag string, poi provider.POI) error {
	b, _ := json.Marshal(poi)
	_, err := s.db.Exec(`INSERT INTO saved_places (session_id, tag, poi_json, updated_at)
	  VALUES (?,?,?,?)
	  ON CONFLICT(session_id, tag) DO UPDATE SET poi_json=excluded.poi_json, updated_at=excluded.updated_at`,
		sessionID, tag, string(b), unix(time.Now()))
	return err
}

// ListSavedPlaces 读常用地址。
func (s *Store) ListSavedPlaces(sessionID string) (map[string]json.RawMessage, error) {
	rows, err := s.db.Query(`SELECT tag, poi_json FROM saved_places WHERE session_id=?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var tag, body string
		if err := rows.Scan(&tag, &body); err != nil {
			return nil, err
		}
		out[tag] = json.RawMessage(body)
	}
	return out, rows.Err()
}
