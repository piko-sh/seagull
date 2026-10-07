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

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/sdk/emit"
)

func TestGoStringLiteralQuotesOnlyWhatARawStringCannotHold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "lines", value: "SELECT 1\nFROM t", want: "`SELECT 1\nFROM t`"},
		{name: "a backquote", value: "SELECT '`'", want: "\"SELECT '`'\""},
		{name: "a carriage return", value: "SELECT 1\r\n", want: "\"SELECT 1\\r\\n\""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, goStringLiteral(testCase.value))
		})
	}
}

func TestGeneratedGoPrintsTheMethodAndOnlyTheConstantItSends(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		want   string
		files  []emit.GeneratedFile
	}{
		{name: "no generated code", method: "GetAuthor", files: nil, want: ""},
		{
			name:   "a method that sends no constant",
			method: "Ping",
			files:  []emit.GeneratedFile{{Name: "ping.go", Content: []byte("package store\n\nfunc (q *Queries) Ping() error { return nil }\n")}},
			want:   "func (q *Queries) Ping() error",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, generatedGo(assemble.NewGoIndex(testCase.files), testCase.method))
		})
	}
}
