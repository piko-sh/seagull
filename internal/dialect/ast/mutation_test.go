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

package ast

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestMutationKindIsValid(t *testing.T) {
	t.Parallel()

	assert.True(t, MutationCreateTable.IsValid())
	assert.True(t, MutationKillMutation.IsValid())
	assert.True(t, MutationDetachPartition.IsValid())
	assert.False(t, mutationKindCount.IsValid())
}

func TestNewFunctionDefinition(t *testing.T) {
	t.Parallel()

	function := sqlmodel.NewFunction(nil, sqlmodel.SQLType{}, sqlmodel.FunctionNullableNeverNull)
	definition := NewFunctionDefinition(function)

	assert.Same(t, function, definition.Function)
	assert.Nil(t, definition.Body)
	assert.Empty(t, definition.BodyParameters)
}

func TestNewFunctionReference(t *testing.T) {
	t.Parallel()

	definition := NewFunctionReference("reporting", "refresh_totals")

	require.NotNil(t, definition)
	want := &sqlmodel.Function{}
	want.Name = "refresh_totals"
	want.Schema = "reporting"
	want.NullableBehaviour = sqlmodel.FunctionNullableCalledOnNull
	assert.Equal(t, want, definition.Function)
	assert.Nil(t, definition.Body)
	assert.Empty(t, definition.BodyParameters)
}

