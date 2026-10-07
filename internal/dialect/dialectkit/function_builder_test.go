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

package dialectkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestTypedCatalogueBuilderResolvesTheDialectTypes(t *testing.T) {
	t.Parallel()

	builder := dialectkit.NewTypedCatalogueBuilder(map[string]sqlmodel.SQLType{
		"int4":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4"),
		"MyType": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "mytype"),
	})

	cases := []struct {
		name         string
		typeName     string
		wantCategory sqlmodel.SQLTypeCategory
		wantEngine   string
	}{
		{name: "registered name", typeName: "int4", wantCategory: sqlmodel.TypeCategoryInteger, wantEngine: "int4"},
		{name: "case is ignored", typeName: "INT4", wantCategory: sqlmodel.TypeCategoryInteger, wantEngine: "int4"},
		{name: "mixed-case key", typeName: "mytype", wantCategory: sqlmodel.TypeCategoryText, wantEngine: "mytype"},
		{name: "unregistered name", typeName: "Missing", wantCategory: sqlmodel.TypeCategoryUnknown, wantEngine: "missing"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolved := builder.Type(testCase.typeName)
			require.Equal(t, testCase.wantCategory, resolved.Category)
			require.Equal(t, testCase.wantEngine, resolved.EngineName)
		})
	}
}

func TestUntypedCatalogueBuilderResolvesNothing(t *testing.T) {
	t.Parallel()

	builder := dialectkit.NewCatalogueBuilder()
	signature := builder.NeverNull("probe", nil, builder.Type("int4"))

	require.Equal(t, sqlmodel.TypeCategoryUnknown, signature.ReturnType.Category)
	require.Len(t, builder.Catalogue.Functions["probe"], 1)
}
