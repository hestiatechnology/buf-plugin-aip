package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/reporter"
	"github.com/bufbuild/protocompile/wellknownimports"
	"github.com/googleapis/api-linter/v2/lint"
	"github.com/googleapis/api-linter/v2/rules"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// CheckProblem represents a single lint violation formatted for output.
type CheckProblem struct {
	FilePath   string `json:"file_path"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	RuleID     string `json:"rule_id"`
	Message    string `json:"message"`
	DocURL     string `json:"doc_url"`
	Suggestion string `json:"suggestion,omitempty"`
	Fixable    bool   `json:"fixable"`
}

func runCheck(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	var importPaths stringListFlag
	fs.Var(&importPaths, "I", "Import directories to search for dependencies")
	var ruleFilter stringListFlag
	fs.Var(&ruleFilter, "rule", "Check only specific rule(s), e.g. AIP_0142_TIME_FIELD_NAMES")
	var categoryFilter stringListFlag
	fs.Var(&categoryFilter, "category", "Check only rules in category, e.g. AIP_CORE, AIP_0142")
	var exceptFilter stringListFlag
	fs.Var(&exceptFilter, "except", "Exclude specific rule(s) or category, e.g. AIP_0192_HAS_COMMENTS")
	fixableOnly := fs.Bool("fixable-only", false, "Only display violations that can be auto-fixed")
	asJSON := fs.Bool("json", false, "Output results as JSON")
	format := fs.String("format", "text", "Output format: text, json, github-actions")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: buf-plugin-aip check [flags] [paths...]\n\n")
		fmt.Fprintf(os.Stderr, "Lints protobuf files against AIP rules with standalone reporting.\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	targets := fs.Args()
	if len(targets) == 0 {
		targets = []string{"."}
	}

	protoFiles, importDirs, err := discoverProtoFiles(targets, importPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error discovering files: %v\n", err)
		os.Exit(1)
	}

	if len(protoFiles) == 0 {
		fmt.Println("No .proto files found to check.")
		return
	}

	compiler := protocompile.Compiler{
		Resolver: wellknownimports.WithStandardImports(
			&embeddedResolver{
				fallback: &protocompile.SourceResolver{
					ImportPaths: importDirs,
				},
			},
		),
		Reporter: reporter.NewReporter(
			func(err reporter.ErrorWithPos) error { return err },
			func(reporter.ErrorWithPos) {},
		),
		SourceInfoMode: protocompile.SourceInfoExtraOptionLocations,
	}

	relativeFiles := make([]string, len(protoFiles))
	fileToAbs := make(map[string]string)
	for i, pf := range protoFiles {
		rel := pf
		for _, dir := range importDirs {
			if r, err := filepath.Rel(dir, pf); err == nil && !strings.HasPrefix(r, "..") {
				rel = filepath.ToSlash(r)
				break
			}
		}
		relativeFiles[i] = rel
		fileToAbs[rel] = pf
	}

	compiledFiles, err := compiler.Compile(context.Background(), relativeFiles...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error compiling protobuf files: %v\n", err)
		os.Exit(1)
	}

	registry := lint.NewRuleRegistry()
	if err := rules.Add(registry); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading rules: %v\n", err)
		os.Exit(1)
	}

	linter := lint.New(registry, lint.Configs{{EnabledRules: []string{"all"}}})
	descriptors := make([]protoreflect.FileDescriptor, len(compiledFiles))
	for i, f := range compiledFiles {
		descriptors[i] = f
	}

	responses, err := linter.LintProtos(descriptors...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error linting protos: %v\n", err)
		os.Exit(1)
	}

	opts := FixOptions{
		Rules:      ruleFilter,
		Categories: categoryFilter,
		Except:     exceptFilter,
	}

	var allProblems []CheckProblem
	filesWithProblems := make(map[string]bool)
	fixableCount := 0

	for _, resp := range responses {
		for _, prob := range resp.Problems {
			bufID := ruleNameToBufRuleID(prob.RuleID)
			if !matchesFilter(bufID, string(prob.RuleID), opts) {
				continue
			}

			isFixable := prob.Suggestion != "" && prob.Location != nil && len(prob.Location.Span) >= 3
			if *fixableOnly && !isFixable {
				continue
			}

			if isFixable {
				fixableCount++
			}

			line := 0
			col := 0
			if prob.Location != nil && len(prob.Location.Span) >= 2 {
				line = int(prob.Location.Span[0]) + 1
				col = int(prob.Location.Span[1]) + 1
			} else if prob.Descriptor != nil && prob.Descriptor.ParentFile() != nil {
				loc := prob.Descriptor.ParentFile().SourceLocations().ByDescriptor(prob.Descriptor)
				line = loc.StartLine + 1
				col = loc.StartColumn + 1
			}

			absPath := fileToAbs[resp.FilePath]
			if absPath == "" {
				absPath = resp.FilePath
			}

			allProblems = append(allProblems, CheckProblem{
				FilePath:   absPath,
				Line:       line,
				Column:     col,
				RuleID:     bufID,
				Message:    prob.Message,
				DocURL:     prob.GetRuleURI(),
				Suggestion: prob.Suggestion,
				Fixable:    isFixable,
			})
			filesWithProblems[absPath] = true
		}
	}

	// Sort problems by FilePath, Line, Column
	sort.Slice(allProblems, func(i, j int) bool {
		if allProblems[i].FilePath != allProblems[j].FilePath {
			return allProblems[i].FilePath < allProblems[j].FilePath
		}
		if allProblems[i].Line != allProblems[j].Line {
			return allProblems[i].Line < allProblems[j].Line
		}
		return allProblems[i].Column < allProblems[j].Column
	})

	if *asJSON || *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(allProblems)
		if len(allProblems) > 0 {
			os.Exit(1)
		}
		return
	}

	if *format == "github-actions" || *format == "github" {
		for _, p := range allProblems {
			docPart := ""
			if p.DocURL != "" {
				docPart = fmt.Sprintf(" (%s)", p.DocURL)
			}
			fmt.Printf("::error file=%s,line=%d,col=%d::%s%s [%s]\n", p.FilePath, p.Line, p.Column, p.Message, docPart, p.RuleID)
		}
		if len(allProblems) > 0 {
			os.Exit(1)
		}
		return
	}

	for _, p := range allProblems {
		fixTag := ""
		if p.Fixable {
			fixTag = " [fixable]"
		}
		docPart := ""
		if p.DocURL != "" {
			docPart = fmt.Sprintf(" (%s)", p.DocURL)
		}
		fmt.Printf("%s:%d:%d: %s%s [%s]%s\n", p.FilePath, p.Line, p.Column, p.Message, docPart, p.RuleID, fixTag)
	}

	if len(allProblems) == 0 {
		fmt.Printf("Checked %d file(s). No AIP violations found!\n", len(protoFiles))
		return
	}

	fmt.Printf("\nChecked %d file(s).\n", len(protoFiles))
	fmt.Printf("Found %d violation(s) (%d auto-fixable) across %d file(s).\n", len(allProblems), fixableCount, len(filesWithProblems))
	if fixableCount > 0 {
		fmt.Printf("Tip: Run 'buf-plugin-aip fix' to automatically resolve %d fixable violation(s).\n", fixableCount)
	}

	os.Exit(1)
}
