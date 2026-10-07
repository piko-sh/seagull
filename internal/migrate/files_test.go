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

package migrate

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigratorRejectsDuplicateVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		files         fstest.MapFS
		name          string
		wantFirst     string
		wantSecond    string
		wantMessage   string
		wantDirection MigrationDirection
	}{
		{
			name: "two up files",
			files: fstest.MapFS{
				"migrations/0001_first.up.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE a (id int);")},
				"migrations/0001_second.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id int);")},
			},
			wantFirst:     "0001_first.up.sql",
			wantSecond:    "0001_second.up.sql",
			wantDirection: MigrationDirectionUp,
			wantMessage:   "duplicate up migration for version 1: 0001_first.up.sql and 0001_second.up.sql",
		},
		{
			name: "two down files",
			files: fstest.MapFS{
				"migrations/0002_first.up.sql":    &fstest.MapFile{Data: []byte("CREATE TABLE a (id int);")},
				"migrations/0002_first.down.sql":  &fstest.MapFile{Data: []byte("DROP TABLE a;")},
				"migrations/0002_second.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE b;")},
			},
			wantFirst:     "0002_first.down.sql",
			wantSecond:    "0002_second.down.sql",
			wantDirection: MigrationDirectionDown,
			wantMessage:   "duplicate down migration for version 2: 0002_first.down.sql and 0002_second.down.sql",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			migrator, err := New(&mockMigrationExecutor{}, testCase.files, "migrations")
			require.NoError(t, err)

			_, err = migrator.Status(t.Context())

			var duplicate *DuplicateMigrationVersionError
			require.ErrorAs(t, err, &duplicate)
			assert.Equal(t, testCase.wantFirst, duplicate.FirstFilename)
			assert.Equal(t, testCase.wantSecond, duplicate.SecondFilename)
			assert.Equal(t, testCase.wantDirection, duplicate.Direction)
			assert.EqualError(t, duplicate, testCase.wantMessage)
		})
	}
}

func TestSeederRejectsDuplicateVersions(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"seeds/001_first.sql":  &fstest.MapFile{Data: []byte("INSERT INTO a VALUES (1);")},
		"seeds/001_second.sql": &fstest.MapFile{Data: []byte("INSERT INTO a VALUES (2);")},
	}
	seeder, err := NewSeeder(&mockSeedExecutor{}, files, "seeds")
	require.NoError(t, err)

	_, err = seeder.Status(t.Context())

	var duplicate *DuplicateSeedVersionError
	require.ErrorAs(t, err, &duplicate)
	assert.Equal(t, "001_first.sql", duplicate.FirstFilename)
	assert.Equal(t, "001_second.sql", duplicate.SecondFilename)
	assert.Equal(t, int64(1), duplicate.Version)
	assert.EqualError(t, duplicate, "duplicate seed for version 1: 001_first.sql and 001_second.sql")
}
