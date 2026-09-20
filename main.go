package main

import (
	"fmt"
	"log"
	"os"

	"buf.build/go/bufplugin/check"
)

var version = "dev"

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			fmt.Println(version)
			return
		}
	}

	spec, err := NewSpec()
	if err != nil {
		log.Fatalf("failed to create AIP plugin spec: %v", err)
	}

	check.Main(spec)
}
