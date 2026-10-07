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
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"go.uber.org/goleak"

	"seagull.sh/seagull/internal/leakcheck"
)

const (
	// startupTimeout bounds starting a suite's database server, image pull included, so a
	// server that never becomes ready fails the run with the reason.
	startupTimeout = 10 * time.Minute

	// shutdownTimeout bounds stopping a suite's database server.
	shutdownTimeout = 2 * time.Minute
)

// Starter starts a suite's database server under ctx, which ends once the startup time
// limit has passed, and returns the function that stops it under a context of its own.
type Starter func(ctx context.Context) (stop func(ctx context.Context) error, err error)

// Main runs a suite's tests, for its TestMain: it starts the suite's database server when
// it has one, runs the tests, stops the server and, when every test passed, fails the run
// on a leaked goroutine. A suite whose tests hold real sockets passes
// leakcheck.IgnoreNetworkPollers among options; a suite that opens only in-process
// databases passes none.
//
// Takes m (*testing.M) which runs the tests.
// Takes start (Starter) which starts the server, or is nil for a suite without a server.
// Takes options (...goleak.Option) which are the suite's extra leak-check ignore rules.
func Main(m *testing.M, start Starter, options ...goleak.Option) {
	flag.Parse()

	stop, err := startServer(start, startupTimeout)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "starting the suite's database server: %v\n", err)
		os.Exit(1)
	}
	exitCode := m.Run()
	if err := stopServer(stop, shutdownTimeout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "stopping the suite's database server: %v\n", err)
	}

	if exitCode == 0 {
		if err := leakcheck.FindLeaks(options...); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "goleak: %v\n", err)
			exitCode = 1
		}
	}
	os.Exit(exitCode)
}

// startServer starts a suite's server under a context that ends with errStartupTimeout as
// its cause once timeout has passed. The cause is part of the error when the start fails
// after the context has ended.
//
// Takes start (Starter) which starts the server, or is nil.
// Takes timeout (time.Duration) which bounds the start.
//
// Returns func(ctx context.Context) error which stops the server, or is nil when there is
// none.
// Returns error when the server does not start.
func startServer(start Starter, timeout time.Duration) (func(ctx context.Context) error, error) {
	if start == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeoutCause(context.Background(), timeout, errStartupTimeout)
	defer cancel()
	stop, err := start(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %w", context.Cause(ctx), err)
		}
		return nil, err
	}
	return stop, nil
}

// stopServer stops a suite's server under a context that ends with errShutdownTimeout as
// its cause once timeout has passed.
//
// Takes stop (func(ctx context.Context) error) which stops the server, or is nil.
// Takes timeout (time.Duration) which bounds the stop.
//
// Returns error when the server does not stop, joined with the cause when the context
// ended first.
func stopServer(stop func(ctx context.Context) error, timeout time.Duration) error {
	if stop == nil {
		return nil
	}
	ctx, cancel := context.WithTimeoutCause(context.Background(), timeout, errShutdownTimeout)
	defer cancel()
	if err := stop(ctx); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %w", context.Cause(ctx), err)
		}
		return err
	}
	return nil
}
