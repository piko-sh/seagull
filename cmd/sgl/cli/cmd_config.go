// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package cli

import (
	"context"
	"fmt"

	"seagull.sh/seagull/cmd/internal/config"
)

// configUsage is the usage of the config command group, with %[1]s for the program name.
const configUsage = `Usage: %[1]s config schema | %[1]s config validate [path]

Subcommands:
  schema           Print the configuration JSON Schema
  validate [path]  Validate a configuration file: path, the -c file, or the one %[1]s finds

Run "%[1]s config <subcommand> -h" for its usage.
`

// runConfig prints the configuration JSON Schema, or validates a configuration file.
//
// Takes arguments ([]string) which holds the subcommand and, for validate, an optional
// path.
//
// Returns int which is the exit status.
func (inv *invocation) runConfig(_ context.Context, arguments []string) int {
	if status, proceed := inv.runGroup(arguments, configUsage); !proceed {
		return status
	}
	switch arguments[0] {
	case "schema":
		return inv.printConfigSchema(arguments[1:])
	case "validate":
		return inv.validateConfig(arguments[1:])
	default:
		return inv.fail(usageError("unknown config subcommand %q; want schema or validate", arguments[0]))
	}
}

// printConfigSchema prints the configuration JSON Schema.
//
// Takes arguments ([]string) which must hold no arguments.
//
// Returns int which is the exit status.
func (inv *invocation) printConfigSchema(arguments []string) int {
	if status, proceed := inv.parseNoArguments("config schema", arguments); !proceed {
		return status
	}
	_, _ = inv.streams.Stdout.Write(config.JSONSchema())
	return exitOK
}

// validateConfig loads a configuration file and checks it against this binary's dialects
// and drivers.
//
// Takes arguments ([]string) which holds an optional path; the default is -c or the file
// found by searching upwards.
//
// Returns int which is the exit status: 2 when the file is invalid.
func (inv *invocation) validateConfig(arguments []string) int {
	flags := inv.newFlags("config validate", "config validate [path]")
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status
	}
	if len(positional) > 1 {
		return inv.fail(usageError("config validate takes at most one path"))
	}
	if len(positional) == 1 {
		inv.configPath = positional[0]
	}
	loaded, err := inv.loadProject("")
	if err != nil {
		return inv.fail(err)
	}
	fmt.Fprintf(inv.streams.Stdout, "%s is valid (%d database(s))\n", inv.display(loaded.path), len(loaded.databases))
	return exitOK
}
