package main

import (
	"bytes"
	"context"
	"embed"
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
	"github.com/pmezard/go-difflib/difflib"
	"google.golang.org/protobuf/reflect/protoreflect"
)

//go:embed proto_deps
var embeddedProtoDeps embed.FS

type embeddedResolver struct {
	fallback protocompile.Resolver
}

func (r *embeddedResolver) FindFileByPath(path string) (protocompile.SearchResult, error) {
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	data, err := embeddedProtoDeps.ReadFile("proto_deps/" + cleanPath)
	if err == nil {
		return protocompile.SearchResult{Source: bytes.NewReader(data)}, nil
	}
	if r.fallback != nil {
		return r.fallback.FindFileByPath(path)
	}
	return protocompile.SearchResult{}, os.ErrNotExist
}

type textReplacement struct {
	startLine int
	startCol  int
	endLine   int
	endCol    int
	newText   string
	ruleID    string
	message   string
}

// FixOptions configures the fix runner.
type FixOptions struct {
	DryRun      bool
	Diff        bool
	ImportPaths []string
	Rules       []string
	Categories  []string
	Except      []string
}

// FixResult records changes made to a file.
type FixResult struct {
	FilePath string
	Diff     string
	Fixed    int
}

func runFix(args []string) {
	fs := flag.NewFlagSet("fix", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Print changes without modifying files on disk")
	diff := fs.Bool("diff", false, "Print unified diffs of suggested fixes")
	var importPaths stringListFlag
	fs.Var(&importPaths, "I", "Import directories to search for dependencies")
	var ruleFilter stringListFlag
	fs.Var(&ruleFilter, "rule", "Apply only specific rule(s), e.g. AIP_0142_TIME_FIELD_NAMES")
	var categoryFilter stringListFlag
	fs.Var(&categoryFilter, "category", "Apply only rules in category, e.g. AIP_0142, AIP_CORE")
	var exceptFilter stringListFlag
	fs.Var(&exceptFilter, "except", "Exclude specific rule(s) or category, e.g. AIP_0122_NAME_SUFFIX")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: buf-plugin-aip fix [flags] [paths...]\n\n")
		fmt.Fprintf(os.Stderr, "Automatically fixes AIP violations in .proto files.\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	targets := fs.Args()
	if len(targets) == 0 {
		targets = []string{"."}
	}

	results, err := FixFiles(context.Background(), targets, FixOptions{
		DryRun:      *dryRun,
		Diff:        *diff,
		ImportPaths: importPaths,
		Rules:       ruleFilter,
		Categories:  categoryFilter,
		Except:      exceptFilter,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	totalFixed := 0
	for _, res := range results {
		totalFixed += res.Fixed
		if res.Fixed > 0 {
			if *dryRun {
				fmt.Printf("[dry-run] %s: %d fixes available\n", res.FilePath, res.Fixed)
			} else {
				fmt.Printf("[fixed]   %s: applied %d fixes\n", res.FilePath, res.Fixed)
			}
			if *diff && res.Diff != "" {
				fmt.Print(res.Diff)
			}
		}
	}

	if totalFixed == 0 {
		fmt.Println("No auto-fixable AIP violations found.")
	} else {
		action := "applied"
		if *dryRun {
			action = "found"
		}
		fmt.Printf("\nDone: %d total fixes %s across %d files.\n", totalFixed, action, len(results))
	}
}

type stringListFlag []string

func (s *stringListFlag) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringListFlag) Set(val string) error {
	// Support comma-separated values as well
	parts := strings.Split(val, ",")
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			*s = append(*s, trimmed)
		}
	}
	return nil
}

// FixFiles finds and auto-fixes AIP violations in the given directories or files.
func FixFiles(ctx context.Context, paths []string, opts FixOptions) ([]FixResult, error) {
	protoFiles, importDirs, err := discoverProtoFiles(paths, opts.ImportPaths)
	if err != nil {
		return nil, err
	}
	if len(protoFiles) == 0 {
		return nil, nil
	}

	// Run up to 3 passes to fix dependent rules.
	var allResults []FixResult
	fileFixCount := make(map[string]int)
	fileDiffMap := make(map[string]string)
	originalContentMap := make(map[string][]byte)

	// Save original content for diff generation
	for _, pf := range protoFiles {
		if data, err := os.ReadFile(pf); err == nil {
			originalContentMap[pf] = data
		}
	}

	for pass := 0; pass < 3; pass++ {
		var compileErrors []error
		compiler := protocompile.Compiler{
			Resolver: wellknownimports.WithStandardImports(
				&embeddedResolver{
					fallback: &protocompile.SourceResolver{
						ImportPaths: importDirs,
					},
				},
			),
			Reporter: reporter.NewReporter(
				func(err reporter.ErrorWithPos) error {
					compileErrors = append(compileErrors, err)
					return err
				},
				func(reporter.ErrorWithPos) {},
			),
			SourceInfoMode: protocompile.SourceInfoExtraOptionLocations,
		}

		// Prepare relative paths from import dirs
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

		compiledFiles, err := compiler.Compile(ctx, relativeFiles...)
		if err != nil {
			if pass > 0 {
				// Earlier pass applied fixes that may require manual follow-ups (e.g. renaming the message definition).
				break
			}
			return nil, fmt.Errorf("failed to compile proto files: %w (details: %v)", err, compileErrors)
		}

		// Run linter
		registry := lint.NewRuleRegistry()
		if err := rules.Add(registry); err != nil {
			return nil, err
		}

		linter := lint.New(registry, lint.Configs{{EnabledRules: []string{"all"}}})
		descriptors := make([]protoreflect.FileDescriptor, len(compiledFiles))
		for i, f := range compiledFiles {
			descriptors[i] = f
		}

		responses, err := linter.LintProtos(descriptors...)
		if err != nil {
			return nil, err
		}

		// Group suggestions by file
		fileReplacements := make(map[string][]textReplacement)
		for _, resp := range responses {
			for _, prob := range resp.Problems {
				if prob.Suggestion == "" || prob.Location == nil || len(prob.Location.Span) < 3 {
					continue
				}

				if prob.RuleID == "core::0191::proto-version" {
					if f, ok := prob.Descriptor.(protoreflect.FileDescriptor); ok && f.Syntax() == protoreflect.Editions {
						continue
					}
				}

				bufID := ruleNameToBufRuleID(prob.RuleID)
				if !matchesFilter(bufID, string(prob.RuleID), opts) {
					continue
				}

				span := prob.Location.Span
				r := textReplacement{
					startLine: int(span[0]),
					startCol:  int(span[1]),
					newText:   prob.Suggestion,
					ruleID:    bufID,
					message:   prob.Message,
				}
				if len(span) == 4 {
					r.endLine = int(span[2])
					r.endCol = int(span[3])
				} else {
					r.endLine = int(span[0])
					r.endCol = int(span[2])
				}

				absPath := fileToAbs[resp.FilePath]
				if absPath == "" {
					absPath = resp.FilePath
				}
				fileReplacements[absPath] = append(fileReplacements[absPath], r)
			}
		}

		fixesThisPass := 0
		for filePath, replacements := range fileReplacements {
			applied, diffText, err := applyReplacements(filePath, replacements, opts.DryRun)
			if err != nil {
				return nil, fmt.Errorf("failed to apply fixes to %s: %w", filePath, err)
			}
			if applied > 0 {
				fixesThisPass += applied
				fileFixCount[filePath] += applied
				fileDiffMap[filePath] = diffText
			}
		}

		if fixesThisPass == 0 {
			break
		}
	}

	for f, count := range fileFixCount {
		allResults = append(allResults, FixResult{
			FilePath: f,
			Fixed:    count,
			Diff:     fileDiffMap[f],
		})
	}
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].FilePath < allResults[j].FilePath
	})

	return allResults, nil
}

