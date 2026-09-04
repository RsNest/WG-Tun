package main

import (
	"fmt"
	"os"

	"proxyctl/internal/cli"
)

func main() {
	opt := cli.Options{}
	if err := cli.Run(os.Args[1:], opt); err != nil {
		fmt.Fprintf(os.Stderr, "proxctl: %v\n", err)
		os.Exit(1)
	}
}
