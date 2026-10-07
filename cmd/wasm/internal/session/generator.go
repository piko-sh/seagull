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

package session

import (
	"fmt"
	"io/fs"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/sdk/observe"
)

// generator is one database ready for generation: its generator and the input it reads.
type generator struct {
	// generator generates, vets and catalogues the database's SQL.
	generator *seagull.Generator

	// input names the migrations and queries within the in-memory file system.
	input seagull.Input
}

// newGenerator builds the generator for the project's database with the assembly sgl
// uses, adding the playground's stage observer and limits. The generator logs through the
// logger its run's context carries; the only record it writes at warning level or above
// is the stack of a panic it recovered.
//
// Takes resolved (*project) which holds the configuration.
// Takes filesystem (fs.FS) which holds the project's files.
// Takes observer (observe.ObserverPort) which receives the generator's stage spans.
//
// Returns *generator which is ready to run.
// Returns error when the dialect, driver, preset, overrides or limits cannot be used.
func newGenerator(resolved *project, filesystem fs.FS, observer observe.ObserverPort) (*generator, error) {
	database := resolved.database
	options, built, err := assemble.Options(resolved.loaded, database, assemble.Factories{Dialects: nil, Emitters: nil})
	if err != nil {
		return nil, err
	}
	options = append(options,
		seagull.WithObserver(observer),
		seagull.WithMaxFiles(MaxFiles),
		seagull.WithMaxFileBytes(MaxFileBytes),
		seagull.WithMaxQueries(maxQueries),
	)
	if resolved.skipFailingQueries {
		options = append(options, seagull.WithSkipFailingQueries())
	}
	created, err := seagull.New(built, options...)
	if err != nil {
		return nil, fmt.Errorf("database %q: %w", database.Name, err)
	}
	return &generator{
		generator: created,
		input: seagull.Input{
			Migrations:   filesystem,
			Queries:      filesystem,
			Package:      database.Go.Package,
			MigrationDir: resolved.migrations,
			QueryDir:     resolved.queries,
			PackagePath:  "",
		},
	}, nil
}
