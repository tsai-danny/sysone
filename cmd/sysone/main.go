// Command sysone is the agent-friendly CLI for the llama.cpp System One
// decision-model API (POST /v1/systemone).
//
// stdout carries JSON data only; stderr carries diagnostics only.
// Exit codes: 0 all passed, 1 answer below threshold, 2 usage error,
// 3 contract error (400/501, local validation), 4 connection/server/loading.
package main

import (
	"os"

	"github.com/sysone/systemone/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
