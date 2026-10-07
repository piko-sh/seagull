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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/cmd/internal/starter"
)

// defaultInitDialect is the dialect sgl init configures when --dialect is absent.
const defaultInitDialect = "postgres"

// runInit writes sgl.yaml, a first migration, an example query file and a seed file.
//
// Takes arguments ([]string) which holds the flags.
//
// Returns int which is the exit status: 2 when a file already exists.
func (inv *invocation) runInit(_ context.Context, arguments []string) int {
	flags := inv.newFlags("init", "init [--dialect name]")
	dialectName := flags.String("dialect", defaultInitDialect, fmt.Sprintf("the database dialect `name`; %q lists them", inv.program+" dialects"))
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status
	}
	if len(positional) > 0 {
		return inv.fail(usageError("init takes no arguments; got %q", positional[0]))
	}

	probe := &config.Database{}
	probe.Name, probe.Dialect = "app", *dialectName
	built, err := inv.buildDialect(probe)
	if err != nil {
		return inv.fail(err)
	}
	files, err := starter.Files(*dialectName, built.Info().Family)
	if errors.Is(err, starter.ErrNoStarter) {
		return inv.fail(usageError("%s init has no starter files for dialect %s", inv.program, *dialectName))
	}
	if err != nil {
		return inv.fail(err)
	}

	for _, file := range files {
		if _, statErr := os.Stat(inv.path(file.Path)); statErr == nil {
			return inv.fail(usageError("%s already exists; %s init never overwrites a file", file.Path, inv.program))
		}
	}
	for _, file := range files {
		if err := inv.writeInitFile(file.Path, file.Content); err != nil {
			return inv.fail(err)
		}
		fmt.Fprintf(inv.streams.Stdout, "created %s\n", file.Path)
	}
	fmt.Fprintf(inv.streams.Stdout, "\nNext: edit the migration and queries, then run `%s generate`.\n", inv.program)
	return exitOK
}

// writeInitFile writes one starter file, creating its directory.
//
// Takes name (string) which is slash-separated and relative to the working directory.
// Takes content (string) which is written.
//
// Returns error when the file exists or cannot be written.
func (inv *invocation) writeInitFile(name, content string) error {
	path := inv.path(filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), directoryPermissions); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(name), err)
	}
	return inv.writeNewFile(path, content)
}
