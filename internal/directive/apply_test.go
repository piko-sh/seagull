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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/sqlmodel"
)

func makeTokenSpan(line, column int) diagbuild.TextSpan {
	return diagbuild.TextSpan{Line: line, Column: column, EndLine: line, EndColumn: column + 1}
}

func TestApplyTopCall_PositionalsFillNameAndCommand(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 5),
		CloseSpan: makeTokenSpan(1, 20),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "GetUser"}, Span: makeTokenSpan(1, 6)},
			{Value: syntax.ParsedValue{Raw: "one"}, Span: makeTokenSpan(1, 15)},
		},
	}
	result := &DirectiveBlock{}

	applyTopCall(result, call)

	require.NotNil(t, result.Name)
	assert.Equal(t, "GetUser", result.Name.Value)
	require.NotNil(t, result.Command)
	assert.Equal(t, sqlmodel.QueryCommandOne, result.Command.Command)
}

func TestApplyTopCall_KeywordsRouteToNameCommandAndMetadata(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 5),
		CloseSpan: makeTokenSpan(1, 50),
		Closed:    true,
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "name", Value: syntax.ParsedValue{Raw: "GetUser"}, Span: makeTokenSpan(1, 6), KeySpan: makeTokenSpan(1, 6), ValueSpan: makeTokenSpan(1, 12)},
			{Key: "command", Value: syntax.ParsedValue{Raw: "many"}, Span: makeTokenSpan(1, 21), KeySpan: makeTokenSpan(1, 21), ValueSpan: makeTokenSpan(1, 30)},
			{Key: "group_by", Value: syntax.ParsedValue{Raw: "orders.id"}, Span: makeTokenSpan(1, 35), KeySpan: makeTokenSpan(1, 35), ValueSpan: makeTokenSpan(1, 45)},
		},
	}
	result := &DirectiveBlock{}

	applyTopCall(result, call)

	require.NotNil(t, result.Name)
	assert.Equal(t, "GetUser", result.Name.Value)
	require.NotNil(t, result.Command)
	assert.Equal(t, sqlmodel.QueryCommandMany, result.Command.Command)
	require.Len(t, result.Metadata, 1)
	assert.Equal(t, "group_by", result.Metadata[0].Directive)
	assert.Equal(t, "orders.id", result.Metadata[0].Value)
}

func TestApplyTopCall_KeywordDoesNotOverridePositional(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 5),
		CloseSpan: makeTokenSpan(1, 40),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "Positional"}, Span: makeTokenSpan(1, 6)},
		},
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "name", Value: syntax.ParsedValue{Raw: "FromKeyword"}, Span: makeTokenSpan(1, 20), KeySpan: makeTokenSpan(1, 20), ValueSpan: makeTokenSpan(1, 26)},
		},
	}
	result := &DirectiveBlock{}

	applyTopCall(result, call)

	require.NotNil(t, result.Name)
	assert.Equal(t, "Positional", result.Name.Value)
}

func TestApplyTopCall_InvalidCommandStaysUnset(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 5),
		CloseSpan: makeTokenSpan(1, 30),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "GetUser"}, Span: makeTokenSpan(1, 6)},
			{Value: syntax.ParsedValue{Raw: "notACommand"}, Span: makeTokenSpan(1, 15)},
		},
	}
	result := &DirectiveBlock{}

	applyTopCall(result, call)

	require.NotNil(t, result.Name)
	assert.Nil(t, result.Command)
}

func TestApplyTopCall_EmptyPositionalsAndKeywordsLeaveResultUntouched(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 5),
		CloseSpan: makeTokenSpan(1, 6),
		Closed:    true,
	}
	result := &DirectiveBlock{}

	applyTopCall(result, call)

	assert.Nil(t, result.Name)
	assert.Nil(t, result.Command)
	assert.Empty(t, result.Metadata)
}

