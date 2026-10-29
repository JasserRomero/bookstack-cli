// Command bsk is a command-line client for the BookStack API.
package main

import (
	"os"

	"github.com/JasserRomero/bookstack-cli/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=<version>".
var version = "dev"

func main() {
	if err := cli.Execute(cli.NewRootCmd(version)); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
