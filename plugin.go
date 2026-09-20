package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/option"
	"github.com/googleapis/api-linter/v2/lint"
	"github.com/googleapis/api-linter/v2/rules"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	categoryAIP           = "AIP"
	categoryAIPCore       = "AIP_CORE"
	categoryAIPClientLibs = "AIP_CLIENT_LIBRARIES"
)

type resultsContextKey struct{}

type problemWithFile struct {
	filePath string
	problem  lint.Problem
}

// NewSpec creates the check.Spec for the AIP linter plugin.
func NewSpec() (*check.Spec, error) {
	registry := lint.NewRuleRegistry()
	if err := rules.Add(registry); err != nil {
		return nil, fmt.Errorf("failed to register AIP rules: %w", err)
	}

	aipCategories := make(map[string]string) // "AIP_0131" -> "0131"
	groupCategoryMap := map[string]string{
		"core":             categoryAIPCore,
		"client-libraries": categoryAIPClientLibs,
	}

	ruleSpecs := make([]*check.RuleSpec, 0, len(registry))
	for name := range registry {
		parts := strings.Split(string(name), "::")
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected rule name format %q: expected 3 parts separated by '::'", name)
		}

		group := parts[0]
		aipNum := parts[1]

		ruleID := ruleNameToBufRuleID(name)
		aipCatID := "AIP_" + aipNum
		aipCategories[aipCatID] = aipNum

		categoryIDs := []string{categoryAIP, aipCatID}
		if groupCat, ok := groupCategoryMap[group]; ok {
			categoryIDs = append(categoryIDs, groupCat)
		}

		isDefault := group == "core"
		purpose := fmt.Sprintf("Checks AIP rule %s.", name)

		ruleSpecs = append(ruleSpecs, &check.RuleSpec{
			ID:          ruleID,
			CategoryIDs: categoryIDs,
			Default:     isDefault,
			Purpose:     purpose,
			Type:        check.RuleTypeLint,
			Handler:     newRuleHandler(ruleID),
		})
	}

	// Sort rules deterministically.
	sort.Slice(ruleSpecs, func(i, j int) bool {
		return ruleSpecs[i].ID < ruleSpecs[j].ID
	})

	// Build categories.
	categorySpecs := []*check.CategorySpec{
		{
			ID:      categoryAIP,
			Purpose: "Checks all API Improvement Proposals (https://aip.dev).",
		},
		{
			ID:      categoryAIPCore,
			Purpose: "Checks core API Improvement Proposals (https://aip.dev).",
		},
		{
			ID:      categoryAIPClientLibs,
			Purpose: "Checks client library API Improvement Proposals (https://aip.dev).",
		},
	}

	for catID, aipNum := range aipCategories {
		trimmedNum := strings.TrimLeft(aipNum, "0")
		categorySpecs = append(categorySpecs, &check.CategorySpec{
			ID:      catID,
			Purpose: fmt.Sprintf("Checks rules from AIP-%s (https://aip.dev/%s).", trimmedNum, trimmedNum),
		})
	}

	sort.Slice(categorySpecs, func(i, j int) bool {
		return categorySpecs[i].ID < categorySpecs[j].ID
	})

	return &check.Spec{
		Rules:      ruleSpecs,
		Categories: categorySpecs,
		Before:     before(registry),
	}, nil
}

func newRuleHandler(ruleID string) check.RuleHandler {
	return check.RuleHandlerFunc(func(ctx context.Context, rw check.ResponseWriter, req check.Request) error {
		results, ok := ctx.Value(resultsContextKey{}).(map[string][]problemWithFile)
		if !ok {
			return errors.New("lint results not found in context")
		}
		for _, item := range results[ruleID] {
			addProblem(rw, item.filePath, item.problem)
		}
		return nil
	})
}