func TestApplyHeaderCall_DispatchesEmbed(t *testing.T) {
	t.Parallel()

	spec, ok := lookupDirective("embed")
	require.True(t, ok)

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 12),
		CloseSpan: makeTokenSpan(1, 28),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "orders"}, Span: makeTokenSpan(1, 13)},
		},
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "from", Value: syntax.ParsedValue{Raw: "o"}, Span: makeTokenSpan(1, 21), KeySpan: makeTokenSpan(1, 21), ValueSpan: makeTokenSpan(1, 27)},
		},
	}
	result := &DirectiveBlock{}
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")
	var diagnostics []diag.SourceError

	applyHeaderCall(result, spec, call, errorBuilder, &diagnostics, "sgl."+spec.Name)

	require.Len(t, result.Embeds, 1)
	assert.Equal(t, "orders", result.Embeds[0].Table)
	assert.Equal(t, "o", result.Embeds[0].From)
	assert.Equal(t, makeTokenSpan(1, 27), result.Embeds[0].FromSpan)
	assert.Empty(t, diagnostics)
}

func TestApplyHeaderCall_DispatchesColumn(t *testing.T) {
	t.Parallel()

	spec, ok := lookupDirective("column")
	require.True(t, ok)

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 12),
		CloseSpan: makeTokenSpan(1, 35),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "email_lower"}, Span: makeTokenSpan(1, 13)},
		},
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "type", Value: syntax.ParsedValue{Raw: "text"}, Span: makeTokenSpan(1, 25), KeySpan: makeTokenSpan(1, 25), ValueSpan: makeTokenSpan(1, 31)},
		},
	}
	result := &DirectiveBlock{}
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")
	var diagnostics []diag.SourceError

	applyHeaderCall(result, spec, call, errorBuilder, &diagnostics, "sgl."+spec.Name)

	require.Len(t, result.ColumnOverrides, 1)
	assert.Equal(t, "email_lower", result.ColumnOverrides[0].Name)
	assert.Equal(t, "text", result.ColumnOverrides[0].SQLType)
}

func TestApplyEmbedHeaderCall_FillsTableFromKeyword(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 12),
		CloseSpan: makeTokenSpan(1, 35),
		Closed:    true,
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "table", Value: syntax.ParsedValue{Raw: "orders"}, Span: makeTokenSpan(1, 13), KeySpan: makeTokenSpan(1, 13), ValueSpan: makeTokenSpan(1, 20)},
			{Key: "as", Value: syntax.ParsedValue{Raw: "Orders"}, Span: makeTokenSpan(1, 25), KeySpan: makeTokenSpan(1, 25), ValueSpan: makeTokenSpan(1, 30)},
		},
	}
	result := &DirectiveBlock{}

	applyEmbedHeaderCall(result, call)

	require.Len(t, result.Embeds, 1)
	assert.Equal(t, "orders", result.Embeds[0].Table)
	assert.Equal(t, "Orders", result.Embeds[0].As)
	assert.Equal(t, makeTokenSpan(1, 30), result.Embeds[0].AsSpan)
	require.Len(t, result.Metadata, 1)
	assert.Equal(t, "embed", result.Metadata[0].Directive)
	assert.Equal(t, "orders", result.Metadata[0].Value)
}

func TestApplyEmbedHeaderCall_PositionalWins(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 12),
		CloseSpan: makeTokenSpan(1, 40),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "positional"}, Span: makeTokenSpan(1, 13)},
		},
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "table", Value: syntax.ParsedValue{Raw: "keyword"}, Span: makeTokenSpan(1, 25), KeySpan: makeTokenSpan(1, 25), ValueSpan: makeTokenSpan(1, 32)},
		},
	}
	result := &DirectiveBlock{}

	applyEmbedHeaderCall(result, call)

	require.Len(t, result.Embeds, 1)
	assert.Equal(t, "positional", result.Embeds[0].Table)
}

