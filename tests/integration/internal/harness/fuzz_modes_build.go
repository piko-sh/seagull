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

package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/internal/testkit/fuzzruntime"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_clickhouse"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
)

const (
	// modeSQL runs the database/sql emitter with direct statements.
	modeSQL = "sql"

	// modePrepared runs the database/sql emitter with prepared statements.
	modePrepared = "prepared"

	// modeNative runs the pgx or native ClickHouse emitter.
	modeNative = "native"

	// modePackageSuffix names each mode's generated package.
	modePackageSuffix = "mode"
)

// modePackage holds one mode's generated files.
type modePackage struct {
	// mode names the execution mode.
	mode string

	// files holds the generated package.
	files []emit.GeneratedFile
}

// executionModes lists the modes an engine's suite can run, database/sql first.
//
// Takes engine (string) which selects prepared and native support.
//
// Returns []string which always starts with the direct database/sql mode.
func executionModes(engine string) []string {
	modes := []string{modeSQL}
	if fuzzcase.RuntimePrepared(engine) {
		modes = append(modes, modePrepared)
	}
	if fuzzcase.RuntimeNative(engine) {
		modes = append(modes, modeNative)
	}
	return modes
}

// modeEmitter constructs the emitter of one mode.
//
// Takes suite (NativeFuzzSuite) which supplies the engine and driver module.
// Takes mode (string) which names the execution mode.
//
// Returns emit.EmitterPort which generates that mode's package.
func modeEmitter(suite NativeFuzzSuite, mode string) emit.EmitterPort {
	switch {
	case mode == modeNative && suite.Engine == engineClickHouse:
		return emit_driver_clickhouse.New()
	case mode == modeNative:
		return emit_driver_pgx.New()
	default:
		return emit_driver_databasesql.New(emit_driver_databasesql.WithDriver(suite.Driver.Module), emit_driver_databasesql.WithPrepared(mode == modePrepared))
	}
}

// generateMode generates one mode's package from migrations and queries.
//
// Takes suite (NativeFuzzSuite) which supplies the dialect and driver.
// Takes mode (string) which names the execution mode.
// Takes migrations ([]string) which holds the schema statements.
// Takes queries (string) which holds the annotated query file.
//
// Returns []emit.GeneratedFile which is the generated package.
// Returns error when Seagull refuses the input or generation fails.
func generateMode(ctx context.Context, suite NativeFuzzSuite, mode string, migrations []string, queries string) ([]emit.GeneratedFile, error) {
	generator, err := seagull.New(suite.Dialect(), seagull.WithEmitter(modeEmitter(suite, mode)))
	if err != nil {
		return nil, err
	}
	files := fstest.MapFS{
		"migrations/001_modes.up.sql": &fstest.MapFile{Data: []byte(strings.Join(migrations, "\n"))},
		"queries/modes.sql":           &fstest.MapFile{Data: []byte(queries)},
	}
	details, err := generator.GenerateDetailed(ctx, seagull.Input{Package: mode + modePackageSuffix, PackagePath: "", Queries: nil,
		Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	if err != nil {
		return nil, err
	}
	if err := details.Result.Err(); err != nil {
		return nil, err
	}
	return details.Result.Files, nil
}

// buildModeProgram compiles one helper holding every mode's generated package.
//
// Takes t (*testing.T) which reports build failures.
// Takes suite (NativeFuzzSuite) which supplies the driver and build environment.
// Takes cache (*fuzzruntime.ProgramCache) which retains compiled helpers.
// Takes packages ([]modePackage) which holds each mode's generated files.
//
// Returns string which names a verified cached executable.
func buildModeProgram(t *testing.T, suite NativeFuzzSuite, cache *fuzzruntime.ProgramCache, packages []modePackage) string {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), nativeCompileBound, errNativeCompileDeadline)
	defer cancel()
	directory := t.TempDir()
	var all []emit.GeneratedFile
	for _, item := range packages {
		for _, file := range item.files {
			require.NoError(t, os.MkdirAll(filepath.Join(directory, item.mode+modePackageSuffix), directoryMode))
			writeFile(t, filepath.Join(directory, item.mode+modePackageSuffix, file.Name), file.Content)
			all = append(all, emit.GeneratedFile{Name: item.mode + "/" + file.Name, Content: file.Content})
		}
	}
	return compileHelper(ctx, t, suite, cache, &helperProgram{directory: directory, name: "modes-program", main: modeMain(suite, packages), files: all, sources: nil})
}

// modeMain writes a helper serving every compiled mode through its real driver API.
//
// Takes suite (NativeFuzzSuite) which supplies the engine and driver name.
// Takes packages ([]modePackage) which names the compiled modes.
//
// Returns string which is the helper's main file.
func modeMain(suite NativeFuzzSuite, packages []modePackage) string {
	imports := []string{`"context"`, `"database/sql"`, `"errors"`, `"fmt"`, `"os"`, `"seagull.sh/seagull/internal/testkit/fuzznative"`}
	var factories strings.Builder
	for _, item := range packages {
		name := item.mode + modePackageSuffix
		imports = append(imports, fmt.Sprintf("%s %q", name, nativeModule+"/"+name))
		factory := ""
		switch item.mode {
		case modeNative:
			factory = nativePGXFactory
			if suite.Engine == engineClickHouse {
				factory = nativeClickHouseFactory
				imports = append(imports, `"github.com/ClickHouse/clickhouse-go/v2"`)
			} else {
				imports = append(imports, `"github.com/jackc/pgx/v5"`)
			}
		default:
			factory = nativeSQLFactory(suite.Engine, suite.Driver.Name, item.mode == modePrepared)
		}
		fmt.Fprintf(&factories, " %q: %s,\n", item.mode, strings.ReplaceAll(factory, "generated.", name+"."))
	}
	return fmt.Sprintf(`package main
import (
 %s
)
var _ = sql.Open
var _ = errors.Join
var _ context.Context
func main() {
 factories := map[string]fuzznative.Factory{
%s }
 if err := fuzznative.ServeModes(%q, factories, os.Stdin, os.Stdout); err != nil {
  fmt.Fprintln(os.Stderr, err)
  os.Exit(1)
 }
}
`, strings.Join(imports, "\n "), factories.String(), suite.Engine)
}
