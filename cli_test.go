package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetAllRules(t *testing.T) {
	rules, err := GetAllRules()
	if err != nil {
		t.Fatalf("GetAllRules failed: %v", err)
	}

	if len(rules) < 300 {
		t.Errorf("expected at least 300 rules, got %d", len(rules))
	}

	foundHttpBody := false
	for _, r := range rules {
		if !strings.HasPrefix(r.ID, "AIP_") {
			t.Errorf("expected rule ID to start with AIP_, got %s", r.ID)
		}
		if r.DocURL == "" || !strings.HasPrefix(r.DocURL, "https://linter.aip.dev/") {
			t.Errorf("expected DocURL for %s, got %s", r.ID, r.DocURL)
		}
		if r.SpecURL == "" || !strings.HasPrefix(r.SpecURL, "https://aip.dev/") {
			t.Errorf("expected SpecURL for %s, got %s", r.ID, r.SpecURL)
		}
		if r.ID == "AIP_0131_HTTP_BODY" {
			foundHttpBody = true
		}
	}

	if !foundHttpBody {
		t.Errorf("expected to find AIP_0131_HTTP_BODY")
	}
}

func TestExplainAndList(t *testing.T) {
	// Verify explain does not panic on valid or invalid queries
	runExplain([]string{"AIP_0131_HTTP_BODY"})
	runExplain([]string{"131"})
	runExplain([]string{"unknown_rule_xyz"})

	// Verify list-rules with various filters
	runListRules([]string{"--category", "AIP_0142"})
	runListRules([]string{"--fixable"})
	runListRules([]string{"--json"})
}

func TestCheckStandalone(t *testing.T) {
	tempDir := t.TempDir()
	protoContent := `syntax = "proto3";

package test.v1;

service LibraryService {
  rpc GetBook(BadBookRequest) returns (Book);
}

message BadBookRequest {
  string name = 1;
}

message Book {
  string name = 1;
}
`
	protoFile := filepath.Join(tempDir, "test.proto")
	if err := os.WriteFile(protoFile, []byte(protoContent), 0644); err != nil {
		t.Fatalf("failed to write proto file: %v", err)
	}

	// Test discovery and compile
	files, dirs, err := discoverProtoFiles([]string{protoFile}, nil)
	if err != nil {
		t.Fatalf("discoverProtoFiles failed: %v", err)
	}
	if len(files) != 1 || len(dirs) == 0 {
		t.Errorf("expected 1 file, got %d files, %d dirs", len(files), len(dirs))
	}
}
