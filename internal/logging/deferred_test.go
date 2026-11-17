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

package logging_test

import (
	"bytes"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/logging"
)

type lockedWriter struct {
	buffer *bytes.Buffer
	mu     *sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.Write(p)
}

func TestDeferredReplaysThroughTheCallersLoggerOnce(t *testing.T) {
	t.Parallel()
	cases := []struct {
		log         func(logger *slog.Logger)
		name        string
		level       slog.Level
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "attributes and groups survive",
			log: func(logger *slog.Logger) {
				logger.With(slog.String("engine", "sqlite")).WithGroup("query").Warn("rejected", slog.String("name", "ListAuthors"))
			},
			level:       slog.LevelInfo,
			wantContain: []string{"msg=rejected", "engine=sqlite", "query.name=ListAuthors"},
		},
		{
			name: "records keep their order",
			log: func(logger *slog.Logger) {
				logger.Warn("first")
				logger.Warn("second")
			},
			level:       slog.LevelInfo,
			wantContain: []string{"msg=first", "msg=second"},
		},
		{
			name: "the replaying handler filters levels",
			log: func(logger *slog.Logger) {
				logger.Debug("hidden")
				logger.Error("shown")
			},
			level:       slog.LevelWarn,
			wantContain: []string{"msg=shown"},
			wantAbsent:  []string{"hidden"},
		},
		{
			name:       "nothing kept logs nothing",
			log:        func(*slog.Logger) {},
			level:      slog.LevelInfo,
			wantAbsent: []string{"msg="},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			deferred := logging.NewDeferred()
			_, early := logging.From(deferred.Context())
			testCase.log(early)

			var logged bytes.Buffer
			ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: testCase.level})))
			deferred.Replay(ctx)
			for _, want := range testCase.wantContain {
				assert.Contains(t, logged.String(), want)
			}
			for _, absent := range testCase.wantAbsent {
				assert.NotContains(t, logged.String(), absent)
			}

			logged.Reset()
			deferred.Replay(ctx)
			assert.Empty(t, logged.String(), "a record is reported once")
		})
	}
}

func TestDeferredCountsRecordsPastItsBound(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		logged      int
		wantDropped string
	}{
		{name: "at the bound nothing is dropped", logged: 256, wantDropped: ""},
		{name: "past the bound the excess is counted", logged: 256 + 3, wantDropped: "dropped=3"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			deferred := logging.NewDeferred()
			_, early := logging.From(deferred.Context())
			for range testCase.logged {
				early.Warn("w")
			}
			var logged bytes.Buffer
			deferred.Replay(logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil))))
			if testCase.wantDropped == "" {
				assert.NotContains(t, logged.String(), "dropped=")
				return
			}
			assert.Contains(t, logged.String(), testCase.wantDropped)
		})
	}
}

func TestDeferredIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	deferred := logging.NewDeferred()
	_, early := logging.From(deferred.Context())
	var logged bytes.Buffer
	var mu sync.Mutex
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&lockedWriter{buffer: &logged, mu: &mu}, nil)))

	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			for range 16 {
				early.Warn("w")
				deferred.Replay(ctx)
			}
		})
	}
	group.Wait()
	deferred.Replay(ctx)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 8*16, bytes.Count(logged.Bytes(), []byte("msg=w")), "every record is replayed exactly once")
}
