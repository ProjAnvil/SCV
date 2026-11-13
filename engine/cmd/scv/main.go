// Command scv is the CLI surface of the SCV engine (spec section 6):
// the run pipeline with its NDJSON stdout contract, job inspection and
// cancellation, repository management, structure-graph queries, and the
// environment snapshot.
package main

import (
	"errors"
	"os"
)

func main() {
	err := newRootCmd().Execute()
	var ee *codedError
	switch {
	case err == nil:
		return
	case errors.As(err, &ee):
		// Command failures carry their documented exit code; cobra has
		// already printed the message to stderr.
		os.Exit(ee.code)
	default:
		// Anything else reaching here is a flag/argument error, which
		// cobra has already reported together with usage text.
		os.Exit(exitUsage)
	}
}
