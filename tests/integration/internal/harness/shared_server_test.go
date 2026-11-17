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

package harness_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/tests/integration/internal/harness"
)

const (
	childDeadline    = 10 * time.Second
	childWaitDelay   = time.Second
	childFailureCode = 7
)

var errChildDeadline = errors.New("the child test process did not finish in time")

type sharedLifecycleProbe struct {
	command *exec.Cmd
	input   io.WriteCloser
	output  *bufio.Reader
	cancel  context.CancelFunc
}

func TestSharedServerWorkersAttachWithoutOwningCleanup(t *testing.T) {
	const key = "SEAGULL_TEST_SERVER_FUZZPROBE"
	if os.Getenv("SEAGULL_FUZZ_SERVER_CHILD") == "1" {
		var endpoint string
		start := harness.SharedServer("fuzzprobe", &endpoint, func(context.Context) (func(context.Context) error, error) {
			require.Fail(t, "a worker started its own server")
			return nil, nil
		})
		stop, err := start(t.Context())
		require.NoError(t, err)
		require.Nil(t, stop)
		require.Equal(t, "disposable-endpoint", endpoint)
		return
	}
	t.Setenv(key, "")
	t.Setenv(key+"_OWNER", "")
	starts, stops := 0, 0
	var endpoint string
	start := harness.SharedServer("fuzzprobe", &endpoint, func(context.Context) (func(context.Context) error, error) {
		starts++
		endpoint = "disposable-endpoint"
		return func(context.Context) error { stops++; return nil }, nil
	})
	stop, err := start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		if stops == 0 {
			require.NoError(t, stop(context.Background()))
		}
	})
	for range 2 {
		runChildTest(t, "^TestSharedServerWorkersAttachWithoutOwningCleanup$", "SEAGULL_FUZZ_SERVER_CHILD=1")
	}
	require.Equal(t, 1, starts)
	require.Zero(t, stops)
	require.NoError(t, stop(t.Context()))
	require.Equal(t, 1, stops)
	require.Empty(t, os.Getenv(key))
	require.Empty(t, os.Getenv(key+"_OWNER"))
}

func TestSharedServerRejectsStaleOwnershipAndPreservesStartupErrors(t *testing.T) {
	const key = "SEAGULL_TEST_SERVER_FUZZPROBE"
	t.Setenv(key, "endpoint")
	t.Setenv(key+"_OWNER", strconv.Itoa(os.Getpid()))
	var endpoint string
	want := errors.New("startup failed")
	start := harness.SharedServer("fuzzprobe", &endpoint, func(context.Context) (func(context.Context) error, error) { return nil, want })
	_, err := start(t.Context())
	require.ErrorContains(t, err, "not inherited")
	require.Empty(t, endpoint)
	t.Setenv(key+"_OWNER", "")
	_, err = start(t.Context())
	require.ErrorIs(t, err, want)
	require.Empty(t, os.Getenv(key+"_OWNER"))
}

func TestSharedServerLifecycleProcess(t *testing.T) {
	if os.Getenv("SEAGULL_FUZZ_LIFECYCLE_CHILD") == "1" {
		sharedLifecycleChild(t)
		return
	}
	const key = "SEAGULL_TEST_SERVER_LIFECYCLE"
	t.Setenv(key, "")
	t.Setenv(key+"_OWNER", "")
	starts, stops := 0, 0
	var endpoint string
	starter := harness.SharedServer("lifecycle", &endpoint, func(context.Context) (func(context.Context) error, error) {
		starts++
		endpoint = "disposable-lifecycle-endpoint"
		return func(context.Context) error { stops++; return nil }, nil
	})
	for campaign := range 3 {
		stop, err := starter(t.Context())
		require.NoError(t, err)
		stopped := false
		t.Cleanup(func() {
			if !stopped {
				require.NoError(t, stop(context.Background()))
			}
		})
		first := startSharedLifecycleChild(t)
		second := startSharedLifecycleChild(t)
		_, err = fmt.Fprintln(first.input, "fail")
		require.NoError(t, err)
		require.Error(t, first.command.Wait())
		require.Equal(t, childFailureCode, first.command.ProcessState.ExitCode())
		require.Equal(t, campaign, stops)
		_, err = fmt.Fprintln(second.input, "ping")
		require.NoError(t, err)
		response, err := second.output.ReadString('\n')
		require.NoError(t, err)
		require.Equal(t, "alive\n", response)
		second.cancel()
		require.Error(t, second.command.Wait())
		require.NotNil(t, second.command.ProcessState)
		require.Equal(t, campaign, stops)
		require.Equal(t, strconv.Itoa(os.Getpid()), os.Getenv(key+"_OWNER"))
		require.NoError(t, stop(context.Background()))
		stopped = true
		require.Equal(t, campaign+1, starts)
		require.Equal(t, campaign+1, stops)
		require.Empty(t, os.Getenv(key))
		require.Empty(t, os.Getenv(key+"_OWNER"))
	}
}