func TestNewCatalogueMutation(t *testing.T) {
	t.Parallel()

	columns := []sqlmodel.Column{sqlmodel.NewColumn("id", sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4"), false)}
	constraints := []sqlmodel.Constraint{{Name: "users_pkey", Columns: []string{"id"}, Kind: sqlmodel.ConstraintPrimaryKey}}
	definition := NewFunctionReference("", "touch")
	parents := []TableReference{{Schema: "public", Name: "base"}}
	engineSpecific := map[string]string{"engine": "MergeTree"}
	viewBody := Warning{Message: "the view body could not be analysed", Span: Span{Start: 20, End: 48}}
	defaultValue := Warning{Message: "the default value could not be analysed", Span: Span{Start: 60, End: 64}}
	copied := []CopiedTable{{Table: TableReference{Schema: "public", Name: "template"}, Position: 1, Keys: true}}
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")

	testCases := []struct {
		want    func() *CatalogueMutation
		name    string
		options []CatalogueMutationOption
	}{
		{
			name:    "no options leaves every other attribute unset",
			options: nil,
			want:    func() *CatalogueMutation { return &CatalogueMutation{} },
		},
		{
			name:    "engine specific attributes",
			options: []CatalogueMutationOption{WithEngineSpecific(engineSpecific)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.EngineSpecific = engineSpecific
				return mutation
			},
		},
		{
			name:    "new name",
			options: []CatalogueMutationOption{WithNewName("accounts")},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.NewName = "accounts"
				return mutation
			},
		},
		{
			name:    "column name",
			options: []CatalogueMutationOption{WithColumnName("email")},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.ColumnName = "email"
				return mutation
			},
		},
		{
			name:    "columns",
			options: []CatalogueMutationOption{WithColumns(columns)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.Columns = columns
				return mutation
			},
		},
		{
			name:    "enum",
			options: []CatalogueMutationOption{WithEnum("mood", []string{"happy", "sad"})},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.EnumName = "mood"
				mutation.EnumValues = []string{"happy", "sad"}
				return mutation
			},
		},
		{
			name:    "function",
			options: []CatalogueMutationOption{WithFunction(definition)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.Function = definition
				return mutation
			},
		},
		{
			name:    "trigger name",
			options: []CatalogueMutationOption{WithTriggerName("users_touch")},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.TriggerName = "users_touch"
				return mutation
			},
		},
		{
			name:    "primary key",
			options: []CatalogueMutationOption{WithPrimaryKey([]string{"id"})},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.PrimaryKey = []string{"id"}
				return mutation
			},
		},
		{
			name:    "constraints",
			options: []CatalogueMutationOption{WithConstraints(constraints)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.Constraints = constraints
				return mutation
			},
		},
		{
			name:    "constraint name",
			options: []CatalogueMutationOption{WithConstraintName("users_email_key")},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.ConstraintName = "users_email_key"
				return mutation
			},
		},
		{
			name:    "inherited tables",
			options: []CatalogueMutationOption{WithInheritsTables(parents)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.InheritsTables = parents
				return mutation
			},
		},
		{
			name:    "dropped column prefix",
			options: []CatalogueMutationOption{WithDroppedColumnPrefix("legs.")},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.DroppedColumnPrefix = "legs."
				return mutation
			},
		},
		{
			name:    "partition of a parent",
			options: []CatalogueMutationOption{WithPartitionOf(TableReference{Schema: "public", Name: "measurements"})},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.PartitionOf = &TableReference{Schema: "public", Name: "measurements"}
				return mutation
			},
		},
		{
			name:    "without rowid",
			options: []CatalogueMutationOption{WithWithoutRowID(true)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.IsWithoutRowID = true
				return mutation
			},
		},
		{
			name:    "virtual module",
			options: []CatalogueMutationOption{WithVirtualModule("fts5")},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.IsVirtual = true
				mutation.VirtualModuleName = "fts5"
				return mutation
			},
		},
		{
			name:    "sequence",
			options: []CatalogueMutationOption{WithSequence("users_id_seq")},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.SequenceName = "users_id_seq"
				return mutation
			},
		},
		{
			name:    "sequence owner",
			options: []CatalogueMutationOption{WithSequenceOwner("users", "id")},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.OwnedByTable = "users"
				mutation.OwnedByColumn = "id"
				return mutation
			},
		},
		{
			name:    "type name",
			options: []CatalogueMutationOption{WithTypeName("address")},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.EnumName = "address"
				return mutation
			},
		},
		{
			name:    "target kind",
			options: []CatalogueMutationOption{WithTargetKind(ObjectKindSequence)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.TargetKind = ObjectKindSequence
				return mutation
			},
		},
		{
			name:    "if not exists",
			options: []CatalogueMutationOption{WithIfNotExists(true)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.IfNotExists = true
				return mutation
			},
		},
		{
			name:    "if exists",
			options: []CatalogueMutationOption{WithIfExists(true)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.IfExists = true
				return mutation
			},
		},
		{
			name:    "if exists left unsaid",
			options: []CatalogueMutationOption{WithIfExists(true), WithIfExists(false)},
			want:    func() *CatalogueMutation { return &CatalogueMutation{} },
		},
		{
			name:    "placement after an anchor",
			options: []CatalogueMutationOption{WithPlacement(PlacementAfter, "email")},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.Placement = Placement{Anchor: "email", Kind: PlacementAfter}
				return mutation
			},
		},
		{
			name:    "placement first",
			options: []CatalogueMutationOption{WithPlacement(PlacementFirst, "")},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.Placement = Placement{Anchor: "", Kind: PlacementFirst}
				return mutation
			},
		},
		{
			name:    "copied tables",
			options: []CatalogueMutationOption{WithCopiedTables(copied)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.CopiedTables = copied
				return mutation
			},
		},
		{
			name:    "a column made nullable",
			options: []CatalogueMutationOption{WithColumnNullability(true)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.DropsNotNull = true
				return mutation
			},
		},
		{
			name:    "a column made not nullable",
			options: []CatalogueMutationOption{WithColumnNullability(false)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.SetsNotNull = true
				return mutation
			},
		},
		{
			name:    "a column's type with array dimensions",
			options: []CatalogueMutationOption{WithColumnName("tags"), WithColumnType(text, 2)},
			want: func() *CatalogueMutation {
				column := sqlmodel.NewColumn("tags", text, true)
				column.ArrayDimensions = 2
				column.IsArray = true
				mutation := &CatalogueMutation{}
				mutation.ColumnName = "tags"
				mutation.Columns = []sqlmodel.Column{column}
				mutation.SetsType = true
				return mutation
			},
		},
		{
			name:    "a column's type without array dimensions",
			options: []CatalogueMutationOption{WithColumnName("name"), WithColumnType(text, 0)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.ColumnName = "name"
				mutation.Columns = []sqlmodel.Column{sqlmodel.NewColumn("name", text, true)}
				mutation.SetsType = true
				return mutation
			},
		},
		{
			name:    "a domain that may hold NULL",
			options: []CatalogueMutationOption{WithDomain("label", text, 0, false)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.EnumName = "label"
				mutation.Columns = []sqlmodel.Column{sqlmodel.NewColumn("label", text, true)}
				return mutation
			},
		},
		{
			name:    "a NOT NULL domain over an array",
			options: []CatalogueMutationOption{WithDomain("labels", text, 1, true)},
			want: func() *CatalogueMutation {
				column := sqlmodel.NewColumn("labels", text, false)
				column.ArrayDimensions = 1
				column.IsArray = true
				mutation := &CatalogueMutation{}
				mutation.EnumName = "labels"
				mutation.Columns = []sqlmodel.Column{column}
				return mutation
			},
		},
		{
			name:    "unanalysed parts accumulate in order",
			options: []CatalogueMutationOption{WithUnanalysedPart(viewBody), WithUnanalysedPart(defaultValue)},
			want: func() *CatalogueMutation {
				mutation := &CatalogueMutation{}
				mutation.UnanalysedParts = []Warning{viewBody, defaultValue}
				return mutation
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := NewCatalogueMutation(MutationCreateTable, "public", "users", testCase.options...)

			want := testCase.want()
			want.Kind = MutationCreateTable
			want.SchemaName = "public"
			want.TableName = "users"
			assert.Equal(t, want, mutation)
		})
	}
}

func TestNewCatalogueMutation_LeavesTheNewAttributesAtTheirZeroValues(t *testing.T) {
	t.Parallel()

	mutation := NewCatalogueMutation(MutationAlterTableRenameTable, "public", "users", WithNewName("accounts"))

	assert.Equal(t, ObjectKindAny, mutation.TargetKind)
	assert.Nil(t, mutation.UnanalysedParts)
}

func TestObjectKindString_NamesEachKind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		want string
		kind ObjectKind
	}{
		{name: "the zero value applies to any object", kind: ObjectKindAny, want: "any"},
		{name: "table", kind: ObjectKindTable, want: "table"},
		{name: "view", kind: ObjectKindView, want: "view"},
		{name: "sequence", kind: ObjectKindSequence, want: "sequence"},
		{name: "an undeclared value gives its number", kind: ObjectKind(200), want: "ObjectKind(200)"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.kind.String())
		})
	}
}
