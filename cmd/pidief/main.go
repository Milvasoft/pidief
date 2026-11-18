// Command pidief is Milvasoft's PDF toolkit: a CLI and, with `pidief serve`, an HTTP API.
package main

import (
	"errors"
	"fmt"
	"os"

	_ "time/tzdata" // Europe/Istanbul without OS time zone data

	"github.com/milvasoft/pidief/internal/cli"
)

func main() {
	if err := cli.Root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "pidief:", err)
		var ee *cli.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.Code)
		}
		os.Exit(1)
	}
}
