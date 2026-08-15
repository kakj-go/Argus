// Package argusctl contains the installation CLI application.
package argusctl

import (
	"fmt"
	"io"

	"github.com/kakj-go/Argus/internal/buildinfo"
)

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		_, _ = fmt.Fprintf(stdout, "argusctl %s (commit=%s, built=%s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return 0
	}

	_, _ = fmt.Fprintln(stderr, "usage: argusctl version")
	return 2
}
