package main

import (
	"os"

	"github.com/veil-panel/veil/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.NewRootCommand(version).Execute(); err != nil {
		os.Exit(1)
	}
}
