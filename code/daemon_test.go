package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBoundedLogKeepsRecentOutput(t *testing.T) {
	var logs boundedLog
	input := strings.Repeat("x", maxDaemonLogBytes+128)
	if _, err := logs.Write([]byte(input)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	got := logs.String()
	if len(got) != maxDaemonLogBytes {
		t.Fatalf("log length = %d, want %d", len(got), maxDaemonLogBytes)
	}
	if got != input[len(input)-maxDaemonLogBytes:] {
		t.Error("bounded log did not retain the newest output")
	}
}

func TestStatusIncludesDaemonDiagnostics(t *testing.T) {
	original := gDaemon
	d := &Daemon{lastError: "headscale failed"}
	_, _ = d.output.Write([]byte("startup line\nheadscale failed\n"))
	gDaemon = d
	t.Cleanup(func() { gDaemon = original })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/status", nil)
	handleStatus(recorder, request)

	var status StatusResponse
	if err := json.NewDecoder(recorder.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.LastError != "headscale failed" {
		t.Errorf("LastError = %q", status.LastError)
	}
	if !strings.Contains(status.RecentLogs, "startup line") {
		t.Errorf("RecentLogs = %q", status.RecentLogs)
	}
}
