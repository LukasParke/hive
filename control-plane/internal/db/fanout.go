package db

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Notification struct {
	Channel string `json:"channel"`
	Payload string `json:"payload"`
}

type Fanout struct {
	pool *pgxpool.Pool
	mu   sync.RWMutex
	subs map[string][]chan Notification
}

func NewFanout(pool *pgxpool.Pool) *Fanout {
	return &Fanout{pool: pool, subs: map[string][]chan Notification{}}
}

func (f *Fanout) Subscribe(channel string, size int) <-chan Notification {
	ch := make(chan Notification, size)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs[channel] = append(f.subs[channel], ch)
	return ch
}

func (f *Fanout) Emit(ctx context.Context, channel, payload string) error {
	_, err := f.pool.Exec(ctx, "select pg_notify($1, $2)", channel, payload)
	return err
}

func (f *Fanout) Run(ctx context.Context, channels []string) error {
	conn, err := f.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	for _, channel := range channels {
		if _, err := conn.Exec(ctx, "listen "+pgx.Identifier{channel}.Sanitize()); err != nil {
			return err
		}
	}

	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		f.broadcast(Notification{Channel: n.Channel, Payload: n.Payload})
	}
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
