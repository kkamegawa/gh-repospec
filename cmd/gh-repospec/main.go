// gh-repospec is a GitHub CLI extension for declarative repository setup.
package main

import (
	"fmt"
	"os"

	"github.com/kkamegawa/gh-repospec/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