func TestApplyColumnHeaderCall_TypeOnly(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 12),
		CloseSpan: makeTokenSpan(1, 35),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "id"}, Span: makeTokenSpan(1, 13)},
		},
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "type", Value: syntax.ParsedValue{Raw: "int8"}, Span: makeTokenSpan(1, 17), KeySpan: makeTokenSpan(1, 17), ValueSpan: makeTokenSpan(1, 23)},
		},
	}
	result := &DirectiveBlock{}
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")
	var diagnostics []diag.SourceError

	applyColumnHeaderCall(result, call, errorBuilder, &diagnostics, "sgl.column")

	require.Len(t, result.ColumnOverrides, 1)
	override := result.ColumnOverrides[0]
	assert.Equal(t, "id", override.Name)
	assert.Equal(t, "int8", override.SQLType)
	assert.Equal(t, "", override.GoType)
	assert.Empty(t, diagnostics)
}

func TestApplyColumnHeaderCall_MutuallyExclusive(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 12),
		CloseSpan: makeTokenSpan(1, 50),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "id"}, Span: makeTokenSpan(1, 13)},
		},
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "type", Value: syntax.ParsedValue{Raw: "text"}, Span: makeTokenSpan(1, 17), KeySpan: makeTokenSpan(1, 17), ValueSpan: makeTokenSpan(1, 23)},
			{Key: "go_type", Value: syntax.ParsedValue{Raw: "foo.Bar"}, Span: makeTokenSpan(1, 30), KeySpan: makeTokenSpan(1, 30), ValueSpan: makeTokenSpan(1, 40)},
		},
	}
	result := &DirectiveBlock{}
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")
	var diagnostics []diag.SourceError

	applyColumnHeaderCall(result, call, errorBuilder, &diagnostics, "sgl.column")

	require.Len(t, result.ColumnOverrides, 1)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, diag.CodeMutuallyExclusiveKeywordArgument, diagnostics[0].Code)
}

func TestApplyColumnHeaderCall_NullablePointer(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 12),
		CloseSpan: makeTokenSpan(1, 35),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "id"}, Span: makeTokenSpan(1, 13)},
		},
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "nullable", Value: syntax.ParsedValue{Raw: "true"}, Span: makeTokenSpan(1, 17), KeySpan: makeTokenSpan(1, 17), ValueSpan: makeTokenSpan(1, 27)},
		},
	}
	result := &DirectiveBlock{}
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")
	var diagnostics []diag.SourceError

	applyColumnHeaderCall(result, call, errorBuilder, &diagnostics, "sgl.column")

	require.Len(t, result.ColumnOverrides, 1)
	override := result.ColumnOverrides[0]
	require.NotNil(t, override.Nullable)
	assert.True(t, *override.Nullable)
}

func TestApplyColumnHeaderCall_NullableFalse(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 12),
		CloseSpan: makeTokenSpan(1, 35),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "id"}, Span: makeTokenSpan(1, 13)},
		},
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "nullable", Value: syntax.ParsedValue{Raw: "false"}, Span: makeTokenSpan(1, 17), KeySpan: makeTokenSpan(1, 17), ValueSpan: makeTokenSpan(1, 27)},
		},
	}
	result := &DirectiveBlock{}
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")
	var diagnostics []diag.SourceError

	applyColumnHeaderCall(result, call, errorBuilder, &diagnostics, "sgl.column")

	require.Len(t, result.ColumnOverrides, 1)
	override := result.ColumnOverrides[0]
	require.NotNil(t, override.Nullable)
	assert.False(t, *override.Nullable)
}

