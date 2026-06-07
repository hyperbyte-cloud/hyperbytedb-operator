// Minimal hyperbytedb stand-in for operator e2e tests.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "config file path")
	flag.Parse()

	if len(flag.Args()) != 1 || flag.Arg(0) != "serve" {
		fmt.Fprintln(os.Stderr, "usage: hyperbytedb --config <path> serve")
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if err := http.ListenAndServe(":8086", mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
