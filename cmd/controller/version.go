package main

import (
	"fmt"
	"os"

	"proxyctl/internal/version"
)

func maybeVersion() bool {
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-version" || a == "version" {
			fmt.Println(version.Line("proxyctl-controller"))
			return true
		}
	}
	return false
}
