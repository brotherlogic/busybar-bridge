package main

import (
	"os"
	"strings"
	"testing"
)

func TestDockerBuildWorkflow_FileAndStructure(t *testing.T) {
	path := ".github/workflows/docker-build.yml"
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read workflow file %s: %v", path, err)
	}

	contentStr := string(content)

	// Ensure no tab characters are present in YAML
	if strings.Contains(contentStr, "\t") {
		t.Errorf("Workflow file %s contains tab characters, YAML requires spaces", path)
	}

	requiredSnippets := []string{
		"push:",
		"pull_request:",
		"branches:",
		"main",
		"go test -v -race ./...",
		"docker/setup-buildx-action",
		"docker/build-push-action",
		"ghcr.io",
		"GITHUB_TOKEN",
		"ghcr.io/brotherlogic/busybar-bridge",
		"latest",
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(contentStr, snippet) {
			t.Errorf("Workflow file %s is missing required directive or snippet: %q", path, snippet)
		}
	}
}

func TestDockerBuildWorkflow_SemanticElements(t *testing.T) {
	path := ".github/workflows/docker-build.yml"
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read workflow file %s: %v", path, err)
	}

	contentStr := string(content)

	// Verify authentication condition: push to main
	if !strings.Contains(contentStr, "github.event_name == 'push'") {
		t.Errorf("Workflow missing push conditional check for GHCR authentication/push")
	}

	// Verify commit SHA tagging
	if !strings.Contains(contentStr, "github.sha") {
		t.Errorf("Workflow missing commit SHA tag for container image")
	}
}
