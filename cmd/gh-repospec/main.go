// gh-repospec is a GitHub CLI extension for declarative repository setup.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/kkamegawa/gh-repospec/internal/cli"
)

func main() {
	os.Exit(exitCode(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr), os.Stderr))
}

// exitCode maps a Run result onto the documented process exit code. A drift
// result carries its own code and has already reported itself on stderr, so
// it is not printed as an error.
func exitCode(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var drift *cli.DriftError
	if errors.As(err, &drift) {
		return drift.ExitCode()
	}
	_, _ = fmt.Fprintln(stderr, "error:", err)
	return 1
}
