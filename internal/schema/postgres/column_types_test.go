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

package postgres

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestParseTypeModifiers(t *testing.T) {
	testCases := []struct {
		name      string
		formatted string
		want      []int
	}{
		{name: "precision and scale", formatted: "numeric(10,2)", want: []int{10, 2}},
		{name: "spaced precision and scale", formatted: "numeric(10, 2)", want: []int{10, 2}},
		{name: "three modifiers in declaration order", formatted: "geometry(3, 1, 2)", want: []int{3, 1, 2}},
		{name: "array of a modified type", formatted: "character varying(10)[]", want: []int{10}},
		{name: "modifier inside a multi-word name", formatted: "timestamp(3) with time zone", want: []int{3}},
		{name: "unconstrained type", formatted: "timestamp with time zone", want: nil},
		{name: "plain array", formatted: "integer[]", want: nil},
		{name: "parentheses in a quoted name", formatted: `app."odd(name)"`, want: nil},
		{name: "modifier out of range", formatted: "bit(99999999999999999999999)", want: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, parseTypeModifiers(testCase.formatted))
		})
	}
}

func TestTypeResolverResolvesDomains(t *testing.T) {
	resolver := &typeResolver{
		typeNormaliser: modifierTypeNormaliser{},
		domains: map[typeKey]domainEntry{
			{schema: "app", name: "email"}: {
				base:    typeDescription{name: "varchar", schema: "pg_catalog", kind: "b", formatted: "character varying(100)"},
				notNull: true,
			},
			{schema: "app", name: "email_list"}: {
				base: typeDescription{name: "email", schema: "app", kind: "d", dimensions: 1, formatted: "app.email[]"},
			},
			{schema: "app", name: "grid"}: {
				base: typeDescription{name: "int4", schema: "pg_catalog", kind: "b", dimensions: 2, formatted: "integer[]"},
			},
		},
	}

	testCases := []struct {
		name           string
		description    typeDescription
		wantType       string
		wantDimensions int
		wantNotNull    bool
	}{
		{
			name:        "not null domain",
			description: typeDescription{name: "email", schema: "app", kind: "d", formatted: "app.email"},
			wantType:    "varchar[100]",
			wantNotNull: true,
		},
		{
			name:           "array of a not null domain",
			description:    typeDescription{name: "email", schema: "app", kind: "d", dimensions: 1, formatted: "app.email[]"},
			wantType:       "varchar[100]",
			wantDimensions: 1,
		},
		{
			name:           "domain over an array of a not null domain",
			description:    typeDescription{name: "email_list", schema: "app", kind: "d", formatted: "app.email_list"},
			wantType:       "varchar[100]",
			wantDimensions: 1,
		},
		{
			name:           "dimensions are capped",
			description:    typeDescription{name: "grid", schema: "app", kind: "d", dimensions: 5, formatted: "app.grid[]"},
			wantType:       "int4",
			wantDimensions: maxArrayDimensions,
		},
		{
			name:        "domain in another schema is not confused",
			description: typeDescription{name: "email", schema: "other", kind: "d", formatted: "other.email"},
			wantType:    "email",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			resolved := resolver.resolve(testCase.description)
			assert.Equal(t, testCase.wantType, resolved.sqlType.EngineName)
			assert.Equal(t, testCase.wantDimensions, resolved.dimensions)
			assert.Equal(t, testCase.wantNotNull, resolved.notNull)
		})
	}
}

func TestTypeResolverClassifiesOnlyUnknownTypes(t *testing.T) {
	resolver := &typeResolver{typeNormaliser: modifierTypeNormaliser{}, domains: map[typeKey]domainEntry{}}

	recognised := resolver.resolve(typeDescription{name: "text", schema: "app", kind: "e", formatted: "app.text"})
	assert.Equal(t, sqlmodel.TypeCategoryText, recognised.sqlType.Category)
	assert.Empty(t, recognised.sqlType.Schema)

	composite := resolver.resolve(typeDescription{name: "Address", schema: "app", kind: "c", formatted: `app."Address"`})
	assert.Equal(t, sqlmodel.TypeCategoryComposite, composite.sqlType.Category)
	assert.Equal(t, "Address", composite.sqlType.EngineName)
	assert.Equal(t, "app", composite.sqlType.Schema)

	rangeType := resolver.resolve(typeDescription{name: "int4range", schema: "pg_catalog", kind: "r", formatted: "int4range"})
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, rangeType.sqlType.Category)
	assert.Empty(t, rangeType.sqlType.Schema)
}

