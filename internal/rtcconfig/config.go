package rtcconfig

import (
	"context"
	"time"
)

type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type Configuration struct {
	ICEServers []ICEServer
	ExpiresAt  time.Time
	RelayOnly  bool
}

type Provider interface {
	Generate(context.Context) (Configuration, error)
}
