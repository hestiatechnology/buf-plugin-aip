package main

import (
	"fmt"
	"log"
	"os"

	"buf.build/go/bufplugin/check"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Println(version)
			return
		case "check":
			runCheck(os.Args[2:])
			return
		case "fix":
			runFix(os.Args[2:])
			return
		case "explain":
			runExplain(os.Args[2:])
			return
		case "list-rules":
			runListRules(os.Args[2:])
			return
		case "--help", "-h", "help":
			printUsage()
			return
		}
	}

	spec, err := NewSpec()
	if err != nil {
		log.Fatalf("failed to create AIP plugin spec: %v", err)
	}

	check.Main(spec)
}

func printUsage() {
	fmt.Printf("buf-plugin-aip %s - Google AIP linter plugin for Buf\n\n", version)
	fmt.Println("Usage:")
	fmt.Println("  buf-plugin-aip [command] [flags]")
	fmt.Println("\nCommands:")
	fmt.Println("  check       Lint protobuf files directly with terminal summary output")
	fmt.Println("  fix         Auto-fix machine-correctable AIP violations in .proto files")
	fmt.Println("  explain     Show documentation, rationale, and metadata for an AIP rule or proposal")
	fmt.Println("  list-rules  List all available AIP rules and filter by category or auto-fixability")
	fmt.Println("\nFlags:")
	fmt.Println("  -v, --version  Show version information")
	fmt.Println("  -h, --help     Show this help message")
	fmt.Println("\nBuf Plugin Usage:")
	fmt.Println("  Configure 'buf-plugin-aip' under plugins in your buf.yaml and run 'buf lint'.")
}
