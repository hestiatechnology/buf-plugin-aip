package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/option"
	"github.com/googleapis/api-linter/v2/lint"
	"github.com/googleapis/api-linter/v2/rules"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	categoryAIP            = "AIP"
	categoryAIPCore        = "AIP_CORE"
	categoryAIPClientLibs  = "AIP_CLIENT_LIBRARIES"
	categoryAIPRecommended = "AIP_RECOMMENDED"
	categoryAIPCRUD        = "AIP_CRUD"
	categoryAIPNoLang      = "AIP_NOLANG"
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

		// Curated Presets
		isCRUD := aipNum == "0131" || aipNum == "0132" || aipNum == "0133" || aipNum == "0134" || aipNum == "0135" || aipNum == "0136"
		if isCRUD {
			categoryIDs = append(categoryIDs, categoryAIPCRUD)
		}

		isLanguageRule := strings.HasPrefix(ruleID, "AIP_0191_JAVA_") ||
			strings.HasPrefix(ruleID, "AIP_0191_CSHARP_") ||
			strings.HasPrefix(ruleID, "AIP_0191_PHP_") ||
			strings.HasPrefix(ruleID, "AIP_0191_RUBY_")
		if group == "core" && !isLanguageRule {
			categoryIDs = append(categoryIDs, categoryAIPNoLang)
		}

		isCommentRule := strings.HasPrefix(ruleID, "AIP_0192_")
		isPrepositionRule := ruleID == "AIP_0140_PREPOSITIONS" || ruleID == "AIP_0136_PREPOSITIONS"

		if group == "core" && !isLanguageRule && !isCommentRule && !isPrepositionRule {
			categoryIDs = append(categoryIDs, categoryAIPRecommended)
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
		{
			ID:      categoryAIPRecommended,
			Purpose: "Checks recommended core API rules (omits comment and language-specific packaging checks).",
		},
		{
			ID:      categoryAIPCRUD,
			Purpose: "Checks standard resource CRUD methods (AIP-131 through AIP-136).",
		},
		{
			ID:      categoryAIPNoLang,
			Purpose: "Checks core API rules without language-specific packaging requirements.",
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

		for _, resp := range responses {
			for _, prob := range resp.Problems {
				// Protobuf Editions (2023+) is the modern successor to proto3.
				// api-linter was written before Editions and checks f.Syntax() != Proto3.
				// If the file uses Editions, skip AIP_0191_PROTO_VERSION.
				if prob.RuleID == "core::0191::proto-version" {
					if f, ok := prob.Descriptor.(protoreflect.FileDescriptor); ok && f.Syntax() == protoreflect.Editions {
						continue
					}
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