func matchesFilter(bufID string, rawRuleID string, opts FixOptions) bool {
	// If except matches, drop it
	for _, exc := range opts.Except {
		excUpper := strings.ToUpper(exc)
		if bufID == excUpper || strings.EqualFold(rawRuleID, exc) || strings.HasPrefix(bufID, excUpper+"_") {
			return false
		}
	}

	// If category filter is set, must match at least one category
	if len(opts.Categories) > 0 {
		matchedCat := false
		for _, cat := range opts.Categories {
			catUpper := strings.ToUpper(cat)
			if catUpper == "AIP" {
				matchedCat = true
				break
			}
			if catUpper == "AIP_CORE" && strings.HasPrefix(rawRuleID, "core::") {
				matchedCat = true
				break
			}
			if catUpper == "AIP_CLIENT_LIBRARIES" && strings.HasPrefix(rawRuleID, "client-libraries::") {
				matchedCat = true
				break
			}
			// AIP_0142 matches AIP_0142_...
			if strings.HasPrefix(bufID, catUpper+"_") || bufID == catUpper {
				matchedCat = true
				break
			}
		}
		if !matchedCat {
			return false
		}
	}

	// If rule filter is set, must match at least one rule
	if len(opts.Rules) > 0 {
		matchedRule := false
		for _, r := range opts.Rules {
			rUpper := strings.ToUpper(r)
			if bufID == rUpper || strings.EqualFold(rawRuleID, r) {
				matchedRule = true
				break
			}
		}
		if !matchedRule {
			return false
		}
	}

	return true
}

