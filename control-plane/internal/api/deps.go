package api

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/agentclient"
	"github.com/luke/hive/control-plane/internal/auth"
	"github.com/luke/hive/control-plane/internal/ca"
	"github.com/luke/hive/control-plane/internal/realtime"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

// Deps holds the shared dependencies for all API handler packages.
type Deps struct {
	Pool           *pgxpool.Pool
	Swarm          *swarmclient.Client
	Authority      *ca.Authority
	Auth           *auth.Service
	Hub            *realtime.Hub
	AgentDialer    *agentclient.Dialer
	BootstrapToken string
}