func TestAttachEnumValuesSkipsUnknownEnums(t *testing.T) {
	schema := newTestSchema("app")
	schema.Enums["mood"] = &sqlmodel.Enum{Name: "mood", Schema: "app", Values: []string{"sad", "ok"}}
	enumColumn := func(name string, schemaName string) sqlmodel.Column {
		sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, name)
		sqlType.Schema = schemaName
		return sqlmodel.NewColumn(name, sqlType, true)
	}
	schema.Tables["things"] = &sqlmodel.Table{Name: "things", Columns: []sqlmodel.Column{
		enumColumn("mood", "app"),
		enumColumn("weather", "app"),
		enumColumn("mood", "elsewhere"),
	}}
	catalogue := &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{"app": schema}}

	attachEnumValues(catalogue)

	columns := catalogue.Schemas["app"].Tables["things"].Columns
	require.Len(t, columns, 3)
	assert.Equal(t, []string{"sad", "ok"}, columns[0].SQLType.EnumValues)
	assert.Empty(t, columns[1].SQLType.EnumValues)
	assert.Empty(t, columns[2].SQLType.EnumValues)
}

func domainChain(length int) map[typeKey]domainEntry {
	domains := make(map[typeKey]domainEntry, length)
	for index := range length {
		base := typeDescription{name: "int4", schema: "pg_catalog", kind: "b", formatted: "integer"}
		if index+1 < length {
			next := fmt.Sprintf("level_%02d", index+1)
			base = typeDescription{name: next, schema: "app", kind: "d", formatted: "app." + next}
		}
		domains[typeKey{schema: "app", name: fmt.Sprintf("level_%02d", index)}] = domainEntry{base: base}
	}
	return domains
}

func TestTypeResolverWarnsWhenDomainsAreNestedTooDeeplyToFollow(t *testing.T) {
	tests := []struct {
		domains      map[typeKey]domainEntry
		name         string
		wantType     string
		wantMessages []string
	}{
		{
			name:     "a chain as deep as the resolver follows",
			domains:  domainChain(maxDomainDepth),
			wantType: "int4",
		},
		{
			name:     "a chain one domain deeper",
			domains:  domainChain(maxDomainDepth + 1),
			wantType: "level_16",
			wantMessages: []string{`column "amount" of app.ledger is declared over domains nested more than 16 deep; ` +
				"its type was read from domain app.level_16, where the chain was no longer followed"},
		},
		{
			name: "a domain declared over itself",
			domains: map[typeKey]domainEntry{
				{schema: "app", name: "level_00"}: {base: typeDescription{name: "level_00", schema: "app", kind: "d", formatted: "app.level_00"}},
			},
			wantType: "level_00",
			wantMessages: []string{`column "amount" of app.ledger is declared over domains nested more than 16 deep; ` +
				"its type was read from domain app.level_00, where the chain was no longer followed"},
		},
		{
			name:     "a domain the catalogue does not hold",
			domains:  map[typeKey]domainEntry{},
			wantType: "level_00",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			resolver := &typeResolver{typeNormaliser: modifierTypeNormaliser{}, domains: testCase.domains}

			column := resolver.column("app.ledger", "amount", typeDescription{name: "level_00", schema: "app", kind: "d", formatted: "app.level_00"}, true)

			assert.Equal(t, testCase.wantType, column.SQLType.EngineName)
			messages := make([]string, 0, len(resolver.problems))
			for _, problem := range resolver.problems {
				assert.Equal(t, diag.CodeDepthLimit, problem.Code)
				assert.Equal(t, diag.SeverityWarning, problem.Severity)
				messages = append(messages, problem.Message)
			}
			if len(testCase.wantMessages) == 0 {
				assert.Empty(t, messages)
				return
			}
			assert.Equal(t, testCase.wantMessages, messages)
		})
	}
}

func TestBuildCatalogueWarnsAboutAColumnWhoseDomainsLoop(t *testing.T) {
	catalogue, sourceErrors, err := New(openFakeDatabase(t, catalogueHandlers...), modifierTypeNormaliser{}).BuildCatalogue(t.Context())
	require.NoError(t, err)
	require.NotNil(t, catalogue)
	require.Len(t, sourceErrors, 1)
	assert.Equal(t, diag.CodeDepthLimit, sourceErrors[0].Code)
	assert.Equal(t, diag.SeverityWarning, sourceErrors[0].Severity)
	assert.Equal(t, `column "looped" of app.users is declared over domains nested more than 16 deep; `+
		"its type was read from domain app.loop, where the chain was no longer followed", sourceErrors[0].Message)
}
