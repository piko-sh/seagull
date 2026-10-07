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

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
	"seagull.sh/seagull/sdk/observe"
)

// traceLevel is the level below Debug that Seagull logs its internals at.
const traceLevel = slog.Level(-8)

// depthKey keys the nesting depth a stagePrinter keeps in a stage's context.
type depthKey struct{}

// stagePrinter is an observe.ObserverPort that prints each stage as it starts, indented
// by how deeply it nests, and each stage that fails as it ends. It prints no timings, so
// its output is the same on every run; a real observer would start a tracing span or
// record a metric here.
type stagePrinter struct {
	// output receives one line per stage.
	output io.Writer
}

// Start prints the stage and returns a context one level deeper, so the stages it starts
// are indented beneath it.
//
// Takes ctx (context.Context) which carries the depth of the enclosing stage.
// Takes stage (observe.Stage) which names the work.
// Takes attributes (...slog.Attr) which describe it.
//
// Returns context.Context which the stage runs under.
// Returns func(err error) which the stage calls once when it ends.
func (p stagePrinter) Start(ctx context.Context, stage observe.Stage, attributes ...slog.Attr) (context.Context, func(err error)) {
	depth, nested := ctx.Value(depthKey{}).(int)
	if !nested {
		depth = 0
	}
	indent := strings.Repeat("  ", depth)
	var line strings.Builder
	line.WriteString(indent + string(stage))
	for _, attribute := range attributes {
		line.WriteString(" " + attribute.String())
	}
	fmt.Fprintln(p.output, line.String())
	return context.WithValue(ctx, depthKey{}, depth+1), func(err error) {
		if err != nil {
			fmt.Fprintf(p.output, "%s%s failed: %v\n", indent, stage, err)
		}
	}
}

// fixedClock is the clock the generator times its runs with. A fixed time makes the
// elapsed time in the trace log zero on every run.
type fixedClock struct{}

// Now returns the same moment every time.
//
// Returns time.Time which is midnight on 1 January 2026.
func (fixedClock) Now() time.Time {
	return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
}

// run generates the store package and migrates the database with an observer and a logger
// attached, so each stage and the trace log of both show in output.
//
// Takes database (*sql.DB) which is an empty database to migrate.
// Takes project (fs.FS) which holds db/migrations and db/queries.
// Takes output (io.Writer) which receives the stages and the log.
//
// Returns error when generation or migration fails.
func run(ctx context.Context, database *sql.DB, project fs.FS, output io.Writer) error {
	observer := stagePrinter{output: output}
	logger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{
		AddSource:   false,
		Level:       traceLevel,
		ReplaceAttr: tidyRecord,
	}))

	generator, err := seagull.New(dialect_engine_sqlite.New(),
		seagull.WithObserver(observer),
		seagull.WithLogger(logger),
		seagull.WithClock(fixedClock{}),
	)
	if err != nil {
		return err
	}
	fmt.Fprintln(output, "generating:")
	result, err := generator.Generate(ctx, seagull.Input{
		Migrations:   project,
		Queries:      nil,
		Package:      "store",
		MigrationDir: "db/migrations",
		QueryDir:     "db/queries",
	})
	if err != nil {
		return err
	}
	if err := result.Err(); err != nil {
		return err
	}

	fmt.Fprintln(output, "\nmigrating:")
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), project, "db/migrations",
		migrate.WithObserver(observer),
		migrate.WithLogger(logger),
	)
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return err
	}
	_, err = migrator.Status(ctx)
	return err
}

// tidyRecord drops the time from every log record, which would differ on every run, and
// names Seagull's trace level, which slog would print as DEBUG-4.
//
// Takes groups ([]string) which are the groups the attribute is in.
// Takes attribute (slog.Attr) which is the attribute.
//
// Returns slog.Attr which is the attribute to print, or an empty one to drop it.
func tidyRecord(groups []string, attribute slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return attribute
	}
	switch attribute.Key {
	case slog.TimeKey:
		return slog.Attr{}
	case slog.LevelKey:
		if level, ok := attribute.Value.Any().(slog.Level); ok && level == traceLevel {
			return slog.String(slog.LevelKey, "TRACE")
		}
	}
	return attribute
}