func TestApplyColumnHeaderCall_NameFromKeyword(t *testing.T) {
	t.Parallel()

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 12),
		CloseSpan: makeTokenSpan(1, 35),
		Closed:    true,
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "name", Value: syntax.ParsedValue{Raw: "email_lower"}, Span: makeTokenSpan(1, 13), KeySpan: makeTokenSpan(1, 13), ValueSpan: makeTokenSpan(1, 20)},
			{Key: "type", Value: syntax.ParsedValue{Raw: "text"}, Span: makeTokenSpan(1, 25), KeySpan: makeTokenSpan(1, 25), ValueSpan: makeTokenSpan(1, 31)},
		},
	}
	result := &DirectiveBlock{}
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")
	var diagnostics []diag.SourceError

	applyColumnHeaderCall(result, call, errorBuilder, &diagnostics, "sgl.column")

	require.Len(t, result.ColumnOverrides, 1)
	assert.Equal(t, "email_lower", result.ColumnOverrides[0].Name)
}

func TestBuildParameterDirective_PositionalNameFillsName(t *testing.T) {
	t.Parallel()

	spec, ok := lookupDirective("param")
	require.True(t, ok)

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 20),
		CloseSpan: makeTokenSpan(1, 35),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "user_id"}, Span: makeTokenSpan(1, 21)},
		},
	}
	anchorToken := syntax.Token{Lexeme: "$1", Span: makeTokenSpan(1, 4)}
	kindSpan := makeTokenSpan(1, 10)

	directive := buildParameterDirective(spec, call, anchorToken, kindSpan, false, 1)

	require.NotNil(t, directive)
	assert.Equal(t, "user_id", directive.Name)
	assert.Equal(t, "param", directive.DirectiveName)
	assert.Equal(t, 1, directive.Number)
	assert.False(t, directive.IsNamed)
}

func TestBuildParameterDirective_KeywordsFillTypeNullableDefault(t *testing.T) {
	t.Parallel()

	spec, ok := lookupDirective("param")
	require.True(t, ok)

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 20),
		CloseSpan: makeTokenSpan(1, 70),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "user_id"}, Span: makeTokenSpan(1, 21)},
		},
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "type", Value: syntax.ParsedValue{Raw: "int8"}, Span: makeTokenSpan(1, 30), KeySpan: makeTokenSpan(1, 30), ValueSpan: makeTokenSpan(1, 36)},
			{Key: "nullable", Value: syntax.ParsedValue{Raw: "true"}, Span: makeTokenSpan(1, 42), KeySpan: makeTokenSpan(1, 42), ValueSpan: makeTokenSpan(1, 52)},
			{Key: "default", Value: syntax.ParsedValue{Raw: "42"}, Span: makeTokenSpan(1, 58), KeySpan: makeTokenSpan(1, 58), ValueSpan: makeTokenSpan(1, 68)},
		},
	}
	anchorToken := syntax.Token{Lexeme: "$1", Span: makeTokenSpan(1, 4)}
	kindSpan := makeTokenSpan(1, 10)

	directive := buildParameterDirective(spec, call, anchorToken, kindSpan, false, 1)

	require.NotNil(t, directive)
	require.NotNil(t, directive.TypeHint)
	assert.Equal(t, "int8", *directive.TypeHint)
	require.NotNil(t, directive.Nullable)
	assert.True(t, *directive.Nullable)
	require.NotNil(t, directive.DefaultValue)
	assert.Equal(t, 42, *directive.DefaultValue)
}

func TestBuildParameterDirective_SortableColumnsListFillsColumns(t *testing.T) {
	t.Parallel()

	spec, ok := lookupDirective("sortable")
	require.True(t, ok)

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 20),
		CloseSpan: makeTokenSpan(1, 50),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "order_by"}, Span: makeTokenSpan(1, 21)},
			{
				Value: syntax.ParsedValue{
					Raw:    "",
					AsList: []string{"name", "total"},
					IsList: true,
				},
				Span: makeTokenSpan(1, 31),
			},
		},
	}
	anchorToken := syntax.Token{Lexeme: "$1", Span: makeTokenSpan(1, 4)}
	kindSpan := makeTokenSpan(1, 10)

	directive := buildParameterDirective(spec, call, anchorToken, kindSpan, false, 1)

	require.NotNil(t, directive)
	assert.Equal(t, "order_by", directive.Name)
	assert.Equal(t, []string{"name", "total"}, directive.Columns)
}

