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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChecksumMismatchError_Error(t *testing.T) {
	t.Parallel()

	err := &ChecksumMismatchError{
		Version:         1,
		Name:            "create_users",
		AppliedChecksum: "abc123",
		FileChecksum:    "def456",
	}

	message := err.Error()
	assert.Contains(t, message, "1")
	assert.Contains(t, message, "create_users")
	assert.Contains(t, message, "abc123")
	assert.Contains(t, message, "def456")
	assert.Equal(t,
		"checksum mismatch for migration 1 (create_users): applied=abc123 file=def456",
		message,
	)
}

func TestDownChecksumMismatchError_Error(t *testing.T) {
	t.Parallel()

	err := &DownChecksumMismatchError{
		Version:          2,
		Name:             "add_index",
		RecordedChecksum: "aaa111",
		FileChecksum:     "bbb222",
	}

	message := err.Error()
	assert.Contains(t, message, "2")
	assert.Contains(t, message, "add_index")
	assert.Contains(t, message, "aaa111")
	assert.Contains(t, message, "bbb222")
	assert.Equal(t,
		"down checksum mismatch for migration 2 (add_index): recorded=aaa111 file=bbb222",
		message,
	)
}

func TestMigrationExecutionError_Error(t *testing.T) {
	t.Parallel()

	cause := errors.New("syntax error near SELECT")

	tests := []struct {
		name           string
		direction      MigrationDirection
		expectedPrefix string
	}{
		{
			name:           "direction up starts with migration",
			direction:      MigrationDirectionUp,
			expectedPrefix: "migration",
		},
		{
			name:           "direction down starts with rollback",
			direction:      MigrationDirectionDown,
			expectedPrefix: "rollback",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			executionError := &MigrationExecutionError{
				Version:   5,
				Name:      "add_column",
				Direction: testCase.direction,
				Cause:     cause,
			}

			message := executionError.Error()
			expected := fmt.Sprintf("%s 5 (add_column): syntax error near SELECT", testCase.expectedPrefix)
			assert.Equal(t, expected, message)
		})
	}
}

func TestMigrationExecutionError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection refused")
	err := &MigrationExecutionError{
		Version:   3,
		Name:      "create_index",
		Direction: MigrationDirectionUp,
		Cause:     cause,
	}

	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, cause), "errors.Is should match the wrapped cause")
}

func TestLockAcquisitionError_Error(t *testing.T) {
	t.Parallel()

	cause := errors.New("timeout exceeded")
	err := &LockAcquisitionError{
		Cause: cause,
	}

	expected := "acquiring migration lock: timeout exceeded"
	assert.Equal(t, expected, err.Error())
}

func TestLockAcquisitionErrorNamesTheLockItCouldNotTake(t *testing.T) {
	t.Parallel()

	cause := errors.New("lock unavailable")
	tests := []struct {
		name string
		lock string
		want string
	}{
		{name: "an unnamed lock is the migration lock", lock: "", want: "acquiring migration lock: lock unavailable"},
		{name: "the migration lock", lock: lockMigration, want: "acquiring migration lock: lock unavailable"},
		{name: "the seed lock", lock: lockSeed, want: "acquiring seed lock: lock unavailable"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := &LockAcquisitionError{Cause: cause, Lock: testCase.lock}
			assert.EqualError(t, err, testCase.want)
			assert.ErrorIs(t, err, cause)
		})
	}
}

func TestLockAcquisitionError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection reset")
	err := &LockAcquisitionError{
		Cause: cause,
	}

	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, cause), "errors.Is should match the wrapped cause")
}

func TestMissingMigrationFileError_Error(t *testing.T) {
	t.Parallel()

	err := &MissingMigrationFileError{
		Version: 7,
		Name:    "drop_legacy_table",
	}

	message := err.Error()
	assert.Contains(t, message, "7")
	assert.Contains(t, message, "drop_legacy_table")
	assert.Equal(t,
		"applied migration 7 (drop_legacy_table) has no corresponding file on disk",
		message,
	)
}

func TestNoDownMigrationError_Error(t *testing.T) {
	t.Parallel()

	err := &NoDownMigrationError{
		Version: 12,
	}

	expected := "no down migration for version 12"
	assert.Equal(t, expected, err.Error())
	assert.Contains(t, err.Error(), "12")
}

func TestNoDownMigrationError_Is(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		target   error
		expected bool
	}{
		{
			name:     "matches ErrNoDownMigration sentinel",
			target:   ErrNoDownMigration,
			expected: true,
		},
		{
			name:     "does not match unrelated sentinel",
			target:   ErrLockNotAcquired,
			expected: false,
		},
		{
			name:     "does not match arbitrary error",
			target:   errors.New("something else entirely"),
			expected: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			noDownError := &NoDownMigrationError{Version: 99}
			assert.Equal(t, testCase.expected, errors.Is(noDownError, testCase.target))
		})
	}
}

func TestMigrationSentinelErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		err             error
		expectedMessage string
	}{
		{
			name:            "ErrLockNotAcquired has correct message",
			err:             ErrLockNotAcquired,
			expectedMessage: "migration lock is already held",
		},
		{
			name:            "ErrNoDownMigration has correct message",
			err:             ErrNoDownMigration,
			expectedMessage: "no down migration file",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.NotNil(t, testCase.err)
			assert.Equal(t, testCase.expectedMessage, testCase.err.Error())

			wrapped := fmt.Errorf("wrapped: %w", testCase.err)
			assert.True(t, errors.Is(wrapped, testCase.err))
		})
	}
}
