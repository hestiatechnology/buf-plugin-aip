package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/googleapis/api-linter/v2/lint"
	"github.com/googleapis/api-linter/v2/rules"
)

// RuleMeta contains complete metadata about an AIP lint rule.
type RuleMeta struct {
	ID          string   `json:"id"`
	RawName     string   `json:"raw_name"`
	AIP         string   `json:"aip"`
	Group       string   `json:"group"`
	Purpose     string   `json:"purpose"`
	DocURL      string   `json:"doc_url"`
	SpecURL     string   `json:"spec_url"`
	IsDefault   bool     `json:"default"`
	Fixable     bool     `json:"fixable"`
	CategoryIDs []string `json:"category_ids"`
}

var knownFixableRules = map[string]bool{
	"AIP_0122_EMBEDDED_RESOURCE":                  true,
	"AIP_0122_NAME_SUFFIX":                        true,
	"AIP_0123_NAME_NEVER_OPTIONAL":                true,
	"AIP_0123_RESOURCE_REFERENCE_TYPE":            true,
	"AIP_0126_UNSPECIFIED":                        true,
	"AIP_0126_UPPER_SNAKE_VALUES":                 true,
	"AIP_0128_RESOURCE_ANNOTATIONS_FIELD":         true,
	"AIP_0128_RESOURCE_RECONCILING_FIELD":         true,
	"AIP_0131_METHOD_SIGNATURE":                   true,
	"AIP_0131_REQUEST_MESSAGE_NAME":               true,
	"AIP_0131_RESPONSE_MESSAGE_NAME":              true,
	"AIP_0131_SYNONYMS":                           true,
	"AIP_0132_METHOD_SIGNATURE":                   true,
	"AIP_0132_REQUEST_MESSAGE_NAME":               true,
	"AIP_0132_RESPONSE_MESSAGE_NAME":              true,
	"AIP_0133_METHOD_SIGNATURE":                   true,
	"AIP_0133_REQUEST_MESSAGE_NAME":               true,
	"AIP_0133_REQUEST_RESOURCE_FIELD":             true,
	"AIP_0133_RESPONSE_LRO":                       true,
	"AIP_0133_RESPONSE_MESSAGE_NAME":              true,
	"AIP_0133_SYNONYMS":                           true,
	"AIP_0134_METHOD_SIGNATURE":                   true,
	"AIP_0134_REQUEST_MESSAGE_NAME":               true,
	"AIP_0134_REQUEST_RESOURCE_FIELD":             true,
	"AIP_0134_RESPONSE_LRO":                       true,
	"AIP_0134_RESPONSE_MESSAGE_NAME":              true,
	"AIP_0134_SYNONYMS":                           true,
	"AIP_0135_METHOD_SIGNATURE":                   true,
	"AIP_0135_REQUEST_MESSAGE_NAME":               true,
	"AIP_0135_RESPONSE_LRO":                       true,
	"AIP_0135_RESPONSE_MESSAGE_NAME":              true,
	"AIP_0140_ABBREVIATIONS":                      true,
	"AIP_0140_LOWER_SNAKE":                        true,
	"AIP_0140_UNDERSCORES":                        true,
	"AIP_0140_URI":                                true,
	"AIP_0141_COUNT_SUFFIX":                       true,
	"AIP_0141_FORBIDDEN_TYPES":                    true,
	"AIP_0142_TIME_FIELD_NAMES":                   true,
	"AIP_0142_TIME_FIELD_TYPE":                    true,
	"AIP_0142_TIME_OFFSET_TYPE":                   true,
	"AIP_0143_STANDARDIZED_CODES":                 true,
	"AIP_0143_STRING_TYPE":                        true,
	"AIP_0144_REQUEST_MESSAGE_NAME":               true,
	"AIP_0148_HUMAN_NAMES":                        true,
	"AIP_0148_USE_UID":                            true,
	"AIP_0152_REQUEST_MESSAGE_NAME":               true,
	"AIP_0152_REQUEST_RESOURCE_SUFFIX":            true,
	"AIP_0152_RESPONSE_MESSAGE_NAME":              true,
	"AIP_0154_FIELD_TYPE":                         true,
	"AIP_0154_NO_DUPLICATE_ETAG":                  true,
	"AIP_0158_REQUEST_SKIP_FIELD":                 true,
	"AIP_0158_RESPONSE_PLURAL_FIRST_FIELD":        true,
	"AIP_0160_FILTER_FIELD_NAME":                  true,
	"AIP_0160_FILTER_FIELD_TYPE":                  true,
	"AIP_0162_COMMIT_RESPONSE_MESSAGE_NAME":          true,
	"AIP_0162_DELETE_REVISION_RESPONSE_MESSAGE_NAME": true,
	"AIP_0162_ROLLBACK_RESPONSE_MESSAGE_NAME":        true,
	"AIP_0162_TAG_REVISION_RESPONSE_MESSAGE_NAME":     true,
	"AIP_0163_SYNONYMS":                           true,
	"AIP_0164_RESPONSE_LRO":                       true,
	"AIP_0164_RESPONSE_MESSAGE_NAME":              true,
	"AIP_0165_REQUEST_FORCE_FIELD":                true,
	"AIP_0165_RESPONSE_MESSAGE_NAME":              true,
	"AIP_0165_RESPONSE_PURGE_COUNT_FIELD":         true,
	"AIP_0165_RESPONSE_PURGE_SAMPLE_FIELD":        true,
	"AIP_0191_CSHARP_NAMESPACE":                   true,
	"AIP_0191_JAVA_PACKAGE":                       true,
	"AIP_0191_PHP_NAMESPACE":                      true,
	"AIP_0191_PROTO_VERSION":                      true,
	"AIP_0191_RUBY_PACKAGE":                       true,
	"AIP_0214_TTL_TYPE":                           true,
	"AIP_0216_SYNONYMS":                           true,
	"AIP_0216_VALUE_SYNONYMS":                     true,
	"AIP_0217_RETURN_PARTIAL_SUCCESS_TYPE":        true,
	"AIP_0217_SYNONYMS":                           true,
	"AIP_0217_UNREACHABLE_FIELD_TYPE":             true,
	"AIP_0231_REQUEST_NAMES_FIELD":                true,
	"AIP_0233_REQUEST_REQUESTS_FIELD":             true,
	"AIP_0233_RESPONSE_MESSAGE_NAME":              true,
	"AIP_0234_REQUEST_REQUESTS_FIELD":             true,
	"AIP_0234_RESPONSE_MESSAGE_NAME":              true,
	"AIP_0235_REQUEST_NAMES_FIELD":                true,
	"AIP_0235_RESPONSE_MESSAGE_NAME":              true,
}

