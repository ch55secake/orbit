package server

import (
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
)

type Status struct {
	Server  config.Server
	Metrics *client.MetricsResponse
	Error   error
}
