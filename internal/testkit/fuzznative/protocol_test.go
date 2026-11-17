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

package fuzznative

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeProtocolPeer(t *testing.T) {
	mode := os.Getenv("SEAGULL_NATIVE_PROTOCOL_TEST")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		os.Exit(2)
	}
	var request Request
	if json.Unmarshal(scanner.Bytes(), &request) != nil {
		os.Exit(3)
	}
	response := Response{ID: request.ID, Version: ProtocolVersion, Error: request.Connection}
	switch mode {
	case "stderr":
		fmt.Fprintln(os.Stderr, request.Connection)
		response.ID++
	case "wrong ID":
		response.ID++
	case "wrong version":
		response.Version++
	case "malformed":
		fmt.Println("{")
		os.Exit(0)
	case "unknown":
		fmt.Println(`{"unknown":true}`)
		os.Exit(0)
	case "trailing":
		fmt.Println(`{} {}`)
		os.Exit(0)
	case "oversized":
		fmt.Println(strings.Repeat("x", protocolBound+1))
		os.Exit(0)
	case "exit":
		os.Exit(7)
	case "hang":

		time.Sleep(time.Minute)
		os.Exit(8)
	}
	if json.NewEncoder(os.Stdout).Encode(response) != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func startPeer(t *testing.T, mode string) *Process {
	t.Helper()
	binary, err := os.Executable()
	require.NoError(t, err)
	command := exec.Command(binary, "-test.run=^TestNativeProtocolPeer$")
	command.Env = append(os.Environ(), "SEAGULL_NATIVE_PROTOCOL_TEST="+mode)
	process, err := startCommand(command)
	require.NoError(t, err)
	t.Cleanup(func() { _ = process.Close() })
	return process
}

func TestNativeProtocolRejectsFaultyHelpers(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"valid", "stderr", "wrong ID", "wrong version", "malformed", "unknown", "trailing", "oversized", "exit", "hang"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			process := startPeer(t, mode)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			response, err := process.Exchange(ctx, "private-connection", []byte{12})
			if mode == "valid" {
				require.NoError(t, err)
				require.Equal(t, "[connection]", response.Error)
				return
			}
			require.Error(t, err, "faulty helper accepted")
			assert.NotContains(t, err.Error(), "private-connection", "connection leaked")
			_, err = process.Exchange(t.Context(), "private-connection", []byte{12})
			require.ErrorIs(t, err, errHelperFailed)
		})
	}
}

func TestNativeProtocolRefusesAClosedHelper(t *testing.T) {
	t.Parallel()
	process := startPeer(t, "valid")

	require.Error(t, process.Close())
	_, err := process.Exchange(t.Context(), "", []byte{12})
	require.ErrorIs(t, err, errHelperClosed)
	require.NoError(t, process.Close())
}

func TestNativeProtocolRefusesACancelledContext(t *testing.T) {
	t.Parallel()
	process := startPeer(t, "valid")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := process.Exchange(ctx, "", []byte{12})
	require.ErrorIs(t, err, context.Canceled)
}

func TestEncodeFrameRefusesOversizedValues(t *testing.T) {
	t.Parallel()
	_, err := encodeFrame(strings.Repeat("x", protocolBound))
	require.ErrorIs(t, err, errFrameTooLarge)
	_, err = encodeFrame(func() {})
	require.Error(t, err)
}

func TestNativeDiagnosticsRetainOnlyABoundedTail(t *testing.T) {
	t.Parallel()
	tail := new(diagnosticTail)
	count, err := tail.Write([]byte("short\n"))
	require.NoError(t, err)
	require.Equal(t, len("short\n"), count)
	require.Equal(t, "short\n", tail.String())

	prefix := "private-connection" + strings.Repeat("a", diagnosticBytes*2) + "\n"
	count, err = tail.Write([]byte(prefix))
	require.NoError(t, err)
	require.Equal(t, len(prefix), count)
	_, err = tail.Write([]byte("last"))
	require.NoError(t, err)
	require.LessOrEqual(t, len(tail.retained), diagnosticBytes)
	require.Equal(t, diagnosticDropped+"last", tail.String(), "a cut line must not survive")
}
