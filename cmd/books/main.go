package main

import (
	"fmt"
	"os"

	"github.com/dispatchlabs-ai/books/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		if !cli.IsRendered(err) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(cli.ExitCode(err))
	}
}
