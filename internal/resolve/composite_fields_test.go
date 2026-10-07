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

package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func TestFieldsOfACompositeValueTakeTheTypesItsTypeDeclares(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	text := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	pair := sqlmodel.SQLType{EngineName: "pair", Category: sqlmodel.TypeCategoryComposite, Schema: "public"}

	testCases := []struct {
		name         string
		expression   ast.Expression
		wantType     sqlmodel.SQLType
		wantNullable bool
		wantCode     string
	}{
		{
			name:         "a field of a column of the type",
			expression:   &ast.StructFieldAccessExpression{Struct: &ast.ColumnRefExpression{ColumnName: "bar"}, FieldName: "a"},
			wantType:     integer,
			wantNullable: true,
		},
		{
			name:         "a field named in other letter case",
			expression:   &ast.StructFieldAccessExpression{Struct: &ast.ColumnRefExpression{ColumnName: "bar"}, FieldName: "B"},
			wantType:     text,
			wantNullable: true,
		},
		{
			name: "a field of a field of another composite type",
			expression: &ast.StructFieldAccessExpression{
				Struct:    &ast.StructFieldAccessExpression{Struct: &ast.ColumnRefExpression{ColumnName: "nest"}, FieldName: "p"},
				FieldName: "a",
			},
			wantType:     integer,
			wantNullable: true,
		},
		{
			name:         "a field the type declares as a composite type",
			expression:   &ast.StructFieldAccessExpression{Struct: &ast.ColumnRefExpression{ColumnName: "nest"}, FieldName: "p"},
			wantType:     pair,
			wantNullable: true,
		},
		{
			name:         "a field the type lacks",
			expression:   &ast.StructFieldAccessExpression{Struct: &ast.ColumnRefExpression{ColumnName: "bar"}, FieldName: "c"},
			wantType:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
			wantNullable: true,
			wantCode:     diag.CodeUnknownColumn,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			catalogue := resolver.catalogue
			catalogue.Schemas["public"].CompositeTypes = map[string]*sqlmodel.CompositeType{
				"pair": {Name: "pair", Schema: "public", Fields: []sqlmodel.Column{
					{Name: "a", SQLType: integer, Nullable: true},
					{Name: "b", SQLType: text, Nullable: true},
				}},
				"nested": {Name: "nested", Schema: "public", Fields: []sqlmodel.Column{
					{Name: "p", SQLType: sqlmodel.SQLType{EngineName: "pair", Category: sqlmodel.TypeCategoryUnknown}, Nullable: true},
				}},
			}
			catalogue.Schemas["public"].Tables["solution"] = fakedialect.NewTable("solution",
				sqlmodel.Column{Name: "bar", SQLType: pair, Nullable: false},
				sqlmodel.Column{Name: "nest", SQLType: sqlmodel.SQLType{EngineName: "nested", Category: sqlmodel.TypeCategoryComposite, Schema: "public"}},
			)
			scope.AddTable(ast.TableReference{Name: "solution"}, ast.JoinInner, catalogue.Schemas["public"].Tables["solution"])

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), testCase.expression, scope, new(false))

			if testCase.wantCode != "" {
				require.Error(t, err)
				require.Equal(t, testCase.wantCode, ExtractErrorCode(err))
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.wantType.Category, sqlType.Category)
			require.Equal(t, testCase.wantType.EngineName, sqlType.EngineName)
			require.Equal(t, testCase.wantNullable, nullable)
		})
	}
}
