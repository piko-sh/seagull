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

package facade_test

import (
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
)

type goTypeSource struct {
	name     string
	build    func(goType string) (fstest.MapFS, []seagull.Option)
	wantFile string
	wantLine int
}

var goTypeSources = []goTypeSource{
	{
		name: "migration column directive",
		build: func(goType string) (fstest.MapFS, []seagull.Option) {
			project := tasksProject()
			project["db/migrations/0002_notes.up.sql"] = &fstest.MapFile{Data: []byte(
				"CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL);\n" +
					"-- sgl.column(notes.body, go_type: \"" + goType + "\")\n")}
			project["db/queries/notes.sql"] = &fstest.MapFile{Data: []byte(
				"-- sgl.query(ListNotes, many)\nSELECT id, body FROM notes;\n")}
			return project, nil
		},
		wantFile: "0002_notes.up.sql",
		wantLine: 2,
	},
	{
		name: "query column directive",
		build: func(goType string) (fstest.MapFS, []seagull.Option) {
			project := tasksProject()
			project["db/queries/titles.sql"] = &fstest.MapFile{Data: []byte(
				"-- sgl.query(ListTitles, many)\n-- sgl.column(title, go_type: \"" + goType + "\")\nSELECT id, title FROM tasks;\n")}
			return project, nil
		},
		wantFile: "titles.sql",
		wantLine: 2,
	},
	{
		name: "column type override",
		build: func(goType string) (fstest.MapFS, []seagull.Option) {
			goPackage, goName := splitGoType(goType)
			return tasksProject(), []seagull.Option{seagull.WithTypeOverrides(
				seagull.TypeOverride{Column: "tasks.title", GoPackage: goPackage, GoName: goName},
			)}
		},
	},
	{
		name: "SQL type override",
		build: func(goType string) (fstest.MapFS, []seagull.Option) {
			goPackage, goName := splitGoType(goType)
			return tasksProject(), []seagull.Option{seagull.WithTypeOverrides(
				seagull.TypeOverride{SQLTypeName: "text", GoPackage: goPackage, GoName: goName},
			)}
		},
	},
}

var arrayOverrideProject = fstest.MapFS{
	"db/migrations/0001_items.up.sql": &fstest.MapFile{Data: []byte(
		"CREATE TABLE items (id BIGINT PRIMARY KEY, amounts numeric[] NOT NULL, tags text[]);\n")},
	"db/queries/items.sql": &fstest.MapFile{Data: []byte(
		"-- sgl.query(GetItem, one)\nSELECT * FROM items WHERE id = $1;\n\n" +
			"-- sgl.query(SetAmounts, exec)\nUPDATE items SET amounts = $1 WHERE id = $2;\n\n" +
			"-- sgl.query(GetTags, one)\n-- sgl.column(tags, go_type: \"[]*string\")\nSELECT tags FROM items WHERE id = $1;\n")},
}

var quantifiedOverrideProject = fstest.MapFS{
	"db/migrations/0001_items.up.sql": &fstest.MapFile{Data: []byte(
		"CREATE TABLE items (id BIGINT PRIMARY KEY, code TEXT NOT NULL);\n")},
	"db/queries/items.sql": &fstest.MapFile{Data: []byte(
		"-- sgl.query(ItemsWithCodes, many)\nSELECT id FROM items WHERE code = ANY($1);\n\n" +
			"-- sgl.query(ItemsWithoutCodes, many)\nSELECT id FROM items WHERE code <> ALL($1::text[]);\n\n" +
			"-- sgl.query(ItemWithCode, one)\nSELECT id FROM items WHERE code = $1;\n")},
}

func splitGoType(goType string) (string, string) {
	head, arguments := goType, ""
	if before, after, found := strings.Cut(goType, "["); found && before != "" && strings.HasSuffix(goType, "]") {
		head, arguments = before, "["+after
	}
	goPackage, goName, qualified := strings.CutLast(head, ".")
	if !qualified {
		return "", goType
	}
	return goPackage, goName + arguments
}

