// Package python holds the Python bot runner, runner.py, for the local server
// host, which runs it in process when no runner functions are configured
// (#225). Lambda runs the same file as the python3.14 function's handler,
// zipped by Terraform (deploy/terraform/bot_runners.tf).
package python

import _ "embed"

// Runner is runner.py.
//
//go:embed runner.py
var Runner []byte
