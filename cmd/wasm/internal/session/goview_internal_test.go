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

package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/scaling"
	"seagull.sh/seagull/sdk/emit"
)

const builderSource = "package store\n\n" +
	"type Queries struct{}\n\n" +
	"type Shelf struct {\n\tID int64\n}\n\n" +
	"type LoopBuilder struct {\n\tqueries *Queries\n}\n\n" +
	"type OtherBuilder struct {\n\tqueries *Queries\n}\n\n" +
	"func (queries *Queries) Loop() *LoopBuilder { return &LoopBuilder{queries: queries} }\n\n" +
	"func (builder *LoopBuilder) Where() *LoopBuilder { return builder }\n\n" +
	"func (builder *LoopBuilder) Limit() *LoopBuilder { return builder }\n\n" +
	"func (builder *LoopBuilder) Other() *OtherBuilder { return &OtherBuilder{queries: builder.queries} }\n\n" +
	"func (builder *OtherBuilder) Back() *LoopBuilder { return &LoopBuilder{queries: builder.queries} }\n\n" +
	"func (builder *LoopBuilder) Count() (int64, error) { return 0, nil }\n"

func TestRowTypeLooksThroughEachBuilderOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "builders whose methods return only builders and no struct",
			source: builderSource,
			want:   "",
		},
		{
			name:   "a builder whose terminal method returns the row struct",
			source: builderSource + "\nfunc (builder *OtherBuilder) All() ([]Shelf, error) { return nil, nil }\n",
			want:   "Shelf",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			index := newGoIndex([]emit.GeneratedFile{{Name: "loop.sql.go", Content: []byte(testCase.source)}}, jsonTagStyle{name: func(column string) string { return column }, omitted: false})
			method := index.declarations.Method("Loop")
			require.NotNil(t, method)

			scaling.Within(t, 100*time.Millisecond, func(context.Context) {
				require.Equal(t, testCase.want, index.rowType("Loop", method))
			})
		})
	}
}
