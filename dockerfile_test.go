package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDockerfile_StructureAndInstructions(t *testing.T) {
	content, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("Failed to read Dockerfile: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	requiredDirectives := []string{
		"FROM golang:1.27-alpine AS builder",
		"WORKDIR /app",
		"COPY go.mod go.sum ./",
		"RUN go mod download",
		"COPY . .",
		`RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /busybar-bridge .`,
		"FROM gcr.io/distroless/static:nonroot",
		"WORKDIR /",
		"COPY --from=builder /busybar-bridge /busybar-bridge",
		"USER nonroot:nonroot",
		"EXPOSE 8080",
		`ENTRYPOINT ["/busybar-bridge"]`,
	}

	contentStr := string(content)
	for _, directive := range requiredDirectives {
		if !strings.Contains(contentStr, directive) {
			t.Errorf("Dockerfile missing required directive: %q", directive)
		}
	}

	// Verify multi-stage structure (builder stage and distroless runtime stage)
	var hasBuilderStage, hasDistrolessStage bool
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "FROM golang:") && strings.Contains(trimmed, "AS builder") {
			hasBuilderStage = true
		}
		if strings.HasPrefix(trimmed, "FROM gcr.io/distroless/static:nonroot") {
			hasDistrolessStage = true
		}
	}

	if !hasBuilderStage {
		t.Errorf("Dockerfile missing builder stage using golang image")
	}
	if !hasDistrolessStage {
		t.Errorf("Dockerfile missing distroless runtime stage")
	}
}

func TestDockerfile_StaticBinaryCompilation(t *testing.T) {
	tmpBinary, err := os.CreateTemp("", "busybar-bridge-bin-*")
	if err != nil {
		t.Fatalf("Failed to create temp binary: %v", err)
	}
	tmpBinaryPath := tmpBinary.Name()
	_ = tmpBinary.Close()
	defer os.Remove(tmpBinaryPath)

	cmd := exec.Command("go", "build", "-ldflags=-w -s", "-o", tmpBinaryPath, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Static binary build failed: %v\nOutput: %s", err, string(out))
	}

	fi, err := os.Stat(tmpBinaryPath)
	if err != nil {
		t.Fatalf("Failed to stat compiled binary: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatalf("Compiled binary is empty")
	}
}
