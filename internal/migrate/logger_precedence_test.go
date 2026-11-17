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

package migrate_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/migrate"
)

func TestACallLogsThroughTheContextLoggerBeforeWithLogger(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		bindContext   bool
		setOption     bool
		wantInContext bool
		wantInOption  bool
	}{
		{name: "the context logger wins over the option", bindContext: true, setOption: true, wantInContext: true, wantInOption: false},
		{name: "the option is used when the context has none", bindContext: false, setOption: true, wantInContext: false, wantInOption: true},
		{name: "the context logger is used without the option", bindContext: true, setOption: false, wantInContext: true, wantInOption: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var contextLogs, optionLogs bytes.Buffer
			var options []migrate.Option
			if testCase.setOption {
				options = append(options, migrate.WithLogger(slog.New(slog.NewTextHandler(&optionLogs, nil))))
			}
			store := newHistoryStore(appliedRow(1, "users", createUsers))
			migrator := newRecoveryMigrator(t, store, threeMigrations(), options...)

			ctx := t.Context()
			if testCase.bindContext {
				ctx = logging.WithLogger(ctx, slog.New(slog.NewTextHandler(&contextLogs, nil)))
			}
			require.NoError(t, migrator.Force(ctx, 2))

			const forced = "forced migration as applied without running it"
			assert.Equal(t, testCase.wantInContext, bytes.Contains(contextLogs.Bytes(), []byte(forced)), contextLogs.String())
			assert.Equal(t, testCase.wantInOption, bytes.Contains(optionLogs.Bytes(), []byte(forced)), optionLogs.String())
		})
	}
}
