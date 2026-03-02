package maintenance

import (
	"encoding/json"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/robfig/cron/v3"

	"go.uber.org/zap"
)

type Scheduler struct {
	cron *cron.Cron
	nc   *nats.Conn
	log  *zap.SugaredLogger
	mu   sync.Mutex
}

func NewScheduler(nc *nats.Conn, log *zap.SugaredLogger) *Scheduler {
	return &Scheduler{
		cron: cron.New(),
		nc:   nc,
		log:  log,
	}
}

func (s *Scheduler) AddTask(schedule, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.cron.AddFunc(schedule, func() {
		msg, _ := json.Marshal(map[string]string{
			"task_id": taskID,
			"action":  "run",
		})
		if err := s.nc.Publish("hive.maintenance", msg); err != nil {
			s.log.Errorf("maintenance scheduler: failed to publish task %s: %v", taskID, err)
		}
	})
	return err
}

func (s *Scheduler) Start() {
	s.cron.Start()
	s.log.Info("maintenance scheduler started")
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
	s.log.Info("maintenance scheduler stopped")
}
