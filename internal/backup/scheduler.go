package backup

import (
	"github.com/nats-io/nats.go"
	"github.com/robfig/cron/v3"

	"go.uber.org/zap"
)

type Scheduler struct {
	cron *cron.Cron
	nc   *nats.Conn
	log  *zap.SugaredLogger
}

func NewScheduler(nc *nats.Conn, log *zap.SugaredLogger) *Scheduler {
	return &Scheduler{
		cron: cron.New(),
		nc:   nc,
		log:  log,
	}
}

func (s *Scheduler) AddJob(schedule, configID string) error {
	_, err := s.cron.AddFunc(schedule, func() {
		s.log.Infof("triggering backup for config %s", configID)
		if err := s.nc.Publish("hive.backup", []byte(`{"config_id":"`+configID+`"}`)); err != nil {
			s.log.Errorf("publish backup trigger: %v", err)
		}
	})
	return err
}

func (s *Scheduler) Start() {
	s.cron.Start()
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
}
