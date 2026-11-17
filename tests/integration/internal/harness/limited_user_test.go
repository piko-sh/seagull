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

package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithCredentialsRewritesURLsAndDataSourceNames(t *testing.T) {
	t.Parallel()
	require.Equal(t, "postgres://lim:limited_pw@localhost:5432/db?sslmode=disable",
		withCredentials("postgres://admin:secret@localhost:5432/db?sslmode=disable", "lim", true))
	require.Equal(t, "postgres://lim@localhost/db", withCredentials("postgres://admin@localhost/db", "lim", false))
	require.Equal(t, "lim:limited_pw@tcp(localhost:3306)/db", withCredentials("root:secret@tcp(localhost:3306)/db", "lim", true))
	require.Equal(t, "lim@tcp(localhost:3306)/db", withCredentials("tcp(localhost:3306)/db", "lim", false))
}

func TestLimitedUserEnginesDifferOnlyInLogin(t *testing.T) {
	t.Parallel()
	postgres, cockroach := postgresLimitedUser(), cockroachLimitedUser()
	require.True(t, postgres.password)
	require.False(t, cockroach.password)
	require.Contains(t, postgres.create("u", "s")[0], "PASSWORD")
	require.Equal(t, "CREATE USER u", cockroach.create("u", "s")[0])
	require.Equal(t, postgres.create("u", "s")[1:], cockroach.create("u", "s")[1:])
}