// GetAllRules returns metadata for all available rules.
func GetAllRules() ([]RuleMeta, error) {
	registry := lint.NewRuleRegistry()
	if err := rules.Add(registry); err != nil {
		return nil, err
	}

	metas := make([]RuleMeta, 0, len(registry))
	for name := range registry {
		parts := strings.Split(string(name), "::")
		if len(parts) != 3 {
			continue
		}

		group := parts[0]
		aipNum := parts[1]
		trimmedAIP := strings.TrimLeft(aipNum, "0")
		ruleID := ruleNameToBufRuleID(name)

		categoryIDs := []string{categoryAIP, "AIP_" + aipNum}
		if group == "core" {
			categoryIDs = append(categoryIDs, categoryAIPCore)
		} else if group == "client-libraries" {
			categoryIDs = append(categoryIDs, categoryAIPClientLibs)
		}

		metas = append(metas, RuleMeta{
			ID:          ruleID,
			RawName:     string(name),
			AIP:         trimmedAIP,
			Group:       group,
			Purpose:     fmt.Sprintf("Checks AIP rule %s.", name),
			DocURL:      fmt.Sprintf("https://linter.aip.dev/%s/%s", trimmedAIP, parts[2]),
			SpecURL:     fmt.Sprintf("https://aip.dev/%s", trimmedAIP),
			IsDefault:   group == "core",
			Fixable:     knownFixableRules[ruleID],
			CategoryIDs: categoryIDs,
		})
	}

	sort.Slice(metas, func(i, j int) bool {
		return metas[i].ID < metas[j].ID
	})

	return metas, nil
}

