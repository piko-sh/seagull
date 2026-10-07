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

package directive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestParseGoTypeOverrideAcceptsWhatTheGeneratorWrites(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		raw  string
		want sqlmodel.GoType
	}{
		{raw: "string", want: sqlmodel.GoType{Package: "", Name: "string"}},
		{raw: "any", want: sqlmodel.GoType{Package: "", Name: "any"}},
		{raw: "Status", want: sqlmodel.GoType{Package: "", Name: "Status"}},
		{raw: "[]byte", want: sqlmodel.GoType{Package: "", Name: "[]byte"}},
		{raw: "*string", want: sqlmodel.GoType{Package: "", Name: "*string"}},
		{raw: "[]*int64", want: sqlmodel.GoType{Package: "", Name: "[]*int64"}},
		{raw: " time.Time ", want: sqlmodel.GoType{Package: "time", Name: "Time"}},
		{raw: "encoding/json.RawMessage", want: sqlmodel.GoType{Package: "encoding/json", Name: "RawMessage"}},
		{raw: "github.com/google/uuid.UUID", want: sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"}},
		{raw: "github.com/cockroachdb/apd/v3.Decimal", want: sqlmodel.GoType{Package: "github.com/cockroachdb/apd/v3", Name: "Decimal"}},
		{raw: "example.com/menu_items.Status", want: sqlmodel.GoType{Package: "example.com/menu_items", Name: "Status"}},
		{raw: "*example.com/menu.Pence", want: sqlmodel.GoType{Package: "example.com/menu", Name: "*Pence"}},
		{raw: "[]example.com/menu.Pence", want: sqlmodel.GoType{Package: "example.com/menu", Name: "[]Pence"}},
		{raw: "example.com/my-menu.Status", want: sqlmodel.GoType{Package: "example.com/my-menu", Name: "Status"}},
		{raw: "gopkg.in/guregu/null.v4.String", want: sqlmodel.GoType{Package: "gopkg.in/guregu/null.v4", Name: "String"}},
		{raw: "github.com/twpayne/go-geom.Point", want: sqlmodel.GoType{Package: "github.com/twpayne/go-geom", Name: "Point"}},
		{raw: "k8s.io/api/core/v1.Pod", want: sqlmodel.GoType{Package: "k8s.io/api/core/v1", Name: "Pod"}},
		{raw: "uuid.UUID", want: sqlmodel.GoType{Package: "uuid", Name: "UUID"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.raw, func(t *testing.T) {
			t.Parallel()

			goType, problem := ParseGoTypeOverride(testCase.raw)
			require.Empty(t, problem)
			require.NotNil(t, goType)
			require.Equal(t, testCase.want, *goType)
		})
	}
}

func TestParseGoTypeOverrideRejectsWhatTheGeneratorCannotWrite(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		raw         string
		wantProblem string
	}{
		{raw: "", wantProblem: "names no type"},
		{raw: "   ", wantProblem: "names no type"},
		{raw: "*", wantProblem: "names no type"},
		{raw: "[]", wantProblem: "names no type"},
		{raw: "bad type", wantProblem: `"bad type" is not a Go type name`},
		{raw: "not a type", wantProblem: `"not a type" is not a Go type name`},
		{raw: "map[string]int", wantProblem: `"map[string]int" is not a Go type name`},
		{raw: "chan int", wantProblem: "is not a Go type name"},
		{raw: "func()", wantProblem: "is not a Go type name"},
		{raw: "interface{}", wantProblem: "is not a Go type name"},
		{raw: "[4]byte", wantProblem: "is not a Go type name"},
		{raw: "Box[int", wantProblem: "is not a Go type name"},
		{raw: "example.com/menu.Status[.Code]", wantProblem: "names no package"},
		{raw: "type", wantProblem: `"type" is not a Go type name`},
		{raw: "1abc", wantProblem: "is not a Go type name"},
		{raw: "example.com/menu.123", wantProblem: `"123" is not a Go type name`},
		{raw: "example.com/menu.Status[int]]", wantProblem: "is not a Go type name"},
		{raw: "example.com/menu.status", wantProblem: `"status" is not exported`},
		{raw: "example.com/menu", wantProblem: "is not a Go type name"},
		{raw: ".UUID", wantProblem: "names no package"},
		{raw: "uuid.", wantProblem: "names no type"},
		{raw: "example.com//menu.Status", wantProblem: `"example.com//menu" is not a valid import path`},
		{raw: "example.com/menu/.Status", wantProblem: "is not a valid import path"},
		{raw: "/menu.Status", wantProblem: "is not a valid import path"},
		{raw: "example.com/../menu.Status", wantProblem: "is not a valid import path"},
		{raw: "example com/menu.Status", wantProblem: "is not a valid import path"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.raw, func(t *testing.T) {
			t.Parallel()

			goType, problem := ParseGoTypeOverride(testCase.raw)
			require.Nil(t, goType)
			require.Contains(t, problem, testCase.wantProblem)
		})
	}
}

