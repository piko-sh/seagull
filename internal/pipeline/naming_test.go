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

package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/preset"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type reservingEmitter struct {
	fakedialect.Emitter
}

func (*reservingEmitter) Reserve(_ *dialect.Facts, _ *emit.Style) emit.Reservation {
	return emit.Reservation{Receiver: "queries", Files: []string{"otel.go"}, Declarations: []string{"Lookup"}}
}

type checkingEmitter struct {
	fakedialect.Emitter
	problems []emit.Problem
}

func (e *checkingEmitter) Check(request *emit.Request) []emit.Problem {
	for index := range e.problems {
		if e.problems[index].Query != nil {
			e.problems[index].Query = request.Queries[0]
		}
	}
	return e.problems
}

func moodType() sqlmodel.SQLType {
	mood := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood")
	mood.Schema = "public"
	mood.EnumValues = []string{"happy", "sad"}
	return mood
}

func namingCatalogue() *sqlmodel.Catalogue {
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Enums["mood"] = &sqlmodel.Enum{Name: "mood", Schema: "public", Values: []string{"happy", "sad"}}
	catalogue.Schemas["public"].Tables["people"] = &sqlmodel.Table{Name: "people", Schema: "public", Columns: []sqlmodel.Column{
		sqlmodel.NewColumn("id", sqlmodel.NewIntegerType("integer", 32, false), false),
		sqlmodel.NewColumn("mood", moodType(), true),
	}}
	return catalogue
}

func namingQueries() []*sqlmodel.AnalysedQuery {
	return []*sqlmodel.AnalysedQuery{
		{
			Name:          "ListPeople",
			Filename:      "people_linux.sql",
			Line:          3,
			Command:       sqlmodel.QueryCommandMany,
			Parameters:    []sqlmodel.QueryParameter{{Name: "rows", SQLType: sqlmodel.NewIntegerType("integer", 32, false)}},
			OutputColumns: []sqlmodel.OutputColumn{{Name: "mood", SQLType: moodType(), Nullable: true}},
		},
	}
}

