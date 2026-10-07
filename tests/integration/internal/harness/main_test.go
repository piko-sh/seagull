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
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	serverWait     = 5 * time.Second
	serverDeadline = 20 * time.Millisecond
)

var errServerRefused = errors.New("the container refused to start")

func waitForContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(serverWait):
		return errServerRefused
	}
}

func TestStartServerBoundsTheStartAndNamesTheTimeLimit(t *testing.T) {
	t.Parallel()

	stopped := func(context.Context) error { return nil }
	tests := []struct {
		start     Starter
		wantErr   error
		name      string
		wantCause bool
		wantStop  bool
	}{
		{name: "a suite without a server", start: nil, wantStop: false},
		{name: "a server that starts", start: func(context.Context) (func(context.Context) error, error) { return stopped, nil }, wantStop: true},
		{name: "a server that fails", start: func(context.Context) (func(context.Context) error, error) { return nil, errServerRefused },
			wantErr: errServerRefused},
		{name: "a server that never becomes ready", start: func(ctx context.Context) (func(context.Context) error, error) {
			return nil, waitForContext(ctx)
		}, wantErr: context.DeadlineExceeded, wantCause: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stop, err := startServer(test.start, serverDeadline)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				require.Equal(t, test.wantCause, errors.Is(err, errStartupTimeout))
				require.Nil(t, stop)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantStop, stop != nil)
		})
	}
}

func TestStopServerBoundsTheStopAndNamesTheTimeLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		stop      func(ctx context.Context) error
		wantErr   error
		name      string
		wantCause bool
	}{
		{name: "a suite without a server", stop: nil},
		{name: "a server that stops", stop: func(context.Context) error { return nil }},
		{name: "a server that fails to stop", stop: func(context.Context) error { return errServerRefused }, wantErr: errServerRefused},
		{name: "a server that never stops", stop: waitForContext, wantErr: context.DeadlineExceeded, wantCause: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := stopServer(test.stop, serverDeadline)
			if test.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, test.wantErr)
			require.Equal(t, test.wantCause, errors.Is(err, errShutdownTimeout))
		})
	}
}
