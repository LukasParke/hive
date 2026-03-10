package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/lholliger/hive/internal/store"
)

type TaskFunc func(ctx context.Context) error

type taskDef struct {
	ID          string
	Name        string
	Description string
	Category    string
	Interval    time.Duration
	InitDelay   time.Duration
	Fn          TaskFunc
}

type SystemTaskManager struct {
	db    *store.Store
	log   *zap.SugaredLogger
	tasks []taskDef

	mu       sync.Mutex
	triggers map[string]chan struct{}
}

func NewSystemTaskManager(db *store.Store, log *zap.SugaredLogger) *SystemTaskManager {
	return &SystemTaskManager{
		db:       db,
		log:      log,
		triggers: make(map[string]chan struct{}),
	}
}

func (m *SystemTaskManager) Register(id, name, description, category string, interval, initDelay time.Duration, fn TaskFunc) {
	m.tasks = append(m.tasks, taskDef{
		ID:          id,
		Name:        name,
		Description: description,
		Category:    category,
		Interval:    interval,
		InitDelay:   initDelay,
		Fn:          fn,
	})
}

func (m *SystemTaskManager) Start(ctx context.Context) {
	m.seedDB(ctx)

	for _, td := range m.tasks {
		ch := make(chan struct{}, 1)
		m.mu.Lock()
		m.triggers[td.ID] = ch
		m.mu.Unlock()

		go m.runLoop(ctx, td, ch)
	}
}

func (m *SystemTaskManager) Trigger(id string) error {
	m.mu.Lock()
	ch, ok := m.triggers[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown system task: %s", id)
	}
	select {
	case ch <- struct{}{}:
	default:
		// already triggered, skip
	}
	return nil
}

func (m *SystemTaskManager) seedDB(ctx context.Context) {
	if m.db == nil {
		return
	}
	for _, td := range m.tasks {
		t := &store.SystemTask{
			ID:              td.ID,
			Name:            td.Name,
			Description:     td.Description,
			Category:        td.Category,
			IntervalSeconds: int(td.Interval.Seconds()),
			Enabled:         true,
		}
		if err := m.db.UpsertSystemTask(ctx, t); err != nil {
			m.log.Warnf("system tasks: seed %s: %v", td.ID, err)
		}
	}
}

func (m *SystemTaskManager) isEnabled(ctx context.Context, id string) bool {
	if m.db == nil {
		return true
	}
	t, err := m.db.GetSystemTask(ctx, id)
	if err != nil || t == nil {
		return true
	}
	return t.Enabled
}

func (m *SystemTaskManager) runLoop(ctx context.Context, td taskDef, trigger <-chan struct{}) {
	m.log.Infof("system task [%s] started (%s interval)", td.ID, td.Interval)

	if td.InitDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(td.InitDelay):
		case <-trigger:
		}
	}

	m.exec(ctx, td)

	ticker := time.NewTicker(td.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.isEnabled(ctx, td.ID) {
				m.exec(ctx, td)
			}
		case <-trigger:
			m.exec(ctx, td)
		}
	}
}

func (m *SystemTaskManager) exec(ctx context.Context, td taskDef) {
	start := time.Now()
	err := td.Fn(ctx)
	durationMs := int(time.Since(start).Milliseconds())

	if m.db == nil {
		return
	}

	status := "success"
	errMsg := ""
	if err != nil {
		status = "error"
		errMsg = err.Error()
		m.log.Warnf("system task [%s] error (%dms): %v", td.ID, durationMs, err)
	}

	if dbErr := m.db.RecordSystemTaskRun(ctx, td.ID, durationMs, status, errMsg); dbErr != nil {
		m.log.Warnf("system task [%s] record run: %v", td.ID, dbErr)
	}
}
