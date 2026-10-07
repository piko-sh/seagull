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

package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/wasm/internal/session"
	"seagull.sh/seagull/internal/testkit/scaling"
)

type textArgument struct {
	text string
}

func (textArgument) IsString() bool { return true }

func (argument textArgument) Length() int {
	length := 0
	for _, character := range argument.text {
		length += utf16.RuneLen(character)
	}
	return length
}

func (argument textArgument) String() string { return argument.text }

type measuredArgument struct {
	t      *testing.T
	length int
}

func (measuredArgument) IsString() bool { return true }

func (argument measuredArgument) Length() int { return argument.length }

func (argument measuredArgument) String() string {
	assert.Fail(argument.t, "the argument was copied although it is longer than its function reads")
	return ""
}

type numberArgument struct{}

func (numberArgument) IsString() bool { return false }

func (numberArgument) Length() int { return 0 }

func (numberArgument) String() string { return "<number: 7>" }

type panickingArgument struct{}

func (panickingArgument) IsString() bool { panic("injected failure") }

func (panickingArgument) Length() int { panic("injected failure") }

func (panickingArgument) String() string { panic("injected failure") }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("the stream is closed") }

func decode[T any](t *testing.T, encoded string) T {
	t.Helper()
	var decoded T
	require.NoError(t, json.Unmarshal([]byte(encoded), &decoded), encoded)
	return decoded
}

func generateRequest(t *testing.T, files map[string]string) string {
	t.Helper()
	encoded, err := json.Marshal(session.Request{Files: files, Override: session.Override{}, V: session.ProtocolVersion})
	require.NoError(t, err)
	return string(encoded)
}

func TestNamesListsEveryFunction(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"dialects", "explain", "format", "generate", "init", "starter", "version"}, Names())
}

func TestCallAnswersEachFunctionInJSON(t *testing.T) {
	t.Parallel()

	starter, err := session.Starter(session.DefaultDialect)
	require.NoError(t, err)
	testCases := []struct {
		check     func(t *testing.T, encoded string)
		name      string
		function  string
		arguments []Argument
	}{
		{
			name: "version", function: "version", arguments: nil,
			check: func(t *testing.T, encoded string) {
				require.NotEmpty(t, decode[session.VersionView](t, encoded).GoVersion)
			},
		},
		{
			name: "init", function: "init", arguments: nil,
			check: func(t *testing.T, encoded string) {
				view := decode[initView](t, encoded)
				require.Equal(t, session.DefaultDialect, view.DefaultDialect)
				require.Equal(t, limitsView{MaxFiles: session.MaxFiles, MaxFileBytes: session.MaxFileBytes, MaxTotalBytes: session.MaxTotalBytes}, view.Limits)
				require.Equal(t, session.ProtocolVersion, view.Protocol)
				require.NotEmpty(t, view.Dialects)
			},
		},
		{
			name: "dialects", function: "dialects", arguments: nil,
			check: func(t *testing.T, encoded string) {
				require.NotEmpty(t, decode[[]session.DialectView](t, encoded))
			},
		},
		{
			name: "generate", function: "generate", arguments: []Argument{textArgument{text: generateRequest(t, starter)}},
			check: func(t *testing.T, encoded string) {
				response := decode[session.Response](t, encoded)
				require.True(t, response.OK, response.Error)
				require.NotEmpty(t, response.Files)
			},
		},
		{
			name: "generate without a request", function: "generate", arguments: nil,
			check: func(t *testing.T, encoded string) {
				require.Equal(t, failureView{Error: "the request is not valid JSON: unexpected end of JSON input", Internal: false}, decode[failureView](t, encoded))
			},
		},
		{
			name: "generate with a request that is not a string", function: "generate", arguments: []Argument{numberArgument{}},
			check: func(t *testing.T, encoded string) {
				require.Equal(t, failureView{Error: "the request is not valid JSON: unexpected end of JSON input", Internal: false}, decode[failureView](t, encoded))
			},
		},
		{
			name: "explain", function: "explain", arguments: []Argument{textArgument{text: "q1"}},
			check: func(t *testing.T, encoded string) {
				require.Equal(t, "Q001", decode[session.ExplainView](t, encoded).Code)
			},
		},
		{
			name: "explain an unknown code", function: "explain", arguments: []Argument{textArgument{text: "Q999"}},
			check: func(t *testing.T, encoded string) {
				require.Equal(t, failureView{Error: `unknown diagnostic code "Q999"`, Internal: false}, decode[failureView](t, encoded))
			},
		},
		{
			name: "format", function: "format", arguments: []Argument{textArgument{text: `{"source":"--  seagull.query( GetUser ,one )\nSELECT 1;\n"}`}},
			check: func(t *testing.T, encoded string) {
				require.Equal(t, session.FormatResponse{Source: "-- sgl.query(GetUser, one)\nSELECT 1;\n", Error: ""}, decode[session.FormatResponse](t, encoded))
			},
		},
		{
			name: "format a request that is not JSON", function: "format", arguments: []Argument{textArgument{text: "{"}},
			check: func(t *testing.T, encoded string) {
				require.Equal(t, session.FormatResponse{Source: "", Error: "the request is not valid JSON: unexpected end of JSON input"}, decode[session.FormatResponse](t, encoded))
			},
		},
		{
			name: "starter", function: "starter", arguments: []Argument{textArgument{text: "postgres"}},
			check: func(t *testing.T, encoded string) {
				require.Contains(t, decode[starterView](t, encoded).Files, "sgl.yaml")
			},
		},
		{
			name: "starter for an unknown dialect", function: "starter", arguments: []Argument{textArgument{text: "oracle"}},
			check: func(t *testing.T, encoded string) {
				view := decode[failureView](t, encoded)
				require.False(t, view.Internal)
				require.Contains(t, view.Error, "unknown dialect")
			},
		},
		{
			name: "a function the bridge does not have", function: "migrate", arguments: nil,
			check: func(t *testing.T, encoded string) {
				require.Equal(t, failureView{Error: `the playground has no such function: "migrate"`, Internal: true}, decode[failureView](t, encoded))
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			testCase.check(t, Call(t.Context(), testCase.function, testCase.arguments))
		})
	}
}

