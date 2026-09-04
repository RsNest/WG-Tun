package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

func runHealthcheck(args []string) error {
	url := "http://127.0.0.1:9101/healthz"
	timeout := 2 * time.Second
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--url":
			i++
			if i < len(args) {
				url = args[i]
			}
		case "--timeout":
			i++
			if i < len(args) {
				d, err := time.ParseDuration(args[i])
				if err != nil {
					return err
				}
				timeout = d
			}
		}
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("healthcheck %s: HTTP %d", url, resp.StatusCode)
	}
	return nil
}

func maybeHealthcheck() bool {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := runHealthcheck(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "agent healthcheck: %v\n", err)
			os.Exit(1)
		}
		return true
	}
	return false
}