func requireUnusableGoType(t *testing.T, diagnostics []seagull.SourceError, source goTypeSource, goType string) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Code != seagull.CodeUnusableGoTypeOverride {
			continue
		}
		require.Equal(t, seagull.SeverityError, diagnostic.Severity)
		require.Contains(t, diagnostic.Message, goType)
		if source.wantFile != "" {
			require.True(t, strings.HasSuffix(diagnostic.Filename, source.wantFile), "%s is in %s", diagnostic.Filename, source.wantFile)
			require.Equal(t, source.wantLine, diagnostic.Line)
		}
		return
	}
	require.Failf(t, "no Q049 diagnostic", "%v", diagnostics)
}

func TestUnusableGoTypesAreReportedInVetAndGenerate(t *testing.T) {
	t.Parallel()

	for _, source := range goTypeSources {
		for _, goType := range []string{"bad type", "not a type", "map[string]int", "example.com/menu.123"} {
			t.Run(source.name+"/"+goType, func(t *testing.T) {
				t.Parallel()

				project, options := source.build(goType)
				input := seagull.Input{Package: "store", Migrations: project, MigrationDir: "db/migrations", QueryDir: "db/queries"}
				options = append(options, seagull.WithLogger(slog.New(slog.DiscardHandler)))
				generator, err := seagull.New(dialect_engine_sqlite.New(), options...)
				require.NoError(t, err)

				report, err := generator.Vet(t.Context(), input)
				require.NoError(t, err)
				require.Error(t, report.Err())
				requireUnusableGoType(t, report.Diagnostics, source, goType)

				result, err := generator.Generate(t.Context(), input)
				require.NoError(t, err)
				require.Error(t, result.Err())
				require.Empty(t, result.Files)
				requireUnusableGoType(t, result.Diagnostics, source, goType)
			})
		}
	}
}

func generatedSource(t *testing.T, result *seagull.Result) string {
	t.Helper()

	var builder strings.Builder
	for _, file := range result.Files {
		builder.Write(file.Content)
	}
	return builder.String()
}

func TestOverrideTypesAreImportedUnderTheNameTheirPathImplies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goType     string
		wantImport string
		wantType   string
	}{
		{goType: "gopkg.in/guregu/null.v4.String", wantImport: `"gopkg.in/guregu/null.v4"`, wantType: "null.String"},
		{goType: "github.com/twpayne/go-geom.Point", wantImport: `"github.com/twpayne/go-geom"`, wantType: "geom.Point"},
		{goType: "github.com/kevinburke/go-types.PrefixUUID", wantImport: `"github.com/kevinburke/go-types"`, wantType: "types.PrefixUUID"},
	}
	for _, source := range goTypeSources {
		for _, testCase := range tests {
			t.Run(source.name+"/"+testCase.goType, func(t *testing.T) {
				t.Parallel()

				project, options := source.build(testCase.goType)
				input := seagull.Input{Package: "store", Migrations: project, MigrationDir: "db/migrations", QueryDir: "db/queries"}
				options = append(options, seagull.WithLogger(slog.New(slog.DiscardHandler)))
				generator, err := seagull.New(dialect_engine_sqlite.New(), options...)
				require.NoError(t, err)

				result, err := generator.Generate(t.Context(), input)
				require.NoError(t, err)
				require.NoError(t, result.Err())
				source := generatedSource(t, result)
				require.Contains(t, source, testCase.wantImport)
				require.Contains(t, source, testCase.wantType)
			})
		}
	}
}

func TestPackageNamesNameTheImportOfAPackageWhosePathDoesNotImplyIt(t *testing.T) {
	t.Parallel()

	for _, source := range goTypeSources {
		t.Run(source.name, func(t *testing.T) {
			t.Parallel()

			project, options := source.build("example.com/app/pkgdir.Code")
			input := seagull.Input{Package: "store", Migrations: project, MigrationDir: "db/migrations", QueryDir: "db/queries"}
			options = append(options,
				seagull.WithLogger(slog.New(slog.DiscardHandler)),
				seagull.WithPackageNames(map[string]string{"example.com/app/pkgdir": "otherpkg"}),
			)
			generator, err := seagull.New(dialect_engine_sqlite.New(), options...)
			require.NoError(t, err)

			result, err := generator.Generate(t.Context(), input)
			require.NoError(t, err)
			require.NoError(t, result.Err())
			generated := generatedSource(t, result)
			require.Contains(t, generated, `otherpkg "example.com/app/pkgdir"`)
			require.Contains(t, generated, "otherpkg.Code")
			require.NotContains(t, generated, "pkgdir.Code")
		})
	}
}

