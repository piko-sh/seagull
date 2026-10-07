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

package generate

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestQuerierInterfaceMatchesSourceCollection(t *testing.T) {
	source := []byte(`package store
import (
 context2 "context"
 moment "time"
 "iter"
)
type Queries struct{}
// Stream follows creation order.
func (*Queries) Stream(ctx context2.Context, start moment.Time) iter.Seq2[Row,error] { return nil }
func (*Queries) hidden() {}
func (*Queries) All(ctx context2.Context) ([]Row,error) { return nil,nil }
func (*Builder) Count() int { return 0 }
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "queries.go", source, parser.ParseComments)
	require.NoError(t, err)
	naming := emit.Naming{Files: map[string]string{"queries.sql": "queries.go"}, Interface: "Querier", Imports: map[string]string{"context": "context2", "time": "moment", "iter": "iter"}}
	emission := &gogen.Emission{Request: &emit.Request{Package: "store", Naming: naming}, Declarations: parsed.Decls}
	actual, err := querierInterface(t.Context(), emission)
	require.NoError(t, err)
	expected, err := emitkit.QuerierInterface("store", naming, []sqlmodel.GeneratedFile{{Name: "queries.go", Content: source}})
	require.NoError(t, err)
	require.Equal(t, string(expected.Content), string(actual.Content))
	emission.Declarations = nil
	actual, err = querierInterface(t.Context(), emission)
	require.NoError(t, err)
	expected, err = emitkit.QuerierInterface("store", naming, nil)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}
