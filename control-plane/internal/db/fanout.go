package db

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Notification is a message received on a LISTEN channel.
type Notification struct {
	Channel string `json:"channel"`
	Payload string `json:"payload"`
}

// pooledFanoutConn adapts a *pgxpool.Conn to fanoutConn.
type pooledFanoutConn struct {
	conn *pgxpool.Conn
}

// Listen issues LISTEN for the channel on the pooled connection.
func (p *pooledFanoutConn) Listen(ctx context.Context, channel string) error {
	_, err := p.conn.Exec(ctx, "listen "+pgx.Identifier{channel}.Sanitize())
	return err
}

// WaitForNotification blocks until the next notification arrives.
func (p *pooledFanoutConn) WaitForNotification(ctx context.Context) (*pgconn.Notification, error) {
	return p.conn.Conn().WaitForNotification(ctx)
}

// Release returns the connection to the pool.
func (p *pooledFanoutConn) Release() { p.conn.Release() }

// fanoutConn is the slice of pgxpool.Conn the fanout needs. It exists so
// tests can inject a fake connection and exercise the reconnect loop
// without a database.
type fanoutConn interface {
	Listen(ctx context.Context, channel string) error
	WaitForNotification(ctx context.Context) (*pgconn.Notification, error)
	Release()
}

// directFanoutConn wraps a raw *pgx.Conn dialed directly from a DSN. Used
// when the pool's DSN points at PgBouncer: transaction pooling does not
// support session-level LISTEN, so the fanout must bypass it.
type directFanoutConn struct {
	conn *pgx.Conn
}

// Listen issues LISTEN for the channel on the direct connection.
func (d *directFanoutConn) Listen(ctx context.Context, channel string) error {
	_, err := d.conn.Exec(ctx, "listen "+pgx.Identifier{channel}.Sanitize())
	return err
}

// WaitForNotification blocks until the next notification arrives.
func (d *directFanoutConn) WaitForNotification(ctx context.Context) (*pgconn.Notification, error) {
	return d.conn.WaitForNotification(ctx)
}

// Release closes the direct connection.
func (d *directFanoutConn) Release() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.conn.Close(ctx)
}

// fanoutConnector acquires dedicated connections for the LISTEN lifetime.
type fanoutConnector interface {
	acquire(ctx context.Context) (fanoutConn, error)
}

type poolConnector struct {
	pool *pgxpool.Pool
}

func (p poolConnector) acquire(ctx context.Context) (fanoutConn, error) {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return &pooledFanoutConn{conn: conn}, nil
}

// dsnConnector dials Postgres directly for every (re)connect.
type dsnConnector struct {
	dsn string
}

func (c dsnConnector) acquire(ctx context.Context) (fanoutConn, error) {
	conn, err := pgx.Connect(ctx, c.dsn)
	if err != nil {
		return nil, fmt.Errorf("fanout listen connect: %w", err)
	}
	return &directFanoutConn{conn: conn}, nil
}

// Fanout broadcasts Postgres LISTEN notifications to in-process subscribers.
type Fanout struct {
	pool      *pgxpool.Pool
	mu        sync.RWMutex
	subs      map[string][]chan Notification
	connector fanoutConnector

	// backoff knobs, overridable in tests.
	backoffBase time.Duration
	backoffMax  time.Duration
	jitter      func() float64
}

// NewFanout returns a Fanout that LISTENs on a connection from the pool.
func NewFanout(pool *pgxpool.Pool) *Fanout {
	return newFanout(pool, poolConnector{pool: pool})
}

// NewFanoutWithListenURL uses a dedicated DSN for the LISTEN connection when
// listenURL is non-empty (required when DatabaseURL points at PgBouncer in
// transaction mode, which cannot carry session-level LISTEN). Emit still
// goes through the pool; only the LISTEN lifetime bypasses PgBouncer.
func NewFanoutWithListenURL(pool *pgxpool.Pool, listenURL string) *Fanout {
	if listenURL == "" {
		return NewFanout(pool)
	}
	return newFanout(pool, dsnConnector{dsn: listenURL})
}

func newFanout(pool *pgxpool.Pool, c fanoutConnector) *Fanout {
	return &Fanout{
		pool:        pool,
		connector:   c,
		subs:        map[string][]chan Notification{},
		backoffBase: 1 * time.Second,
		backoffMax:  30 * time.Second,
		jitter:      rand.Float64, //nolint:gosec // non-cryptographic reconnect jitter
	}
}

// Subscribe returns a buffered channel receiving notifications for channel.
func (f *Fanout) Subscribe(channel string, size int) <-chan Notification {
	ch := make(chan Notification, size)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs[channel] = append(f.subs[channel], ch)
	return ch
}

// Emit publishes payload on the channel via pg_notify.
func (f *Fanout) Emit(ctx context.Context, channel, payload string) error {
	_, err := f.pool.Exec(ctx, "select pg_notify($1, $2)", channel, payload)
	return err
}

// Run listens on the given channels forever, broadcasting every notification
// to subscribers. It holds one dedicated connection acquired via pool.Acquire
// for the whole LISTEN lifetime. Any error (acquire, LISTEN, or the receive
// loop) releases the connection and retries after exponential backoff
// (1s doubling up to a 30s cap, plus jitter), re-issuing LISTEN for all
// channels. Subscriber channels persist across reconnects. Returns nil when
// ctx is cancelled.
func (f *Fanout) Run(ctx context.Context, channels []string) error {
	for attempt := 0; ; attempt++ {
		_ = f.listenOnce(ctx, channels)
		if ctx.Err() != nil {
			return nil
		}
		wait := f.backoff(attempt)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// listenOnce performs a single acquire → LISTEN → receive cycle and returns
// the error that ended it (or nil if ctx was cancelled mid-wait).
func (f *Fanout) listenOnce(ctx context.Context, channels []string) error {
	conn, err := f.connector.acquire(ctx)
	if err != nil {
		return fmt.Errorf("fanout acquire conn: %w", err)
	}
	defer conn.Release()

	for _, ch := range channels {
		if err := conn.Listen(ctx, ch); err != nil {
			return fmt.Errorf("fanout listen %q: %w", ch, err)
		}
	}

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("fanout wait notification: %w", err)
		}
		f.broadcast(Notification{Channel: n.Channel, Payload: n.Payload})
	}
}

func (f *Fanout) backoff(attempt int) time.Duration {
	base := f.backoffBase
	max := f.backoffMax
	d := base << min(attempt, 62)
	if d > max || d <= 0 {
		d = max
	}
	// Full jitter within [d/2, d] keeps reconnect storms from syncing.
	wait := d/2 + time.Duration(f.jitter()*float64(d/2))
	if wait > max {
		wait = max
	}
	return wait
}

func (f *Fanout) broadcast(n Notification) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, ch := range f.subs[n.Channel] {
		select {
		case ch <- n:
		default:
		}
	}
}
