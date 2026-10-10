package main

import (
	"fmt"
	"os"

	"github.com/ch55secake/orbit/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.New(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "orbit:", err)
		os.Exit(1)
	}
}