func startSharedLifecycleChild(t *testing.T) sharedLifecycleProbe {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), childDeadline, errChildDeadline)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSharedServerLifecycleProcess$")
	command.Env = append(os.Environ(), "SEAGULL_FUZZ_LIFECYCLE_CHILD=1")
	command.WaitDelay = childWaitDelay
	input, err := command.StdinPipe()
	require.NoError(t, err)
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	command.Stderr = os.Stderr
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		cancel()
		_ = input.Close()
		_ = output.Close()
		if command.ProcessState == nil {
			_ = command.Wait()
		}
	})
	reader := bufio.NewReader(output)
	response, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "attached\n", response)
	return sharedLifecycleProbe{command: command, input: input, output: reader, cancel: cancel}
}

func sharedLifecycleChild(t *testing.T) {
	t.Helper()
	var endpoint string
	starter := harness.SharedServer("lifecycle", &endpoint, func(context.Context) (func(context.Context) error, error) {
		require.Fail(t, "worker tried to start a server")
		return nil, nil
	})
	stop, err := starter(t.Context())
	require.NoError(t, err)
	require.Nil(t, stop)
	require.Equal(t, "disposable-lifecycle-endpoint", endpoint)
	_, err = fmt.Fprintln(os.Stdout, "attached")
	require.NoError(t, err)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "fail":
			os.Exit(childFailureCode)
		case "ping":
			_, err = fmt.Fprintln(os.Stdout, "alive")
			require.NoError(t, err)
		default:
			require.Failf(t, "unknown request", "%q", scanner.Text())
		}
	}
	require.NoError(t, scanner.Err())
}

func TestSharedServerStartupCancellationDoesNotPublishOwnership(t *testing.T) {
	const key = "SEAGULL_TEST_SERVER_CANCELLED"
	t.Setenv(key, "")
	t.Setenv(key+"_OWNER", "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var endpoint string
	starter := harness.SharedServer("cancelled", &endpoint, func(ctx context.Context) (func(context.Context) error, error) {
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		return nil, ctx.Err()
	})
	stop, err := starter(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, stop)
	require.Empty(t, os.Getenv(key))
	require.Empty(t, os.Getenv(key+"_OWNER"))
}

func TestSharedServerStartupFailureProcess(t *testing.T) {
	const key = "SEAGULL_TEST_SERVER_FAILURE"
	t.Setenv(key, "")
	t.Setenv(key+"_OWNER", "")
	if os.Getenv("SEAGULL_FUZZ_STARTUP_CHILD") == "1" {
		var endpoint string
		expected := errors.New("deliberate startup failure")
		starter := harness.SharedServer("failure", &endpoint, func(context.Context) (func(context.Context) error, error) {
			return nil, expected
		})
		stop, err := starter(t.Context())
		require.ErrorIs(t, err, expected)
		require.Nil(t, stop)
		require.Empty(t, os.Getenv(key))
		require.Empty(t, os.Getenv(key+"_OWNER"))
		return
	}
	runChildTest(t, "^TestSharedServerStartupFailureProcess$", "SEAGULL_FUZZ_STARTUP_CHILD=1")
}

func runChildTest(t *testing.T, pattern, environment string) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), childDeadline, errChildDeadline)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run="+pattern)
	command.Env = append(os.Environ(), environment)
	command.WaitDelay = childWaitDelay
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
