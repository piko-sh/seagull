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
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestLookupDirective_ResolvesKnownNamesAndRejectsUnknown(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		lookup      string
		expectFound bool
		expectRole  DirectiveRole
	}{
		{name: "top-level sgl.query resolves to top role", lookup: "query", expectFound: true, expectRole: DirectiveRoleTop},
		{name: "sgl.param resolves to param role", lookup: "param", expectFound: true, expectRole: DirectiveRoleParam},
		{name: "sgl.embed resolves to header role", lookup: "embed", expectFound: true, expectRole: DirectiveRoleHeader},
		{name: "sgl.migration resolves to migration role", lookup: "migration", expectFound: true, expectRole: DirectiveRoleMigration},
		{name: "unknown name is not found", lookup: "nope", expectFound: false},
		{name: "wrong case is not found because lookup is case-sensitive", lookup: "Query", expectFound: false},
		{name: "empty name is not found", lookup: "", expectFound: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			spec, found := lookupDirective(testCase.lookup)
			assert.Equal(t, testCase.expectFound, found)
			if testCase.expectFound {
				require.NotNil(t, spec)
				assert.Equal(t, testCase.lookup, spec.Name)
				assert.Equal(t, testCase.expectRole, spec.Role)
			} else {
				assert.Nil(t, spec)
			}
		})
	}
}

func TestLookupDirective_ReturnsPointerIntoRegistrySoParamKindIsCarried(t *testing.T) {
	t.Parallel()

	spec, found := lookupDirective("sortable")
	require.True(t, found)
	require.NotNil(t, spec)
	assert.Equal(t, sqlmodel.ParameterDirectiveSortable, spec.ParamKind)
}

func TestDirectiveSpecsDeclareParameterKindsOnlyForParameterDirectives(t *testing.T) {
	t.Parallel()

	wantKinds := map[string]sqlmodel.ParameterDirectiveKind{
		"param":    sqlmodel.ParameterDirectiveParam,
		"sortable": sqlmodel.ParameterDirectiveSortable,
	}

	for index := range directiveSpecs {
		spec := &directiveSpecs[index]
		t.Run(spec.Name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, wantKinds[spec.Name], spec.ParamKind)
		})
	}
}

func TestLookupKeywordArgument_ResolvesAcceptedKeysAndRejectsUnknown(t *testing.T) {
	t.Parallel()

	spec, found := lookupDirective("query")
	require.True(t, found)

	keywordArgument, ok := LookupKeywordArgument(spec, "readonly")
	require.True(t, ok)
	require.NotNil(t, keywordArgument)
	assert.Equal(t, "readonly", keywordArgument.Name)
	assert.Equal(t, KeywordArgumentBool, keywordArgument.Kind)

	_, ok = LookupKeywordArgument(spec, "does_not_exist")
	assert.False(t, ok)
}

func TestLookupKeywordArgument_ReturnsFalseForNilSpec(t *testing.T) {
	t.Parallel()

	keywordArgument, ok := LookupKeywordArgument(nil, "readonly")
	assert.False(t, ok)
	assert.Nil(t, keywordArgument)
}

func TestLookupPositional_ResolvesByNameAndReportsIndex(t *testing.T) {
	t.Parallel()

	spec, found := lookupDirective("query")
	require.True(t, found)

	first, firstIndex, ok := lookupPositional(spec, "name")
	require.True(t, ok)
	require.NotNil(t, first)
	assert.Equal(t, "name", first.Name)
	assert.Equal(t, 0, firstIndex)

	second, secondIndex, ok := lookupPositional(spec, "command")
	require.True(t, ok)
	require.NotNil(t, second)
	assert.Equal(t, "command", second.Name)
	assert.Equal(t, 1, secondIndex)
}

func TestLookupPositional_ReturnsFalseForUnknownNameAndNilSpec(t *testing.T) {
	t.Parallel()

	spec, found := lookupDirective("query")
	require.True(t, found)

	positional, index, ok := lookupPositional(spec, "missing")
	assert.False(t, ok)
	assert.Nil(t, positional)
	assert.Equal(t, 0, index)

	positional, index, ok = lookupPositional(nil, "name")
	assert.False(t, ok)
	assert.Nil(t, positional)
	assert.Equal(t, 0, index)
}

