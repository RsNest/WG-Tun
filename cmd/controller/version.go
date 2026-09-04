package main

import (
	"fmt"
	"os"

	"transitforge/internal/version"
)

func maybeVersion() bool {
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-version" || a == "version" {
			fmt.Println(version.Line("transitforge-controller"))
			return true
		}
	}
	return false
}
