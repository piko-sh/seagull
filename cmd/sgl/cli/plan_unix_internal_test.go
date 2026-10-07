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

//go:build unix

package cli

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestStaleFilesNeverOpensANamedPipe(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(directory, "pipe.go"), 0o600))

	var changes []fileChange
	var err error
	scaling.Within(t, time.Second, func(context.Context) {
		changes, err = planChanges(directory, nil, []string{generatedHeader}, false)
	})

	require.NoError(t, err)
	require.Empty(t, changes)
}