func applyReplacements(filePath string, reps []textReplacement, dryRun bool) (int, string, error) {
	origContent, err := os.ReadFile(filePath)
	if err != nil {
		return 0, "", err
	}
	content := origContent

	lineOffsets := []int{0}
	for i, b := range content {
		if b == '\n' {
			lineOffsets = append(lineOffsets, i+1)
		}
	}

	// Sort replacements in reverse order (bottom to top, right to left)
	sort.Slice(reps, func(i, j int) bool {
		if reps[i].startLine != reps[j].startLine {
			return reps[i].startLine > reps[j].startLine
		}
		return reps[i].startCol > reps[j].startCol
	})

	applied := 0
	lastStartByte := len(content) + 1

	for _, rep := range reps {
		if rep.startLine >= len(lineOffsets) || rep.endLine >= len(lineOffsets) {
			continue
		}
		startByte := lineOffsets[rep.startLine] + rep.startCol
		endByte := lineOffsets[rep.endLine] + rep.endCol

		if startByte < 0 || endByte > len(content) || startByte > endByte {
			continue
		}
		// Prevent overlapping replacements
		if endByte > lastStartByte {
			continue
		}

		lastStartByte = startByte
		newContent := make([]byte, 0, len(content)-endByte+startByte+len(rep.newText))
		newContent = append(newContent, content[:startByte]...)
		newContent = append(newContent, []byte(rep.newText)...)
		newContent = append(newContent, content[endByte:]...)
		content = newContent

		applied++

		// Recompute line offsets after replacement
		lineOffsets = []int{0}
		for i, b := range content {
			if b == '\n' {
				lineOffsets = append(lineOffsets, i+1)
			}
		}
	}

	var diffText string
	if applied > 0 {
		diff := difflib.UnifiedDiff{
			A:        difflib.SplitLines(string(origContent)),
			B:        difflib.SplitLines(string(content)),
			FromFile: "a/" + filepath.Base(filePath),
			ToFile:   "b/" + filepath.Base(filePath),
			Context:  3,
		}
		diffText, _ = difflib.GetUnifiedDiffString(diff)

		if !dryRun {
			if err := os.WriteFile(filePath, content, 0644); err != nil {
				return 0, "", err
			}
		}
	}

	return applied, diffText, nil
}

func discoverProtoFiles(targets []string, customImports []string) ([]string, []string, error) {
	var files []string
	var importDirs []string

	for _, imp := range customImports {
		abs, err := filepath.Abs(imp)
		if err == nil {
			importDirs = append(importDirs, abs)
		}
	}

	for _, target := range targets {
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return nil, nil, err
		}

		info, err := os.Stat(absTarget)
		if err != nil {
			return nil, nil, err
		}

		if !info.IsDir() {
			files = append(files, absTarget)
			importDirs = append(importDirs, filepath.Dir(absTarget))
			continue
		}

		importDirs = append(importDirs, absTarget)

		err = filepath.Walk(absTarget, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !fi.IsDir() && strings.HasSuffix(path, ".proto") {
				abs, err := filepath.Abs(path)
				if err != nil {
					return err
				}
				files = append(files, abs)
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}

	// Deduplicate importDirs
	seen := make(map[string]struct{})
	var uniqueDirs []string
	for _, d := range importDirs {
		if _, ok := seen[d]; !ok {
			seen[d] = struct{}{}
			uniqueDirs = append(uniqueDirs, d)
		}
	}

	return files, uniqueDirs, nil
}
