// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"lefalock/internal/app"
)

var version = "dev"

func main() {
	if err := app.Run(os.Args[1:], version); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "lefalock: %v\n", err)
		os.Exit(1)
	}
}
