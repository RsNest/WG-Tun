package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func runHealthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	url := fs.String("url", "https://127.0.0.1:8443/readyz", "URL to probe")
	insecure := fs.Bool("k", false, "skip TLS certificate verification")
	insecureLong := fs.Bool("insecure", false, "skip TLS certificate verification")
	timeout := fs.Duration("timeout", 2*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if *insecure || *insecureLong {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit healthcheck -k
	}
	client := &http.Client{Timeout: *timeout, Transport: tr}
	resp, err := client.Get(*url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("healthcheck %s: HTTP %d", *url, resp.StatusCode)
	}
	return nil
}

func maybeHealthcheck() bool {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := runHealthcheck(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "controller healthcheck: %v\n", err)
			os.Exit(1)
		}
		return true
	}
	return false
}