func TestPackageNamesThatCannotBeUsedAreRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		names map[string]string
	}{
		{name: "a malformed path", names: map[string]string{"example.com//pkgdir": "otherpkg"}},
		{name: "a name that is not an identifier", names: map[string]string{"example.com/app/pkgdir": "other-pkg"}},
		{name: "the blank identifier", names: map[string]string{"example.com/app/pkgdir": "_"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := seagull.New(dialect_engine_sqlite.New(), seagull.WithPackageNames(testCase.names))
			require.ErrorIs(t, err, seagull.ErrInvalidConfig)
		})
	}
}

func TestOverrideTypesOfTheGeneratedPackageAreNotImported(t *testing.T) {
	t.Parallel()

	for _, source := range goTypeSources {
		t.Run(source.name, func(t *testing.T) {
			t.Parallel()

			project, options := source.build("example.com/app/store.Label")
			input := seagull.Input{Package: "store", PackagePath: "example.com/app/store", Migrations: project, MigrationDir: "db/migrations", QueryDir: "db/queries"}
			options = append(options, seagull.WithLogger(slog.New(slog.DiscardHandler)))
			generator, err := seagull.New(dialect_engine_sqlite.New(), options...)
			require.NoError(t, err)

			result, err := generator.Generate(t.Context(), input)
			require.NoError(t, err)
			require.NoError(t, result.Err())
			generated := generatedSource(t, result)
			require.NotContains(t, generated, `"example.com/app/store"`)
			require.NotContains(t, generated, "store.Label")
			require.Contains(t, generated, " Label")
		})
	}
}

func TestOverridesOfArrayColumnsNameTheirElementsEverywhere(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		emitter   emit.EmitterPort
		wantReads []string
	}{
		{name: "database/sql", emitter: emit_driver_databasesql.New(), wantReads: []string{
			`to_json("items"."amounts") AS "amounts"`, "sglScanInto(&row.Amounts)", "sglScanInto(&row.Tags)"}},
		{name: "pgx", emitter: emit_driver_pgx.New(), wantReads: []string{"&row.Amounts", "&row.Tags"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			generator, err := seagull.New(dialect_engine_postgres.New(),
				seagull.WithEmitter(testCase.emitter),
				seagull.WithLogger(slog.New(slog.DiscardHandler)),
				seagull.WithTypeOverrides(seagull.TypeOverride{Column: "items.amounts", GoPackage: "github.com/shopspring/decimal", GoName: "Decimal"}),
			)
			require.NoError(t, err)
			result, err := generator.Generate(t.Context(), seagull.Input{
				Package: "store", Migrations: arrayOverrideProject, MigrationDir: "db/migrations", QueryDir: "db/queries",
			})
			require.NoError(t, err)
			require.NoError(t, result.Err())

			generated := generatedSource(t, result)
			require.Equal(t, 2, strings.Count(generated, "Amounts []*decimal.Decimal `json:\"amounts\"`"), "the model and the row reading the column")
			require.Contains(t, generated, "Amounts []*decimal.Decimal\n")
			require.Contains(t, generated, "Tags []*string `json:\"tags\"`")
			require.NotContains(t, generated, "*[]")
			for _, want := range testCase.wantReads {
				require.Contains(t, generated, want)
			}
		})
	}
}

func TestAQuantifiedComparisonBindsASliceOfTheOverrideOfTheColumnItReads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		emitter emit.EmitterPort
	}{
		{name: "database/sql", emitter: emit_driver_databasesql.New()},
		{name: "pgx", emitter: emit_driver_pgx.New()},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			generator, err := seagull.New(dialect_engine_postgres.New(),
				seagull.WithEmitter(testCase.emitter),
				seagull.WithLogger(slog.New(slog.DiscardHandler)),
				seagull.WithTypeOverrides(seagull.TypeOverride{Column: "items.code", GoPackage: "example.com/codes", GoName: "Code"}),
			)
			require.NoError(t, err)
			result, err := generator.Generate(t.Context(), seagull.Input{
				Package: "store", Migrations: quantifiedOverrideProject, MigrationDir: "db/migrations", QueryDir: "db/queries",
			})
			require.NoError(t, err)
			require.NoError(t, result.Err())

			generated := generatedSource(t, result)
			require.Contains(t, generated, "ItemsWithCodes(ctx context.Context, code []codes.Code)")
			require.Contains(t, generated, "ItemsWithoutCodes(ctx context.Context, code []codes.Code)")
			require.Contains(t, generated, "ItemWithCode(ctx context.Context, code codes.Code)")
			require.NotContains(t, generated, "code []string")
		})
	}
}

