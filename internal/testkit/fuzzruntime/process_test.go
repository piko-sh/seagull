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

package fuzzruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const exchangeTimeout = 30 * time.Second

const silentTimeout = time.Second

var errExchangeTimedOut = errors.New("exchange timed out")

func TestProtocolHelper(t *testing.T) {
	mode := os.Getenv("SEAGULL_FUZZ_PROTOCOL_HELPER")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID   uint64
			Data []byte
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		switch mode {
		case "silent":
			continue
		case "crash":
			os.Exit(3)
		case "mismatch":
			request.ID++
		case "oversized":
			fmt.Printf("{\"Panic\":%q}\n", strings.Repeat("x", maxResponseBytes))
			continue
		}
		if err := json.NewEncoder(os.Stdout).Encode(Response{ID: request.ID}); err != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}

func TestProcessRejectsBrokenProtocolsAndReapsChildren(t *testing.T) {
	for _, mode := range []string{"valid", "silent", "crash", "mismatch", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestProtocolHelper$")
			command.Env = append(os.Environ(), "SEAGULL_FUZZ_PROTOCOL_HELPER="+mode)
			input, err := command.StdinPipe()
			require.NoError(t, err)
			output, err := command.StdoutPipe()
			require.NoError(t, err)
			require.NoError(t, command.Start())
			process := &Process{command: command, input: input, output: output}
			t.Cleanup(func() { _ = process.Close(); require.NotNil(t, command.ProcessState) })
			timeout := exchangeTimeout
			if mode == "silent" {
				timeout = silentTimeout
			}
			ctx, cancel := context.WithTimeoutCause(t.Context(), timeout, errExchangeTimedOut)
			defer cancel()
			_, err = process.Exchange(ctx, []byte{1})
			switch mode {
			case "valid":
				require.NoError(t, err)
				_, err = process.Exchange(ctx, []byte{2})
				require.NoError(t, err)
			case "silent":
				require.ErrorIs(t, err, errExchangeTimedOut)
			case "mismatch":
				require.ErrorContains(t, err, "does not match")
			default:
				require.Error(t, err)
			}
			if mode == "oversized" {
				require.ErrorContains(t, err, "exceeds")
			}
		})
	}
}

func TestProcessStartFailure(t *testing.T) {
	process, err := Start(t.TempDir()+"/missing", os.Stderr)
	require.Nil(t, process)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestProcessExchangeHonoursAnEndedContext(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestProtocolHelper$")
	command.Env = append(os.Environ(), "SEAGULL_FUZZ_PROTOCOL_HELPER=valid")
	input, err := command.StdinPipe()
	require.NoError(t, err)
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	process := &Process{command: command, input: input, output: output, sequence: 0}
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errExchangeTimedOut)
	_, err = process.Exchange(ctx, []byte{1})
	require.ErrorIs(t, err, errExchangeTimedOut)
	require.Zero(t, process.sequence)
	require.NoError(t, process.Close())
}
