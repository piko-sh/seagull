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

package source

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryNameReturnsNothingForAnUnknownQuery(t *testing.T) {
	t.Parallel()

	require.Empty(t, QueryName("SELECT id FROM never_registered"))
}

func TestQueryNameReturnsTheNameRegisteredForAQuery(t *testing.T) {
	original := queryNameMap
	t.Cleanup(func() { queryNameMap = original })
	queryNameMap = map[string]string{"SELECT id FROM posts": "ListPosts"}

	require.Equal(t, "ListPosts", QueryName("SELECT id FROM posts"))
	require.Empty(t, QueryName("SELECT id FROM posts "))
}

func TestQueryNameNamesEditedSQLByTheLongestPrefixItStartsWith(t *testing.T) {
	originalMap, originalPrefixes, originalNames := queryNameMap, queryNamePrefixes, queryNamePrefixNames
	t.Cleanup(func() {
		queryNameMap, queryNamePrefixes, queryNamePrefixNames = originalMap, originalPrefixes, originalNames
	})
	queryNameMap = map[string]string{"SELECT id FROM posts WHERE id IN ($1)": "PostsByID"}
	queryNamePrefixes = []string{"SELECT id FROM posts WHERE id ", "SELECT id FROM posts"}
	queryNamePrefixNames = []string{"PostsByID", "ListPosts"}

	require.Equal(t, "PostsByID", QueryName("SELECT id FROM posts WHERE id IN ($1)"))
	require.Equal(t, "PostsByID", QueryName("SELECT id FROM posts WHERE id IN ($1,$2,$3)"))
	require.Equal(t, "ListPosts", QueryName("SELECT id FROM posts ORDER BY id"))
	require.Empty(t, QueryName("SELECT id FROM users"))
}
