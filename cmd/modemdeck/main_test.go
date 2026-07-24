package main

import (
	"testing"
	"time"
)

func TestHostAgentRequestTimeoutExceedsSingleReadBudget(t *testing.T) {
	const (
		agentSingleReadBudget = 10 * time.Second
		maximumBoundedTimeout = 30 * time.Second
	)
	if hostAgentRequestTimeout <= agentSingleReadBudget {
		t.Fatalf(
			"hostAgentRequestTimeout = %s, must exceed %s",
			hostAgentRequestTimeout,
			agentSingleReadBudget,
		)
	}
	if hostAgentRequestTimeout > maximumBoundedTimeout {
		t.Fatalf(
			"hostAgentRequestTimeout = %s, exceeds bounded maximum %s",
			hostAgentRequestTimeout,
			maximumBoundedTimeout,
		)
	}
}
