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

package sqlmodel_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func text(value string) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentText, Text: value}
}

func parameter(index int) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentParameter, Parameter: index}
}

func TestSQLSegmentKindString(t *testing.T) {
	t.Parallel()

	kinds := []sqlmodel.SQLSegmentKind{
		sqlmodel.SegmentText,
		sqlmodel.SegmentParameter,
		sqlmodel.SegmentList,
		sqlmodel.SegmentColumn,
		sqlmodel.SegmentStar,
		sqlmodel.SegmentFilter,
		sqlmodel.SegmentOrderBy,
		sqlmodel.SegmentPagination,
		sqlmodel.SegmentValuesRow,
	}
	names := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		names = append(names, kind.String())
	}

	require.Equal(t, "text parameter list column star filter order-by pagination values-row", strings.Join(names, " "))
	require.Equal(t, "SQLSegmentKind(200)", sqlmodel.SQLSegmentKind(200).String())
}
