package monitor

import (
	"sync"
	"time"
)

var ServiceHealthCache = &serviceHealthCache{}

type serviceHealthCache struct {
	mu      sync.RWMutex
	latest  []ServiceHealth
	updated time.Time
}

func (shc *serviceHealthCache) Update(entries []ServiceHealth) {
	shc.mu.Lock()
	defer shc.mu.Unlock()
	shc.latest = entries
	shc.updated = time.Now()
}

func (shc *serviceHealthCache) GetAll() ([]ServiceHealth, time.Time) {
	shc.mu.RLock()
	defer shc.mu.RUnlock()
	if shc.latest == nil {
		return []ServiceHealth{}, shc.updated
	}
	out := make([]ServiceHealth, len(shc.latest))
	copy(out, shc.latest)
	return out, shc.updated
}