func before(registry lint.RuleRegistry) func(ctx context.Context, req check.Request) (context.Context, check.Request, error) {
	return func(ctx context.Context, req check.Request) (context.Context, check.Request, error) {
		var protoFiles []protoreflect.FileDescriptor
		for _, fd := range req.FileDescriptors() {
			if !fd.IsImport() {
				protoFiles = append(protoFiles, fd.ProtoreflectFileDescriptor())
			}
		}

		results := make(map[string][]problemWithFile)
		if len(protoFiles) == 0 {
			return context.WithValue(ctx, resultsContextKey{}, results), req, nil
		}

		var configs lint.Configs
		configPath, err := option.GetStringValue(req.Options(), "config")
		if err != nil {
			return nil, nil, err
		}
		if configPath == "" {
			configPath, err = option.GetStringValue(req.Options(), "config_file")
			if err != nil {
				return nil, nil, err
			}
		}

		if configPath != "" {
			c, err := lint.ReadConfigsFromFile(configPath)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read api-linter config from %q: %w", configPath, err)
			}
			configs = c
		} else {
			configs = lint.Configs{{EnabledRules: []string{"all"}}}
		}

		linter := lint.New(registry, configs)
		responses, err := linter.LintProtos(protoFiles...)
		if err != nil {
			return nil, nil, fmt.Errorf("api-linter failed: %w", err)
		}

		autoFix, _ := option.GetBoolValue(req.Options(), "auto_fix")
		if !autoFix {
			if s, err := option.GetStringValue(req.Options(), "auto_fix"); err == nil {
				autoFix = s == "true" || s == "1"
			}
		}

		baseDir, _ := option.GetStringValue(req.Options(), "base_dir")
		if baseDir == "" {
			baseDir = "."
		}

		fixedProblems := make(map[string]map[int]bool)
		if autoFix {
			fileReplacements := make(map[string][]textReplacement)
			for _, resp := range responses {
				for _, prob := range resp.Problems {
					if prob.Suggestion == "" || prob.Location == nil || len(prob.Location.Span) < 3 {
						continue
					}
					diskPath := resolveDiskPath(resp.FilePath, baseDir)
					span := prob.Location.Span
					r := textReplacement{
						startLine: int(span[0]),
						startCol:  int(span[1]),
						newText:   prob.Suggestion,
						ruleID:    ruleNameToBufRuleID(prob.RuleID),
						message:   prob.Message,
					}
					if len(span) == 4 {
						r.endLine = int(span[2])
						r.endCol = int(span[3])
					} else {
						r.endLine = int(span[0])
						r.endCol = int(span[2])
					}
					fileReplacements[diskPath] = append(fileReplacements[diskPath], r)
				}
			}

			for diskPath, reps := range fileReplacements {
				applied, _, err := applyReplacements(diskPath, reps, false)
				if err == nil && applied > 0 {
					fmt.Fprintf(os.Stderr, "[buf-plugin-aip] auto-fixed %d issue(s) in %s\n", applied, diskPath)
					if fixedProblems[diskPath] == nil {
						fixedProblems[diskPath] = make(map[int]bool)
					}
					for _, r := range reps {
						fixedProblems[diskPath][r.startLine] = true
					}
				}
			}
		}

		for _, resp := range responses {
			diskPath := resolveDiskPath(resp.FilePath, baseDir)
			for _, prob := range resp.Problems {
				if autoFix && prob.Location != nil && fixedProblems[diskPath] != nil && fixedProblems[diskPath][int(prob.Location.Span[0])] {
					// Suppress annotation since it was auto-fixed on disk
					continue
				}
				bufID := ruleNameToBufRuleID(prob.RuleID)
				filePath := resp.FilePath
				if filePath == "" && prob.Descriptor != nil && prob.Descriptor.ParentFile() != nil {
					filePath = prob.Descriptor.ParentFile().Path()
				}
				results[bufID] = append(results[bufID], problemWithFile{
					filePath: filePath,
					problem:  prob,
				})
			}
		}

		return context.WithValue(ctx, resultsContextKey{}, results), req, nil
	}
}

func resolveDiskPath(protoPath string, baseDir string) string {
	if filepath.IsAbs(protoPath) {
		if _, err := os.Stat(protoPath); err == nil {
			return protoPath
		}
	}
	if baseDir != "" && baseDir != "." {
		target := filepath.Join(baseDir, protoPath)
		if _, err := os.Stat(target); err == nil {
			return target
		}
	}
	if _, err := os.Stat(protoPath); err == nil {
		return protoPath
	}
	candidates := []string{
		filepath.Join("proto", protoPath),
		filepath.Join("protos", protoPath),
		filepath.Join("src", protoPath),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	var found string
	searchRoot := "."
	if baseDir != "" {
		searchRoot = baseDir
	}
	_ = filepath.Walk(searchRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(filepath.ToSlash(p), filepath.ToSlash(protoPath)) {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if found != "" {
		return found
	}
	return protoPath
}

func ruleNameToBufRuleID(name lint.RuleName) string {
	parts := strings.Split(string(name), "::")
	if len(parts) < 3 {
		s := strings.ReplaceAll(string(name), "::", "_")
		s = strings.ReplaceAll(s, "-", "_")
		return "AIP_" + strings.ToUpper(s)
	}
	rule := strings.ReplaceAll(parts[2], "-", "_")
	return "AIP_" + parts[1] + "_" + strings.ToUpper(rule)
}

func addProblem(rw check.ResponseWriter, filePath string, p lint.Problem) {
	message := p.Message
	if uri := p.GetRuleURI(); uri != "" {
		message = fmt.Sprintf("%s (%s)", message, uri)
	}
	opts := []check.AddAnnotationOption{
		check.WithMessage(message),
	}
	if p.Location != nil && len(p.Location.GetPath()) > 0 {
		sourcePath := make(protoreflect.SourcePath, len(p.Location.GetPath()))
		for i, v := range p.Location.GetPath() {
			sourcePath[i] = v
		}
		opts = append(opts, check.WithFileNameAndSourcePath(filePath, sourcePath))
	} else if p.Descriptor != nil {
		opts = append(opts, check.WithDescriptor(p.Descriptor))
	} else if filePath != "" {
		opts = append(opts, check.WithFileName(filePath))
	}
	rw.AddAnnotation(opts...)
}
