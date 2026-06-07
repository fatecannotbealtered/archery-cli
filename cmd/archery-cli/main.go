package main

import (
	"os"

	"github.com/fatecannotbealtered/archery-cli/cmd"
)

func main() {
	_ = cmd.Execute()
	os.Exit(cmd.LastExitCode())
}
