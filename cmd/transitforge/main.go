package main

import (
	"fmt"
	"os"

	"transitforge/internal/cli"
)

func main() {
	opt := cli.Options{}
	if err := cli.Run(os.Args[1:], opt); err != nil {
		fmt.Fprintf(os.Stderr, "transitforge: %v\n", err)
		os.Exit(1)
	}
}
