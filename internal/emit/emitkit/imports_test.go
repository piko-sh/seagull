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

package emitkit_test

import (
	"go/format"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func plannedImports() *emit.Naming {
	return &emit.Naming{Imports: map[string]string{
		"github.com/shopspring/decimal": "decimal",
		"github.com/google/uuid":        "uuid2",
		"github.com/jackc/pgx/v5":       "pgx",
		"time":                          "time",
	}}
}

func TestFileImportsWritesATypeWithThePlansQualifiers(t *testing.T) {
	t.Parallel()

	decimal := sqlmodel.GoType{Package: "github.com/shopspring/decimal", Name: "Decimal"}
	text := sqlmodel.GoType{Name: "string"}
	tests := []struct {
		name     string
		goType   sqlmodel.GoType
		expected string
	}{
		{name: "a built-in type", goType: sqlmodel.GoType{Name: "int64"}, expected: "int64"},
		{name: "a pointer to a built-in type", goType: sqlmodel.GoType{Name: "*string"}, expected: "*string"},
		{name: "a type of a package", goType: decimal, expected: "decimal.Decimal"},
		{name: "a pointer to a type of a package", goType: sqlmodel.GoType{Package: "github.com/shopspring/decimal", Name: "*Decimal"}, expected: "*decimal.Decimal"},
		{name: "slices and pointers", goType: sqlmodel.GoType{Package: "time", Name: "[][]*Time"}, expected: "[][]*time.Time"},
		{name: "a qualifier the plan numbered", goType: sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"}, expected: "uuid2.UUID"},
		{name: "a map", goType: sqlmodel.NewGoMap(text, decimal), expected: "map[string]decimal.Decimal"},
		{
			name:     "a slice of a struct",
			goType:   sliceOf(sqlmodel.NewGoStruct([]sqlmodel.GoField{{Name: "Price", Type: decimal}, {Name: "Label", Tag: `json:"label"`, Type: text}})),
			expected: "[]struct { Price decimal.Decimal; Label string `json:\"label\"` }",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.expected, emitkit.NewFileImports(plannedImports()).Type(test.goType))
		})
	}
}

func TestFileImportsDeclaresTheImportsAFileUses(t *testing.T) {
	t.Parallel()

	imports := emitkit.NewFileImports(plannedImports())

	require.Empty(t, imports.Declaration())
	imports.Type(sqlmodel.GoType{Package: "time", Name: "Time"})
	imports.Type(sqlmodel.GoType{Package: "github.com/google/uuid", Name: "*UUID"})
	require.Equal(t, "pgx", imports.Qualifier("github.com/jackc/pgx/v5"))
	require.Equal(t, "sql", imports.Qualifier("database/sql"))
	imports.Type(sqlmodel.GoType{Package: "time", Name: "Duration"})

	declaration := imports.Declaration()
	require.Equal(t, "import (\n\t\"database/sql\"\n\tuuid2 \"github.com/google/uuid\"\n\t\"github.com/jackc/pgx/v5\"\n\t\"time\"\n)\n", declaration)
	_, err := format.Source([]byte("package store\n\n" + declaration))
	require.NoError(t, err)
}

func sliceOf(goType sqlmodel.GoType) sqlmodel.GoType {
	goType.Name = "[]"
	return goType
}

func TestFileImportsNumbersAnUnplannedQualifierPastEveryOtherOne(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		importPaths []string
		want        []string
	}{
		{
			name:        "an unplanned path whose name the plan holds",
			importPaths: []string{"example.com/money/decimal"},
			want:        []string{"decimal2"},
		},
		{
			name:        "two unplanned paths of one name",
			importPaths: []string{"example.com/first/ledger", "example.com/second/ledger"},
			want:        []string{"ledger", "ledger2"},
		},
		{
			name:        "an unplanned path asked for twice",
			importPaths: []string{"example.com/first/ledger", "example.com/first/ledger"},
			want:        []string{"ledger", "ledger"},
		},
		{
			name:        "a planned path keeps its planned qualifier",
			importPaths: []string{"example.com/money/decimal", "github.com/shopspring/decimal"},
			want:        []string{"decimal2", "decimal"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			imports := emitkit.NewFileImports(plannedImports())
			qualifiers := make([]string, 0, len(testCase.importPaths))
			for _, importPath := range testCase.importPaths {
				qualifiers = append(qualifiers, imports.Qualifier(importPath))
			}

			require.Equal(t, testCase.want, qualifiers)
		})
	}
}
