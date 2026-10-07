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

package models

import (
	"context"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

func generateModels(ctx context.Context, packageName string, catalogue *sqlmodel.Catalogue, mappings sqlmodel.GoTypeResolver) ([]sqlmodel.GeneratedFile, error) {
	return generateModelsNaming(ctx, packageName, catalogue, mappings, emitkit.NameStyle{})
}

func generateModelsNaming(ctx context.Context, packageName string, catalogue *sqlmodel.Catalogue, mappings sqlmodel.GoTypeResolver, style emitkit.NameStyle) ([]sqlmodel.GeneratedFile, error) {
	request := &emit.Request{Catalogue: catalogue, Types: mappings, Package: packageName}
	style.SQLConstantSuffix = "SQL"
	naming, _ := emitkit.PlanPackage(request, emitkit.DefaultReservation(&request.Dialect, &request.Style), style)
	return GenerateWithReferences(ctx, packageName, catalogue, mappings, &naming, &request.Style, nil)
}

func textMappings() sqlmodel.GoTypeResolver {
	return &gotypes.MappingTable{Mappings: []gotypes.Mapping{
		{
			SQLCategory: sqlmodel.TypeCategoryInteger,
			NotNull:     sqlmodel.GoType{Composite: nil, Package: "", Name: "int64"},
			Nullable:    sqlmodel.GoType{Composite: nil, Package: "", Name: "*int64"},
		},
		{
			SQLCategory: sqlmodel.TypeCategoryText,
			NotNull:     sqlmodel.GoType{Composite: nil, Package: "", Name: "string"},
			Nullable:    sqlmodel.GoType{Composite: nil, Package: "", Name: "*string"},
		},
	}}
}

func requireValidGo(t *testing.T, content []byte) {
	t.Helper()
	_, err := parser.ParseFile(token.NewFileSet(), "generated.go", content, parser.AllErrors)
	require.NoErrorf(t, err, "emitted source must be valid Go:\n%s", content)
}
