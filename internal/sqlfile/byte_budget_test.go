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

package sqlfile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/fault"
)

func TestByteBudgetIsSharedAcrossReaders(t *testing.T) {
	t.Parallel()

	migrations := fstest.MapFS{"0001_init.up.sql": &fstest.MapFile{Data: []byte(strings.Repeat("M", 6))}}
	queries := fstest.MapFS{"items.sql": &fstest.MapFile{Data: []byte(strings.Repeat("Q", 5))}}
	budget := NewByteBudget(10)
	migrationReader := NewFSReader(migrations, 0, 0).WithBudget(budget)
	queryReader := NewFSReader(queries, 0, 0).WithBudget(budget)

	content, err := migrationReader.ReadFile(t.Context(), "0001_init.up.sql")
	require.NoError(t, err)
	require.Len(t, content, 6)

	_, err = queryReader.ReadFile(t.Context(), "items.sql")
	require.ErrorIs(t, err, fault.ErrLimit)
	require.NotErrorIs(t, err, fault.ErrFileTooLarge)
	require.ErrorContains(t, err, "10 bytes in total")

	unbudgeted, err := NewFSReader(queries, 0, 0).ReadFile(t.Context(), "items.sql")
	require.NoError(t, err)
	require.Len(t, unbudgeted, 5)

	unlimited := NewFSReader(queries, 0, 0).WithBudget(NewByteBudget(0))
	_, err = unlimited.ReadFile(t.Context(), "items.sql")
	require.NoError(t, err)
}
