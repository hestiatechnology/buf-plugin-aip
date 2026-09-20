package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoFix(t *testing.T) {
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
  uint32 total_pages = 2;
}

enum BookStatus {
  unspecified = 0;
  active = 1;
}
`
	protoFile := filepath.Join(tempDir, "test.proto")
	if err := os.WriteFile(protoFile, []byte(protoContent), 0644); err != nil {
		t.Fatalf("failed to write proto file: %v", err)
	}

	results, err := FixFiles(context.Background(), []string{protoFile}, FixOptions{
		DryRun: false,
	})
	if err != nil {
		t.Fatalf("FixFiles failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected fixes, got none")
	}

	fixedContentBytes, err := os.ReadFile(protoFile)
	if err != nil {
		t.Fatalf("failed to read fixed file: %v", err)
	}
	fixedContent := string(fixedContentBytes)

	// Verify BadBookRequest was fixed to GetBookRequest in rpc GetBook
	if !strings.Contains(fixedContent, "rpc GetBook(GetBookRequest) returns (Book);") {
		t.Errorf("expected rpc GetBook(GetBookRequest), got:\n%s", fixedContent)
	}

	// Verify uint32 was fixed to int32 (AIP-141)
	if !strings.Contains(fixedContent, "int32 total_pages = 2;") {
		t.Errorf("expected int32 total_pages, got:\n%s", fixedContent)
	}

	// Verify enum name fixed to BookState (AIP-216) and values to upper snake (AIP-126)
	if !strings.Contains(fixedContent, "enum BookState") {
		t.Errorf("expected enum BookState, got:\n%s", fixedContent)
	}
	if !strings.Contains(fixedContent, "UNSPECIFIED = 0;") {
		t.Errorf("expected UNSPECIFIED = 0, got:\n%s", fixedContent)
	}
	if !strings.Contains(fixedContent, "ACTIVE = 1;") {
		t.Errorf("expected ACTIVE = 1, got:\n%s", fixedContent)
	}
}

func TestAutoFixFilter(t *testing.T) {
	tempDir := t.TempDir()
	protoContent := `syntax = "proto3";

package test.v1;

message Book {
  uint32 total_pages = 1;
  string author_name = 2;
}
`
	protoFile := filepath.Join(tempDir, "test.proto")
	if err := os.WriteFile(protoFile, []byte(protoContent), 0644); err != nil {
		t.Fatalf("failed to write proto file: %v", err)
	}

	// Fix only AIP-141 (forbidden types: uint32 -> int32), ignoring AIP-122 (name suffix)
	results, err := FixFiles(context.Background(), []string{protoFile}, FixOptions{
		Rules: []string{"AIP_0141_FORBIDDEN_TYPES"},
	})
	if err != nil {
		t.Fatalf("FixFiles failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected fixes, got none")
	}

	contentBytes, err := os.ReadFile(protoFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	content := string(contentBytes)

	// total_pages should be fixed to int32
	if !strings.Contains(content, "int32 total_pages = 1;") {
		t.Errorf("expected int32 total_pages, got:\n%s", content)
	}
	// author_name should NOT be modified because AIP_0122 was filtered out
	if !strings.Contains(content, "string author_name = 2;") {
		t.Errorf("expected author_name to remain unchanged, got:\n%s", content)
	}
}

func TestAutoFixDryRun(t *testing.T) {
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

	results, err := FixFiles(context.Background(), []string{protoFile}, FixOptions{
		DryRun: true,
		Diff:   true,
	})
	if err != nil {
		t.Fatalf("FixFiles failed: %v", err)
	}
	if len(results) == 0 || results[0].Fixed == 0 {
		t.Fatalf("expected fixes in dry-run result")
	}
	if !strings.Contains(results[0].Diff, "-  uint32 total_pages = 1;") ||
		!strings.Contains(results[0].Diff, "+  int32 total_pages = 1;") {
		t.Errorf("unexpected diff: %s", results[0].Diff)
	}

	// Verify file on disk was NOT modified
	diskContent, _ := os.ReadFile(protoFile)
	if string(diskContent) != protoContent {
		t.Errorf("file on disk was modified in dry-run mode")
	}
}