func TestBuildParameterDirective_MaxIntKeywordFillsMaxVal(t *testing.T) {
	t.Parallel()

	spec, ok := lookupDirective("param")
	require.True(t, ok)

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 20),
		CloseSpan: makeTokenSpan(1, 50),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "page_size"}, Span: makeTokenSpan(1, 21)},
		},
		KeywordArguments: []syntax.ParsedKeywordArgument{
			{Key: "max", Value: syntax.ParsedValue{Raw: "100"}, Span: makeTokenSpan(1, 32), KeySpan: makeTokenSpan(1, 32), ValueSpan: makeTokenSpan(1, 45)},
		},
	}
	anchorToken := syntax.Token{Lexeme: "$1", Span: makeTokenSpan(1, 4)}
	kindSpan := makeTokenSpan(1, 10)

	directive := buildParameterDirective(spec, call, anchorToken, kindSpan, false, 1)

	require.NotNil(t, directive)
	require.NotNil(t, directive.MaxValue)
	assert.Equal(t, 100, *directive.MaxValue)
}

func TestBuildParameterDirective_NoPositionalsReturnsNil(t *testing.T) {
	t.Parallel()

	spec, ok := lookupDirective("param")
	require.True(t, ok)

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 20),
		CloseSpan: makeTokenSpan(1, 21),
		Closed:    true,
	}
	anchorToken := syntax.Token{Lexeme: "$1", Span: makeTokenSpan(1, 4)}
	kindSpan := makeTokenSpan(1, 10)

	directive := buildParameterDirective(spec, call, anchorToken, kindSpan, false, 1)

	assert.Nil(t, directive)
}

func TestBuildParameterDirective_NamedAnchorMarksIsNamed(t *testing.T) {
	t.Parallel()

	spec, ok := lookupDirective("param")
	require.True(t, ok)

	call := &syntax.CallArguments{
		OpenSpan:  makeTokenSpan(1, 20),
		CloseSpan: makeTokenSpan(1, 30),
		Closed:    true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "status"}, Span: makeTokenSpan(1, 21)},
		},
	}
	anchorToken := syntax.Token{Lexeme: ":status", Span: makeTokenSpan(1, 4)}
	kindSpan := makeTokenSpan(1, 10)

	directive := buildParameterDirective(spec, call, anchorToken, kindSpan, true, 3)

	require.NotNil(t, directive)
	assert.True(t, directive.IsNamed)
	assert.Equal(t, 3, directive.Number)
	assert.Equal(t, sqlmodel.ParameterDirectiveParam, directive.Kind)
}

func TestApplyColumnHeaderCall_StructTags(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		want    map[string]string
		name    string
		line    string
		problem string
	}{
		{name: "one tag", line: `-- sgl.column(email, struct_tags: 'validate:"required,email"')`, want: map[string]string{"validate": "required,email"}},
		{name: "several tags", line: `-- sgl.column(email, struct_tags: 'validate:"email" db:"{name}"')`, want: map[string]string{"validate": "email", "db": "{name}"}},
		{name: "json replaces the json tag", line: `-- sgl.column(password_hash, struct_tags: 'json:"-"')`, want: map[string]string{"json": "-"}},
		{name: "beside a Go type", line: `-- sgl.column(id, go_type: "github.com/google/uuid.UUID", struct_tags: 'validate:"uuid"')`, want: map[string]string{"validate": "uuid"}},
		{name: "a value that is no tag", line: `-- sgl.column(email, struct_tags: 'required')`, problem: `struct_tags for column "email"`},
		{name: "a repeated key", line: `-- sgl.column(email, struct_tags: 'db:"a" db:"b"')`, problem: "more than once"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			block, diagnostics := parseSQL(t, newTestParser(), "-- sgl.query(GetUser, one)\n"+testCase.line+"\nSELECT 1;")

			require.Len(t, block.ColumnOverrides, 1)
			if testCase.problem != "" {
				require.Len(t, diagnostics, 1)
				require.Equal(t, diag.CodeInvalidKeywordArgumentValue, diagnostics[0].Code)
				require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
				require.Contains(t, diagnostics[0].Message, testCase.problem)
				require.Equal(t, 2, diagnostics[0].Line)
				require.Nil(t, block.ColumnOverrides[0].StructTags)
				return
			}
			require.Empty(t, diagnostics)
			require.Equal(t, testCase.want, block.ColumnOverrides[0].StructTags)
		})
	}
}