func TestNewRejectsAQueryNamesFileOrFunctionTheEmitterUses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		emitter emit.EmitterPort
		names   emit.QueryNames
		message string
	}{
		{name: "the querier file", emitter: &fakedialect.Emitter{}, names: emit.QueryNames{Enabled: true, File: "querier.go", Function: "QueryName"}, message: `query-names file "querier.go"`},
		{name: "the constructor", emitter: &fakedialect.Emitter{}, names: emit.QueryNames{Enabled: true, File: "query_names.go", Function: "New"}, message: `query-names function "New"`},
		{name: "a file the emitter reserves", emitter: &reservingEmitter{}, names: emit.QueryNames{Enabled: true, File: "otel.go", Function: "QueryName"}, message: `query-names file "otel.go"`},
		{name: "a declaration the emitter reserves", emitter: &reservingEmitter{}, names: emit.QueryNames{Enabled: true, File: "names.go", Function: "Lookup"}, message: `query-names function "Lookup"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := preset.DefaultSettings()
			settings.Style.QueryNames = test.names

			generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: test.emitter, Settings: &settings})

			require.ErrorIs(t, err, fault.ErrInvalidConfig)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, generator)
		})
	}
}

func TestNewAcceptsAFreeQueryNamesFileAndFunction(t *testing.T) {
	t.Parallel()

	settings := preset.DefaultSettings()
	settings.Style.QueryNames = emit.QueryNames{Enabled: true, File: "otel.go", Function: "QueryNameResolver"}

	_, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: databasesql.New(), Settings: &settings})

	require.NoError(t, err)
}

func TestEmitCarriesTheNamingPlanToTheEmitter(t *testing.T) {
	t.Parallel()

	emitter := &fakedialect.Emitter{}
	generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: emitter})
	require.NoError(t, err)

	emitted, err := generator.emit(t.Context(), generatedPackage{name: "generated", path: ""}, namingCatalogue(), namingQueries(), newDiagnosticCollector(DefaultMaxDiagnostics), nil)

	require.NoError(t, err)
	require.Empty(t, emitted.diagnostics)
	require.Len(t, emitter.Requests, 1)
	naming := emitter.Requests[0].Naming
	require.Equal(t, map[string]string{"people_linux.sql": "people_linux_sql.sql.go"}, naming.Files)
	require.Equal(t, "context", naming.Imports["context"])
	require.Equal(t, []emit.ModelNaming{{Schema: "public", Relation: "people", Type: "People", Fields: []string{"ID", "Mood"}}}, naming.Models)
	require.Len(t, naming.Types, 1)
	require.Equal(t, "Mood", naming.Types[0].Name)
	require.Len(t, naming.Queries, 1)
	require.Equal(t, "ListPeople", naming.Queries[0].Method)
	require.Equal(t, "listPeopleSQL", naming.Queries[0].SQLConstant)
	require.Equal(t, []string{"rows2"}, naming.Queries[0].ParameterFields)
	require.Equal(t, map[string]string{namingQueries()[0].Name: "ListPeople"}, emitted.methods)
	require.Equal(t, sqlmodel.GoType{Name: "*Mood"}, emitter.Requests[0].Types.Resolve(moodType(), true))
}

func TestEmitKeepsTheEnumSlotsTypeWhenThePresetNamesOne(t *testing.T) {
	t.Parallel()

	settings := preset.DefaultSettings()
	settings.Types.Enum = sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Name: "string"}, Nullable: sqlmodel.GoType{Name: "*string"}}
	emitter := &fakedialect.Emitter{}
	generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: emitter, Settings: &settings})
	require.NoError(t, err)

	emitted, err := generator.emit(t.Context(), generatedPackage{name: "generated", path: ""}, namingCatalogue(), namingQueries(), newDiagnosticCollector(DefaultMaxDiagnostics), nil)

	require.NoError(t, err)
	require.Empty(t, emitted.diagnostics)
	require.Nil(t, emitter.Requests[0].Naming.Types)
	require.Equal(t, sqlmodel.GoType{Name: "*string"}, emitter.Requests[0].Types.Resolve(moodType(), true))
}

func TestEmitNamesEnumsOnceNamedTypesAreOn(t *testing.T) {
	t.Parallel()

	emitter := &fakedialect.Emitter{}
	generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: emitter})
	require.NoError(t, err)
	generator.namedTypes = true

	emitted, err := generator.emit(t.Context(), generatedPackage{name: "generated", path: ""}, namingCatalogue(), namingQueries(), newDiagnosticCollector(DefaultMaxDiagnostics), nil)

	require.NoError(t, err)
	require.Empty(t, emitted.diagnostics)
	naming := emitter.Requests[0].Naming
	require.Len(t, naming.Types, 1)
	require.Equal(t, "Mood", naming.Types[0].Name)
	require.Equal(t, []string{"MoodHappy", "MoodSad"}, naming.Types[0].Constants)
	require.Equal(t, sqlmodel.GoType{Name: "*Mood"}, emitter.Requests[0].Types.Resolve(moodType(), true))
}

func TestEmitReportsANameThatCannotBePlanned(t *testing.T) {
	t.Parallel()

	emitter := &fakedialect.Emitter{}
	generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: emitter})
	require.NoError(t, err)
	queries := namingQueries()
	queries[0].Name = "0people"

	emitted, err := generator.emit(t.Context(), generatedPackage{name: "generated", path: ""}, namingCatalogue(), queries, newDiagnosticCollector(DefaultMaxDiagnostics), nil)

	require.NoError(t, err)
	require.Nil(t, emitted.files)
	require.Nil(t, emitted.methods)
	require.Empty(t, emitter.Requests)
	require.Len(t, emitted.diagnostics, 1)
	require.Equal(t, "people_linux.sql", emitted.diagnostics[0].Filename)
	require.Equal(t, 3, emitted.diagnostics[0].Line)
	require.Equal(t, diag.CodeInvalidKeywordArgumentValue, emitted.diagnostics[0].Code)
	require.Equal(t, diag.SeverityError, emitted.diagnostics[0].Severity)
	require.Contains(t, emitted.diagnostics[0].Message, "cannot become an exported Go identifier")
}

func TestGenerateRefusesTwoGeneratedFilesOfOneName(t *testing.T) {
	t.Parallel()

	emitter := &fakedialect.Emitter{
		EmitFunc: func(_ context.Context, _ *emit.Request) ([]sqlmodel.GeneratedFile, error) {
			return []sqlmodel.GeneratedFile{{Name: "querier.go"}, {Name: "models.go"}, {Name: "querier.go"}}, nil
		},
	}
	source := &mockCatalogueProvider{}
	generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: emitter, BuildCatalogue: source.BuildCatalogue})
	require.NoError(t, err)

	generation, err := generator.Generate(t.Context(), testRequest{packageName: "testdb", queries: emptyQueries, queryDirectory: "queries"})
	files := generation.Files

	require.ErrorIs(t, err, errDuplicateGeneratedFile)
	require.ErrorContains(t, err, "querier.go")
	require.Nil(t, files)
}

func TestEmitReportsTheEmittersCheckerProblemsAsDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		problems []emit.Problem
		emits    bool
		filename string
		line     int
	}{
		{
			name:     "an error at a query stops emission",
			problems: []emit.Problem{{Query: &sqlmodel.AnalysedQuery{}, Code: diag.CodeEmitterUnsupported, Message: "command: execrows is not supported", Severity: diag.SeverityError}},
			emits:    false,
			filename: "people_linux.sql",
			line:     3,
		},
		{
			name:     "a warning about the request is reported and emission goes on",
			problems: []emit.Problem{{Query: nil, Code: diag.CodeEmitterUnsupported, Message: "prepared statements are skipped", Severity: diag.SeverityWarning}},
			emits:    true,
			filename: "",
			line:     0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			emitter := &checkingEmitter{problems: test.problems}
			generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: emitter})
			require.NoError(t, err)

			emitted, err := generator.emit(t.Context(), generatedPackage{name: "generated", path: ""}, namingCatalogue(), namingQueries(), newDiagnosticCollector(DefaultMaxDiagnostics), nil)

			require.NoError(t, err)
			require.Len(t, emitted.diagnostics, 1)
			require.Equal(t, test.filename, emitted.diagnostics[0].Filename)
			require.Equal(t, test.line, emitted.diagnostics[0].Line)
			require.Equal(t, diag.CodeEmitterUnsupported, emitted.diagnostics[0].Code)
			require.Equal(t, test.problems[0].Severity, emitted.diagnostics[0].Severity)
			require.Contains(t, emitted.diagnostics[0].Message, test.problems[0].Message)
			require.Equal(t, test.emits, len(emitter.Requests) == 1)
		})
	}
}

func TestNewRejectsAnInterfaceNameTheEmitterUses(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Queries", "New", "context"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			settings := preset.DefaultSettings()
			settings.Style.Interface = emit.Interface{Enabled: true, Name: name}

			generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: databasesql.New(), Settings: &settings})

			require.ErrorIs(t, err, fault.ErrInvalidConfig)
			require.ErrorContains(t, err, `the interface name "`+name+`"`)
			require.Nil(t, generator)
		})
	}
}

func TestNewAcceptsAFreeInterfaceName(t *testing.T) {
	t.Parallel()

	settings := preset.DefaultSettings()
	settings.Style.Interface = emit.Interface{Enabled: true, Name: "Querier"}

	_, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: databasesql.New(), Settings: &settings})

	require.NoError(t, err)
}