func TestPositionalNames_ReturnsDeclarationOrder(t *testing.T) {
	t.Parallel()

	spec, found := lookupDirective("query")
	require.True(t, found)

	assert.Equal(t, []string{"name", "command"}, positionalNames(spec))
}

func TestPositionalNames_ReturnsNilForNilSpec(t *testing.T) {
	t.Parallel()

	assert.Nil(t, positionalNames(nil))
}

func TestPositionalNames_ReturnsEmptySliceForDirectiveWithoutPositionals(t *testing.T) {
	t.Parallel()

	spec, found := lookupDirective("param")
	require.True(t, found)

	specNoPositionals := &DirectiveSpec{Name: "synthetic"}
	assert.Empty(t, positionalNames(specNoPositionals))

	assert.Equal(t, []string{"name"}, positionalNames(spec))
}

func TestDirectiveNames_ContainsEveryRegisteredDirectiveInOrder(t *testing.T) {
	t.Parallel()

	names := directiveNames(namespace.Default())
	assert.Equal(t, []string{
		"sgl.query",
		"sgl.param",
		"sgl.sortable",
		"sgl.embed",
		"sgl.column",
		"sgl.migration",
	}, names)
	assert.Len(t, names, len(directiveSpecs))
}

func TestDirectiveNames_QualifiesWithCanonicalPrefix(t *testing.T) {
	t.Parallel()

	names := directiveNames(namespace.Set{Canonical: "acme", Aliases: []string{"acmedb"}, Foreign: nil})
	assert.Equal(t, "acme.query", names[0])
	assert.Len(t, names, len(directiveSpecs))
}

func TestKeywordArgumentNames_ReturnsAcceptedKeysInDisplayOrder(t *testing.T) {
	t.Parallel()

	spec, found := lookupDirective("query")
	require.True(t, found)

	assert.Equal(t, []string{"dynamic", "readonly", "nullable", "optional", "group_by", "model", "row"}, keywordArgumentNames(spec))
}

func TestKeywordArgumentNames_ReturnsNilForNilSpec(t *testing.T) {
	t.Parallel()

	assert.Nil(t, keywordArgumentNames(nil))
}

func TestKeywordArgumentNames_ReturnsEmptyForDirectiveWithoutKeywordArguments(t *testing.T) {
	t.Parallel()

	spec, found := lookupDirective("sortable")
	require.True(t, found)

	assert.Empty(t, keywordArgumentNames(spec))
}

func TestDirectiveSpecs_CommandPositionalCarriesClosedEnum(t *testing.T) {
	t.Parallel()

	spec, found := lookupDirective("query")
	require.True(t, found)

	command, _, ok := lookupPositional(spec, "command")
	require.True(t, ok)
	assert.Equal(t, KeywordArgumentIdentifier, command.Kind)
	assert.Equal(t, []string{
		"one", "many", "exec", "execresult", "execrows",
		"batch", "stream", "copyfrom", "asyncexec",
	}, command.AllowedValues)
	assert.True(t, command.Required)
}

func TestMigrationFileDirectivesShareTheirSpecsWithTheMigrationParser(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		role string
		want syntax.CallSpec
	}{
		{role: RoleColumn, want: syntax.ColumnCallSpec()},
		{role: RoleMigration, want: syntax.MigrationCallSpec()},
	}

	for _, testCase := range testCases {
		t.Run(testCase.role, func(t *testing.T) {
			t.Parallel()

			spec, found := lookupDirective(testCase.role)
			require.True(t, found)
			assert.Equal(t, testCase.want.Positionals, spec.Positionals)
			assert.Equal(t, testCase.want.KeywordArguments, spec.KeywordArguments)
		})
	}
}

func TestEveryDirectiveRoleIsKnownToTheMigrationParser(t *testing.T) {
	t.Parallel()

	for _, role := range Roles() {
		t.Run(role, func(t *testing.T) {
			t.Parallel()

			_, diagnostics := syntax.ParseMigration("-- sgl."+role+"(x)\n", syntax.NeutralLexicon(), namespace.Default(), "001.up.sql")

			for _, diagnostic := range diagnostics {
				assert.NotEqual(t, diag.CodeUnknownDirective, diagnostic.Code, diagnostic.Message)
			}
		})
	}
}
