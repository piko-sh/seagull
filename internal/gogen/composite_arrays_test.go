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

package gogen

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestCompositeArrayParametersListsArraysOfCompositesWithoutAnOverride(t *testing.T) {
	t.Parallel()

	pair := sqlmodel.SQLType{EngineName: "pair", Category: sqlmodel.TypeCategoryComposite}
	row := sqlmodel.SQLType{EngineName: "solutions", Category: sqlmodel.TypeCategoryUnknown}
	text := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	arrayOf := func(element sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{EngineName: element.EngineName + "[]", Category: sqlmodel.TypeCategoryArray, ElementType: &element}
	}
	catalogue := &sqlmodel.Catalogue{DefaultSchema: "public", Schemas: map[string]*sqlmodel.Schema{"public": {
		Name:   "public",
		Tables: map[string]*sqlmodel.Table{"solutions": {Name: "solutions", Schema: "public"}},
	}}}
	override := &sqlmodel.GoType{Composite: nil, Package: "example.com/pairs", Name: "Pairs"}
	query := &sqlmodel.AnalysedQuery{Parameters: []sqlmodel.QueryParameter{
		{Name: "pairs", SQLType: arrayOf(pair)},
		{Name: "grid", SQLType: arrayOf(arrayOf(row))},
		{Name: "pair", SQLType: pair},
		{Name: "names", SQLType: arrayOf(text)},
		{Name: "chosen", SQLType: arrayOf(pair), GoTypeOverride: override},
	}}

	found := CompositeArrayParameters(nil, catalogue, query)

	require.Equal(t, []CompositeArrayParameter{{Name: "pairs", TypeName: "pair"}, {Name: "grid", TypeName: "solutions"}}, found)
}
