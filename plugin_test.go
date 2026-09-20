package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"buf.build/go/bufplugin/check/checktest"
)

func TestSpec(t *testing.T) {
	spec, err := NewSpec()
	if err != nil {
		t.Fatalf("failed to create spec: %v", err)
	}

	checktest.SpecTest(t, spec)
}

func TestCheckRuleViolation(t *testing.T) {
	spec, err := NewSpec()
	if err != nil {
		t.Fatalf("failed to create spec: %v", err)
	}

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

	reqSpec := &checktest.RequestSpec{
		Files: &checktest.ProtoFileSpec{
			DirPaths:  []string{tempDir},
			FilePaths: []string{"test.proto"},
		},
		RuleIDs: []string{"AIP_0131_REQUEST_MESSAGE_NAME"},
	}

	req, err := reqSpec.ToRequest(context.Background())
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}

	checkTest := checktest.CheckTest{
		Request: reqSpec,
		Spec:    spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			{
				RuleID:  "AIP_0131_REQUEST_MESSAGE_NAME",
				Message: `Request message should be named after the RPC, i.e. "GetBookRequest". (https://linter.aip.dev/131/request-message-name)`,
				FileLocation: &checktest.ExpectedFileLocation{
					FileName:    "test.proto",
					StartLine:   5,
					StartColumn: 14,
					EndLine:     5,
					EndColumn:   28,
				},
			},
		},
	}
	checkTest.Run(t)
	_ = req
}

func TestCheckRuleWithConfig(t *testing.T) {
	spec, err := NewSpec()
	if err != nil {
		t.Fatalf("failed to create spec: %v", err)
	}

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

	configFile := filepath.Join(tempDir, "api-linter.yaml")
	configContent := `
- disabled_rules:
    - core::0131::request-message-name
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	reqSpec := &checktest.RequestSpec{
		Files: &checktest.ProtoFileSpec{
			DirPaths:  []string{tempDir},
			FilePaths: []string{"test.proto"},
		},
		RuleIDs: []string{"AIP_0131_REQUEST_MESSAGE_NAME"},
		Options: map[string]any{
			"config": configFile,
		},
	}

	checkTest := checktest.CheckTest{
		Request:             reqSpec,
		Spec:                spec,
		ExpectedAnnotations: nil, // Should be empty because the rule was disabled in config
	}
	checkTest.Run(t)
}

func TestCheckWithAutoFixOption(t *testing.T) {
	spec, err := NewSpec()
	if err != nil {
		t.Fatalf("failed to create spec: %v", err)
	}

	tempDir := t.TempDir()
	protoContent := `syntax = "proto3";

package test.v1;

message Book {
  uint32 total_pages = 1;
}
`
	protoFile := filepath.Join(tempDir, "test.proto")
	if err := os.WriteFile(protoFile, []byte(protoContent), 0644); err != nil {
		t.Fatalf("failed to write proto file: %v", err)
	}

	reqSpec := &checktest.RequestSpec{
		Files: &checktest.ProtoFileSpec{
			DirPaths:  []string{tempDir},
			FilePaths: []string{"test.proto"},
		},
		RuleIDs: []string{"AIP_0141_FORBIDDEN_TYPES"},
		Options: map[string]any{
			"auto_fix": true,
			"base_dir": tempDir,
		},
	}

	checkTest := checktest.CheckTest{
		Request:             reqSpec,
		Spec:                spec,
		ExpectedAnnotations: nil, // Should be suppressed because auto_fix fixed it on disk
	}
	checkTest.Run(t)

	// Verify file on disk was modified to int32
	diskContent, err := os.ReadFile(protoFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if !strings.Contains(string(diskContent), "int32 total_pages = 1;") {
		t.Errorf("expected uint32 to be auto-fixed to int32 on disk, got:\n%s", string(diskContent))
	}
}

func TestConvenienceOptions(t *testing.T) {
	spec, err := NewSpec()
	if err != nil {
		t.Fatalf("failed to create spec: %v", err)
	}

	tempDir := t.TempDir()
	protoContent := `syntax = "proto3";

package test.v1;

message Book {
  string name = 1;
}
`
	protoFile := filepath.Join(tempDir, "test.proto")
	if err := os.WriteFile(protoFile, []byte(protoContent), 0644); err != nil {
		t.Fatalf("failed to write proto file: %v", err)
	}

	// Test with ignore_comments and skip_language_options:
	// Missing comment on Book (AIP-192) and missing Java options (AIP-191) should both be suppressed!
	reqSpec := &checktest.RequestSpec{
		Files: &checktest.ProtoFileSpec{
			DirPaths:  []string{tempDir},
			FilePaths: []string{"test.proto"},
		},
		RuleIDs: []string{
			"AIP_0192_HAS_COMMENTS",
			"AIP_0191_JAVA_PACKAGE",
		},
		Options: map[string]any{
			"ignore_comments":       true,
			"skip_language_options": true,
		},
	}

	checkTest := checktest.CheckTest{
		Request:             reqSpec,
		Spec:                spec,
		ExpectedAnnotations: nil, // Both rules should be suppressed
	}
	checkTest.Run(t)
}
