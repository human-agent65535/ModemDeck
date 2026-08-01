package main

import (
	"errors"
	"net/http"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
)

func TestAgentDiagnosticLogsUnsupportedRecognizesVersionSkew(t *testing.T) {
	for name, err := range map[string]error{
		"missing route": &agentclient.OperationError{Status: http.StatusNotFound, Code: "not_found"},
		"unsupported":   &agentclient.OperationError{Status: http.StatusNotImplemented, Code: "not_supported"},
	} {
		t.Run(name, func(t *testing.T) {
			if !agentDiagnosticLogsUnsupported(err) {
				t.Fatalf("error was not recognized: %v", err)
			}
		})
	}
	if agentDiagnosticLogsUnsupported(errors.New("socket unavailable")) {
		t.Fatal("transport error was treated as unsupported")
	}
}
