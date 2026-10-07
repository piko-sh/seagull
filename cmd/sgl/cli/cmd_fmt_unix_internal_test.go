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
	"io"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestFmtSkipsANamedPipe(t *testing.T) {
	t.Parallel()

	directory := minimalProject(t)
	require.NoError(t, syscall.Mkfifo(filepath.Join(directory, "db", "queries", "pipe.sql"), 0o600))

	var status int
	scaling.Within(t, 5*time.Second, func(ctx context.Context) {
		status = MainContext(ctx, []string{"fmt", "-l"}, WithIO(strings.NewReader(""), io.Discard, io.Discard), WithDirectory(directory))
	})

	require.Equal(t, exitOK, status)
}