func TestParseGoTypeOverrideReadsAnInstanceOfAGenericType(t *testing.T) {
	t.Parallel()

	option := sqlmodel.GoType{Package: "github.com/x/opt", Name: "Option"}
	identifier := sqlmodel.GoType{Package: "uuid", Name: "UUID"}
	tests := []struct {
		raw  string
		want sqlmodel.GoType
	}{
		{raw: "github.com/x/opt.Option[uuid.UUID]", want: sqlmodel.NewGoInstance(option, identifier)},
		{raw: "github.com/x/opt.Option[github.com/x/opt.Option[int]]", want: sqlmodel.NewGoInstance(option, sqlmodel.NewGoInstance(option, sqlmodel.GoType{Name: "int"}))},
		{raw: "Box[int, string]", want: sqlmodel.NewGoInstance(sqlmodel.GoType{Name: "Box"}, sqlmodel.GoType{Name: "int"}, sqlmodel.GoType{Name: "string"})},
	}
	for _, testCase := range tests {
		t.Run(testCase.raw, func(t *testing.T) {
			t.Parallel()

			goType, problem := ParseGoTypeOverride(testCase.raw)
			require.Empty(t, problem)
			require.NotNil(t, goType)
			require.Equal(t, testCase.want, *goType)
		})
	}
}

func TestCheckGoTypeOverride(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		goType      sqlmodel.GoType
		wantProblem string
	}{
		{name: "built-in", goType: sqlmodel.GoType{Package: "", Name: "string"}},
		{name: "byte slice", goType: sqlmodel.GoType{Package: "", Name: "[]byte"}},
		{name: "qualified", goType: sqlmodel.GoType{Package: "example.com/app/text", Name: "Text"}},
		{name: "qualified pointer", goType: sqlmodel.GoType{Package: "example.com/app/text", Name: "*Text"}},
		{name: "no name", goType: sqlmodel.GoType{Package: "example.com/app/text", Name: ""}, wantProblem: "names no type"},
		{name: "spaced name", goType: sqlmodel.GoType{Package: "", Name: "bad type"}, wantProblem: "is not a Go type name"},
		{name: "map", goType: sqlmodel.GoType{Package: "", Name: "map[string]int"}, wantProblem: "is not a Go type name"},
		{name: "numeric name", goType: sqlmodel.GoType{Package: "example.com/menu", Name: "123"}, wantProblem: "is not a Go type name"},
		{name: "unexported", goType: sqlmodel.GoType{Package: "example.com/menu", Name: "status"}, wantProblem: "is not exported"},
		{name: "bad path", goType: sqlmodel.GoType{Package: "example.com/my menu", Name: "Status"}, wantProblem: "is not a valid import path"},
		{name: "the standard library's UUID", goType: sqlmodel.GoType{Package: "uuid", Name: "UUID"}},
		{name: "an instance", goType: sqlmodel.GoType{Package: "github.com/x/opt", Name: "Option[uuid.UUID]"}},
		{name: "an instance of an unexported argument", goType: sqlmodel.GoType{Package: "github.com/x/opt", Name: "Option[example.com/y.z]"}, wantProblem: `"z" is not exported`},
		{name: "an instance with an empty argument", goType: sqlmodel.GoType{Package: "github.com/x/opt", Name: "Option[]"}, wantProblem: "names no type"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problem := CheckGoTypeOverride(testCase.goType)
			if testCase.wantProblem == "" {
				require.Empty(t, problem)
				return
			}
			require.Contains(t, problem, testCase.wantProblem)
		})
	}
}

