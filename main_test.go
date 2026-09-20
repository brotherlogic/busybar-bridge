package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

func TestWebsocketDependency(t *testing.T) {
	if websocket.StatusCode(0) != 0 {
		t.Fatal("unexpected status code")
	}
}

func TestProtobufDependency(t *testing.T) {
	var m proto.Message
	if proto.Size(m) != 0 {
		t.Fatal("unexpected proto size")
	}
}

func TestRun_HelpFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "double dash help", args: []string{"--help"}},
		{name: "single dash help", args: []string{"-help"}},
		{name: "short help", args: []string{"-h"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			ctx := context.Background()
			code := run(ctx, tc.args, &stderr)
			if code != 0 {
				t.Fatalf("expected exit code 0 for help flag, got %d. stderr: %s", code, stderr.String())
			}
		})
	}
}

func TestRun_ValidationFailure_MissingToken(t *testing.T) {
	t.Setenv("HASS_TOKEN", "")
	var stderr bytes.Buffer
	ctx := context.Background()
	code := run(ctx, []string{}, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing token, got %d", code)
	}
	if !strings.Contains(stderr.String(), "hass_token is required") {
		t.Fatalf("expected error message to contain 'hass_token is required', got: %s", stderr.String())
	}
}

func TestRun_ValidationFailure_InvalidPort(t *testing.T) {
	var stderr bytes.Buffer
	ctx := context.Background()
	args := []string{"--hass-token=valid-token", "--port=70000"}
	code := run(ctx, args, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid port, got %d", code)
	}
	if !strings.Contains(stderr.String(), "port must be between 1 and 65535") {
		t.Fatalf("expected error message to contain 'port must be between 1 and 65535', got: %s", stderr.String())
	}
}

func TestRun_ValidationFailure_InvalidHassURL(t *testing.T) {
	var stderr bytes.Buffer
	ctx := context.Background()
	args := []string{"--hass-token=valid-token", "--hass-url=ftp://invalid-url"}
	code := run(ctx, args, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid hass url, got %d", code)
	}
	if !strings.Contains(stderr.String(), "invalid hass_url") {
		t.Fatalf("expected error message to contain 'invalid hass_url', got: %s", stderr.String())
	}
}

func TestRun_InvalidFlag(t *testing.T) {
	var stderr bytes.Buffer
	ctx := context.Background()
	args := []string{"--unrecognized-cli-flag"}
	code := run(ctx, args, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for unrecognized flag, got %d", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected error output on stderr for unrecognized flag")
	}
}

func TestRun_GracefulShutdown(t *testing.T) {
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel context shortly after startup to trigger graceful shutdown
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	args := []string{
		"--hass-token=test-token-12345",
		"--port=19090",
		"--busybar-port=19091",
		"--shutdown-timeout=500ms",
	}

	code := run(ctx, args, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 on graceful shutdown, got %d. stderr: %s", code, stderr.String())
	}
}

func TestMain_CLIExecution_Help(t *testing.T) {
	if os.Getenv("BE_CRASHING_PROCESS") == "1" {
		os.Args = []string{"busybar-bridge", "--help"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMain_CLIExecution_Help")
	cmd.Env = append(os.Environ(), "BE_CRASHING_PROCESS=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit code 0 when running with --help, got error: %v, output: %s", err, string(output))
	}
}

func TestMain_CLIExecution_MissingTokenFailure(t *testing.T) {
	if os.Getenv("BE_CRASHING_PROCESS") == "1" {
		os.Args = []string{"busybar-bridge"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMain_CLIExecution_MissingTokenFailure")
	// Clear HASS_TOKEN
	cleanEnv := make([]string, 0, len(os.Environ()))
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "HASS_TOKEN=") {
			cleanEnv = append(cleanEnv, env)
		}
	}
	cmd.Env = append(cleanEnv, "BE_CRASHING_PROCESS=1", "HASS_TOKEN=")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit code on missing token, got 0. output: %s", string(output))
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got %d", exitErr.ExitCode())
	}
}

func TestRun_ValidationFailure_InvalidGoogleRedirectURL(t *testing.T) {
	var stderr bytes.Buffer
	ctx := context.Background()
	args := []string{
		"--hass-token=valid-token",
		"--google-redirect-url=ftp://invalid-redirect-url",
	}
	code := run(ctx, args, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid google redirect url, got %d", code)
	}
	if !strings.Contains(stderr.String(), "invalid google_redirect_url") {
		t.Fatalf("expected error message to contain 'invalid google_redirect_url', got: %s", stderr.String())
	}
}

func TestRun_InitializationFailure_CorruptedCalendarStore(t *testing.T) {
	tempDir := t.TempDir()
	corruptPath := filepath.Join(tempDir, "corrupted.pb")
	if err := os.WriteFile(corruptPath, []byte("bad-proto"), 0600); err != nil {
		t.Fatalf("failed to write corrupt test file: %v", err)
	}

	var stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	args := []string{
		"--hass-token=valid-token",
		"--calendar-store-path=" + corruptPath,
	}
	code := run(ctx, args, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for corrupt calendar store, got %d", code)
	}
	if !strings.Contains(stderr.String(), "initialization error") || !strings.Contains(stderr.String(), "calendar store") {
		t.Fatalf("expected stderr to contain 'initialization error' and 'calendar store', got: %s", stderr.String())
	}
}

func TestRun_WithGoogleOAuthAndCalendarFlags(t *testing.T) {
	tempDir := t.TempDir()
	calPath := filepath.Join(tempDir, "calendar.pb")

	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	args := []string{
		"--hass-token=test-token-12345",
		"--port=19095",
		"--busybar-port=19096",
		"--shutdown-timeout=500ms",
		"--google-client-id=my-client-id",
		"--google-client-secret=my-client-secret",
		"--google-redirect-url=http://localhost:19095/oauth/google/callback",
		"--calendar-store-path=" + calPath,
	}

	code := run(ctx, args, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 on graceful shutdown with OAuth flags, got %d. stderr: %s", code, stderr.String())
	}
}