func TestOverrideTypesMayBeInstancesOfGenericTypesAndTheStandardUUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goType      string
		wantImports []string
		wantType    string
	}{
		{goType: "github.com/x/opt.Option[github.com/google/uuid.UUID]", wantImports: []string{`"github.com/x/opt"`, `"github.com/google/uuid"`}, wantType: "opt.Option[uuid.UUID]"},
		{goType: "example.com/pairs.Pair[string, []example.com/y.Z]", wantImports: []string{`"example.com/pairs"`, `"example.com/y"`}, wantType: "pairs.Pair[string, []y.Z]"},
		{goType: "uuid.UUID", wantImports: []string{`"uuid"`}, wantType: ".UUID"},
	}
	for _, source := range goTypeSources {
		for _, testCase := range tests {
			t.Run(source.name+"/"+testCase.goType, func(t *testing.T) {
				t.Parallel()

				project, options := source.build(testCase.goType)
				input := seagull.Input{Package: "store", Migrations: project, MigrationDir: "db/migrations", QueryDir: "db/queries"}
				options = append(options, seagull.WithLogger(slog.New(slog.DiscardHandler)))
				generator, err := seagull.New(dialect_engine_sqlite.New(), options...)
				require.NoError(t, err)

				result, err := generator.Generate(t.Context(), input)
				require.NoError(t, err)
				require.NoError(t, result.Err())
				generated := generatedSource(t, result)
				for _, want := range testCase.wantImports {
					require.Contains(t, generated, want)
				}
				require.Contains(t, generated, testCase.wantType)
			})
		}
	}
}

func TestTypeOverridesThatMatchNoTypeAreReported(t *testing.T) {
	t.Parallel()

	project := fstest.MapFS{
		"db/migrations/0001_items.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE items (id integer PRIMARY KEY, embedding vector(3), labels text[]);\n")},
		"db/queries/items.sql":            &fstest.MapFile{Data: []byte("-- sgl.query(GetItem, one)\nSELECT id, embedding, labels FROM items WHERE id = $1;\n")},
	}
	tests := []struct {
		name    string
		sqlType string
		want    bool
	}{
		{name: "a misspelt type", sqlType: "notatype", want: true},
		{name: "a schema-qualified built-in type", sqlType: "pg_catalog.int4", want: true},
		{name: "an array of a misspelt type", sqlType: "notatype[]", want: true},
		{name: "a built-in type", sqlType: "integer", want: false},
		{name: "an array type", sqlType: "text[]", want: false},
		{name: "a type the dialect does not know that a column has", sqlType: "vector", want: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			generator, err := seagull.New(dialect_engine_postgres.New(),
				seagull.WithLogger(slog.New(slog.DiscardHandler)),
				seagull.WithTypeOverrides(seagull.TypeOverride{SQLTypeName: testCase.sqlType, GoName: "string"}),
			)
			require.NoError(t, err)
			report, err := generator.Vet(t.Context(), seagull.Input{Migrations: project, MigrationDir: "db/migrations", QueryDir: "db/queries"})
			require.NoError(t, err)
			require.NoError(t, report.Err())
			var reported bool
			for _, diagnostic := range report.Diagnostics {
				if diagnostic.Code == seagull.CodeUnmatchedTypeOverride {
					reported = true
					require.Equal(t, seagull.SeverityWarning, diagnostic.Severity)
					require.Contains(t, diagnostic.Message, testCase.sqlType)
				}
			}
			require.Equal(t, testCase.want, reported, "%v", report.Diagnostics)
		})
	}
}
