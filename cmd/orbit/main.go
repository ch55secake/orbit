package main

import (
	"fmt"
	"os"

	"github.com/ch55secake/orbit/internal/cli"
	"github.com/ch55secake/orbit/internal/tunnel"
)

var version = "dev"

func main() {
	if tunnel.IsAskpassHelper() {
		if err := tunnel.RunAskpassHelper(); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := cli.New(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "orbit:", err)
		os.Exit(1)
	}
}
