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

//go:build js && wasm

package main

import (
	"bytes"
	"encoding/json"
	"syscall/js"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/wasm/internal/bridge"
	"seagull.sh/seagull/cmd/wasm/internal/session"
)

func TestSelfTestPassesUnderJavaScript(t *testing.T) {
	var stdout, stderr bytes.Buffer

	status := bridge.SelfTest(t.Context(), &stdout, &stderr)

	require.Zero(t, status, stderr.String())
	var response session.Response
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &response))
	require.True(t, response.OK, response.Error)
}

func TestTheRegisteredBridgeAnswersInJSON(t *testing.T) {
	register()
	exported := js.Global().Get(globalName)
	repeat := js.Global().Get("String").Get("prototype").Get("repeat")

	testCases := []struct {
		argument js.Value
		name     string
		function string
		want     string
	}{
		{
			name: "a code is read from a JavaScript string", function: "explain", argument: js.ValueOf("q1"),
			want: `"code":"Q001"`,
		},
		{
			name: "a string longer than a request is measured, not copied", function: "generate",
			argument: repeat.Call("call", "x", 16<<20),
			want:     `"error":"the request is too long: the playground reads at most 2162688 bytes"`,
		},
		{
			name: "an argument that is not a string reads as empty", function: "starter", argument: js.ValueOf(7),
			want: `"error":"unknown dialect`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := exported.Call(testCase.function, testCase.argument)

			require.Equal(t, js.TypeString, result.Type())
			require.Contains(t, result.String(), testCase.want)
		})
	}
}

func TestEveryBridgeFunctionIsRegistered(t *testing.T) {
	register()
	exported := js.Global().Get(globalName)

	for _, name := range bridge.Names() {
		require.Equal(t, js.TypeFunction, exported.Get(name).Type(), name)
	}
}