func TestDirectiveParser_ColumnOverrideUnusableGoType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		goType      string
		wantMessage string
	}{
		{name: "spaced name", goType: "bad type", wantMessage: `"bad type" is not a Go type name`},
		{name: "map", goType: "map[string]int", wantMessage: `"map[string]int" is not a Go type name`},
		{name: "numeric name", goType: "example.com/menu.123", wantMessage: `"123" is not a Go type name`},
		{name: "a malformed argument", goType: "github.com/x/opt.Option[map[string]int]", wantMessage: `"map[string]int" is not a Go type name`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			line := `-- sgl.column(id, go_type: "` + testCase.goType + `")`
			block, diagnostics := parseSQL(t, newTestParser(), "-- sgl.query(GetUser, one)\n"+line+"\nSELECT 1;")

			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeUnusableGoTypeOverride, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Contains(t, diagnostics[0].Message, testCase.wantMessage)
			require.Contains(t, diagnostics[0].Message, `column "id"`)
			require.Equal(t, 2, diagnostics[0].Line)
			require.Equal(t, 2, diagnostics[0].EndLine)
			require.Equal(t, `"`+testCase.goType+`"`, line[diagnostics[0].Column-1:diagnostics[0].EndColumn-1])
			require.Len(t, block.ColumnOverrides, 1)
			require.Equal(t, testCase.goType, block.ColumnOverrides[0].GoType)
		})
	}
}

func nestedOption(depth int) string {
	return strings.Repeat("example.com/o.Option[", depth) + "int" + strings.Repeat("]", depth)
}

func TestParseGoTypeOverrideBoundsTheLengthAndNestingOfAValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		raw         string
		wantProblem string
	}{
		{name: "type arguments nested to the limit", raw: strings.Repeat("Option[", sqlmodel.MaxGoTypeArgumentDepth) + "int" + strings.Repeat("]", sqlmodel.MaxGoTypeArgumentDepth)},
		{
			name:        "type arguments nested past the limit",
			raw:         strings.Repeat("Option[", sqlmodel.MaxGoTypeArgumentDepth+1) + "int" + strings.Repeat("]", sqlmodel.MaxGoTypeArgumentDepth+1),
			wantProblem: "its type arguments nest more than 16 levels deep",
		},
		{name: "a value as long as the limit", raw: strings.Repeat("A", maxGoTypeLength)},
		{name: "a value one character too long", raw: strings.Repeat("A", maxGoTypeLength+1), wantProblem: "it is longer than the 1024 characters a go_type may hold"},
		{name: "a value of multi-byte characters as long as the limit", raw: strings.Repeat("Ü", maxGoTypeLength)},
		{name: "a deeply nested value", raw: nestedOption(4_000), wantProblem: "it is longer than the 1024 characters a go_type may hold"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			goType, problem := ParseGoTypeOverride(testCase.raw)
			if testCase.wantProblem == "" {
				require.Empty(t, problem)
				require.NotNil(t, goType)
				return
			}
			require.Nil(t, goType)
			require.Equal(t, testCase.wantProblem, problem)
			require.Equal(t, testCase.wantProblem, CheckGoTypeOverride(sqlmodel.GoType{Package: "", Name: strings.TrimSpace(testCase.raw)}))
		})
	}
}

func TestParseGoTypeOverrideRefusesADeeplyNestedValueQuickly(t *testing.T) {
	t.Parallel()

	raw := nestedOption(4_000)
	line := `-- sgl.column(id, go_type: "` + raw + `")`

	scaling.Within(t, 50*time.Millisecond, func(context.Context) {
		block, diagnostics := parseSQL(t, newTestParser(), "-- sgl.query(GetUser, one)\n"+line+"\nSELECT 1;")

		require.Len(t, diagnostics, 1)
		require.Equal(t, diag.CodeUnusableGoTypeOverride, diagnostics[0].Code)
		require.Less(t, len(diagnostics[0].Message), 2*maxGoTypeLength)
		require.Nil(t, block.ColumnOverrides[0].ParsedGoType)
	})
}

func TestDirectiveParser_ColumnOverrideKeepsTheParsedGoType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		written string
		want    *sqlmodel.GoType
	}{
		{name: "a usable type", written: `"github.com/google/uuid.UUID"`, want: &sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"}},
		{name: "an unquoted import path", written: "github.com/twpayne/go-geom.Point", want: &sqlmodel.GoType{Package: "github.com/twpayne/go-geom", Name: "Point"}},
		{name: "an unusable type", written: `"bad type"`, want: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			block, diagnostics := parseSQL(t, newTestParser(), "-- sgl.query(GetUser, one)\n-- sgl.column(id, go_type: "+testCase.written+")\nSELECT 1;")

			require.Len(t, block.ColumnOverrides, 1)
			require.Equal(t, testCase.want, block.ColumnOverrides[0].ParsedGoType)
			if testCase.want != nil {
				require.Empty(t, diagnostics)
			}
		})
	}
}