func TestCallRefusesAnArgumentLongerThanItsFunctionReads(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		function string
		length   int
		want     string
	}{
		{
			name: "a generate request", function: "generate", length: maxRequestBytes + 1,
			want: `{"error":"the request is too long: the playground reads at most 2162688 bytes","internal":false}`,
		},
		{
			name: "a format request", function: "format", length: maxFormatRequestBytes + 1,
			want: `{"source":"","error":"the request is too long: the playground reads at most 589824 bytes"}`,
		},
		{
			name: "a diagnostic code", function: "explain", length: maxNameBytes + 1,
			want: `{"error":"the code is too long: the playground reads at most 256 bytes","internal":false}`,
		},
		{
			name: "a dialect name", function: "starter", length: maxNameBytes + 1,
			want: `{"error":"the dialect is too long: the playground reads at most 256 bytes","internal":false}`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			encoded := Call(t.Context(), testCase.function, []Argument{measuredArgument{t: t, length: testCase.length}})

			require.JSONEq(t, testCase.want, encoded)
		})
	}
}

func TestTextRefusesTextLongerInBytesThanItsLimit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		argument textArgument
		limit    int
		want     string
		refused  bool
	}{
		{name: "text at its limit", argument: textArgument{text: "abcd"}, limit: 4, want: "abcd", refused: false},
		{name: "text one byte over its limit", argument: textArgument{text: "abcde"}, limit: 4, want: "", refused: true},
		{name: "text short in code units but long in bytes", argument: textArgument{text: "ééé"}, limit: 4, want: "", refused: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := text([]Argument{testCase.argument}, 0, testCase.limit)

			require.Equal(t, testCase.want, got)
			if testCase.refused {
				require.ErrorIs(t, err, errTooLong)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCallRefusesAnOversizedRequestWithoutReadingIt(t *testing.T) {
	t.Parallel()

	request := textArgument{text: strings.Repeat("x", 2*maxRequestBytes)}

	scaling.Within(t, 50*time.Millisecond, func(ctx context.Context) {
		encoded := Call(ctx, "generate", []Argument{request})
		require.Contains(t, encoded, "the request is too long")
	})
}

func TestCallRecoversAPanicAndLogsItsStackOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		function string
	}{
		{name: "generate", function: "generate"},
		{name: "explain", function: "explain"},
		{name: "format", function: "format"},
		{name: "starter", function: "starter"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var logged bytes.Buffer
			ctx := seagull.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logged, nil)))

			encoded := Call(ctx, testCase.function, []Argument{panickingArgument{}})

			require.Equal(t, failureView{Error: "internal error: injected failure", Internal: true}, decode[failureView](t, encoded))
			lines := strings.Split(strings.TrimSpace(logged.String()), "\n")
			require.Len(t, lines, 1)
			record := decode[map[string]any](t, lines[0])
			assert.Equal(t, "WARN", record["level"])
			assert.Equal(t, callOperation+testCase.function, record["operation"])
			assert.Contains(t, record["stack"], "bridge.Call")
		})
	}
}

func TestEncodeReportsAValueItCannotEncode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		value any
		name  string
		want  string
	}{
		{name: "a failure", value: failureView{Error: "no", Internal: false}, want: `{"error":"no","internal":false}`},
		{name: "a channel", value: make(chan int), want: `{"error":"json: unsupported type: chan int","internal":true}`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.JSONEq(t, testCase.want, encode(testCase.value))
		})
	}
}

func TestSelfTestGeneratesTheStarterProject(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	status := SelfTest(t.Context(), &stdout, &stderr)

	require.Zero(t, status, stderr.String())
	require.Empty(t, stderr.String())
	response := decode[session.Response](t, stdout.String())
	require.True(t, response.OK, response.Error)
	require.Zero(t, response.Counts.Errors)
}

func TestSelfTestFailsWhenItCannotWriteTheResponse(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	status := SelfTest(t.Context(), failingWriter{}, &stderr)

	require.Equal(t, 1, status)
	require.Contains(t, stderr.String(), "the stream is closed")
}