func runExplain(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: buf-plugin-aip explain <RULE_ID | AIP_NUMBER>\n\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  buf-plugin-aip explain AIP_0131_HTTP_BODY\n")
		fmt.Fprintf(os.Stderr, "  buf-plugin-aip explain core::0131::http-body\n")
		fmt.Fprintf(os.Stderr, "  buf-plugin-aip explain 131\n")
		os.Exit(1)
	}

	query := strings.TrimSpace(args[0])
	allRules, err := GetAllRules()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// 1. Exact match on Rule ID or Raw Name
	queryUpper := strings.ToUpper(query)
	for _, r := range allRules {
		if strings.EqualFold(r.ID, queryUpper) || strings.EqualFold(r.RawName, query) {
			printRuleDetail(r)
			return
		}
	}

	// 2. Match on AIP number (e.g. "131", "0131", "AIP_0131", "aip-131")
	trimmedQuery := strings.TrimPrefix(strings.TrimPrefix(queryUpper, "AIP_"), "AIP-")
	trimmedQuery = strings.TrimLeft(trimmedQuery, "0")
	var aipRules []RuleMeta
	for _, r := range allRules {
		if r.AIP == trimmedQuery {
			aipRules = append(aipRules, r)
		}
	}

	if len(aipRules) > 0 {
		fmt.Printf("AIP-%s (Specification: https://aip.dev/%s)\n\n", trimmedQuery, trimmedQuery)
		fmt.Printf("Rules in AIP-%s (%d rules):\n", trimmedQuery, len(aipRules))
		for _, r := range aipRules {
			fixableTag := ""
			if r.Fixable {
				fixableTag = " [auto-fixable]"
			}
			fmt.Printf("  %-35s %s%s\n", r.ID, r.Purpose, fixableTag)
		}
		return
	}

	// 3. Substring search for suggestions
	var matches []RuleMeta
	for _, r := range allRules {
		if strings.Contains(strings.ToUpper(r.ID), queryUpper) || strings.Contains(strings.ToLower(r.RawName), strings.ToLower(query)) {
			matches = append(matches, r)
		}
	}

	if len(matches) > 0 {
		fmt.Printf("No exact rule found for %q. Did you mean:\n", query)
		for _, m := range matches {
			fmt.Printf("  - %s (%s)\n", m.ID, m.DocURL)
		}
	} else {
		fmt.Printf("No rule or AIP found matching %q.\n", query)
	}
}

func printRuleDetail(r RuleMeta) {
	fixableStr := "No"
	if r.Fixable {
		fixableStr = "Yes (supports `buf-plugin-aip fix` and `options.auto_fix`)"
	}
	defaultStr := "No"
	if r.IsDefault {
		defaultStr = "Yes (part of AIP_CORE)"
	}

	fmt.Printf("Rule ID:        %s\n", r.ID)
	fmt.Printf("Raw Name:       %s\n", r.RawName)
	fmt.Printf("AIP:            AIP-%s (%s)\n", r.AIP, r.SpecURL)
	fmt.Printf("Categories:     %s\n", strings.Join(r.CategoryIDs, ", "))
	fmt.Printf("Default Rule:   %s\n", defaultStr)
	fmt.Printf("Auto-Fixable:   %s\n", fixableStr)
	fmt.Printf("Linter Docs:    %s\n", r.DocURL)
	fmt.Printf("Purpose:        %s\n", r.Purpose)
}

func runListRules(args []string) {
	fs := flag.NewFlagSet("list-rules", flag.ExitOnError)
	category := fs.String("category", "", "Filter by category, e.g. AIP_CORE, AIP_0131")
	fixableOnly := fs.Bool("fixable", false, "Only list rules that can be auto-fixed")
	search := fs.String("search", "", "Search term to match rule ID or purpose")
	asJSON := fs.Bool("json", false, "Output rules as JSON")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: buf-plugin-aip list-rules [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	allRules, err := GetAllRules()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var filtered []RuleMeta
	searchUpper := strings.ToUpper(*search)
	catUpper := strings.ToUpper(*category)

	for _, r := range allRules {
		if *fixableOnly && !r.Fixable {
			continue
		}
		if catUpper != "" {
			matched := false
			for _, c := range r.CategoryIDs {
				if c == catUpper {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if searchUpper != "" {
			if !strings.Contains(r.ID, searchUpper) && !strings.Contains(strings.ToUpper(r.Purpose), searchUpper) {
				continue
			}
		}
		filtered = append(filtered, r)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(filtered)
		return
	}

	fmt.Printf("%-36s %-8s %-9s %-9s %s\n", "RULE ID", "AIP", "DEFAULT", "FIXABLE", "DOCS")
	fmt.Println(strings.Repeat("-", 100))
	for _, r := range filtered {
		defStr := "no"
		if r.IsDefault {
			defStr = "yes"
		}
		fixStr := "no"
		if r.Fixable {
			fixStr = "yes"
		}
		fmt.Printf("%-36s %-8s %-9s %-9s %s\n", r.ID, "AIP-"+r.AIP, defStr, fixStr, r.DocURL)
	}
	fmt.Printf("\nTotal: %d rules\n", len(filtered))
}
