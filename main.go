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
		case "fix":
			runFix(os.Args[2:])
			return
		}
	}

	spec, err := NewSpec()
	if err != nil {
		log.Fatalf("failed to create AIP plugin spec: %v", err)
	}

	check.Main(spec)
}