func TestApplyColumnHeaderCallRecordsAnOverrideOnlyForANamedColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		line      string
		wantNames []string
		wantLines []int
	}{
		{name: "an unclosed call with no arguments", line: "-- sgl.column(", wantNames: nil, wantLines: nil},
		{name: "a closed call with no arguments", line: "-- sgl.column()", wantNames: nil, wantLines: nil},
		{name: "keyword arguments without a name", line: "-- sgl.column(type: text, nullable: true)", wantNames: nil, wantLines: nil},
		{name: "a positional name", line: "-- sgl.column(id, type: text)", wantNames: []string{"id"}, wantLines: []int{2}},
		{name: "a name keyword", line: "-- sgl.column(type: text, name: id)", wantNames: []string{"id"}, wantLines: []int{2}},
		{name: "an unclosed call with a name", line: "-- sgl.column(id, type: text", wantNames: []string{"id"}, wantLines: []int{2}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			block, diagnostics := parseSQL(t, newTestParser(), "-- sgl.query(GetUser, one)\n"+testCase.line+"\nSELECT 1;")

			var names []string
			var lines []int
			for _, override := range block.ColumnOverrides {
				names = append(names, override.Name)
				lines = append(lines, override.Span.Line)
				require.Positive(t, override.NameSpan.Line, "the override's name must be located")
			}
			require.Equal(t, testCase.wantNames, names)
			require.Equal(t, testCase.wantLines, lines)
			for _, diagnostic := range diagnostics {
				require.Positive(t, diagnostic.Line, "%s: %s", diagnostic.Code, diagnostic.Message)
			}
			if testCase.wantNames == nil {
				require.True(t, reportsCode(diagnostics, diag.CodeMissingRequired), "the missing name must be reported")
			}
		})
	}
}

func TestApplyHeaderCallLocatesAnUnclosedCall(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		line     string
		spanOf   func(block *DirectiveBlock) diagbuild.TextSpan
		wantSpan diagbuild.TextSpan
	}{
		{
			name:     "an embed",
			line:     "-- sgl.embed(users",
			spanOf:   func(block *DirectiveBlock) diagbuild.TextSpan { return block.Embeds[0].Span },
			wantSpan: diagbuild.TextSpan{Line: 2, Column: 13, EndLine: 2, EndColumn: 14},
		},
		{
			name:     "a column",
			line:     "-- sgl.column(id",
			spanOf:   func(block *DirectiveBlock) diagbuild.TextSpan { return block.ColumnOverrides[0].Span },
			wantSpan: diagbuild.TextSpan{Line: 2, Column: 14, EndLine: 2, EndColumn: 15},
		},
		{
			name:     "a sortable",
			line:     "-- sgl.sortable(order_by, [id]",
			spanOf:   func(block *DirectiveBlock) diagbuild.TextSpan { return block.Parameters[0].Span },
			wantSpan: diagbuild.TextSpan{Line: 2, Column: 16, EndLine: 2, EndColumn: 17},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			block, diagnostics := parseSQL(t, newTestParser(), "-- sgl.query(GetUser, one)\n"+testCase.line+"\nSELECT 1;")

			require.True(t, reportsCode(diagnostics, diag.CodeUnclosedDirective), "the unclosed call must be reported")
			require.Equal(t, testCase.wantSpan, testCase.spanOf(block))
		})
	}
}
