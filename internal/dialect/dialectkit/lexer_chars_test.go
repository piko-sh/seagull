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

package dialectkit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

func TestIsIdentStart(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		character rune
		want      bool
	}{
		{character: 'a', want: true},
		{character: 'z', want: true},
		{character: 'A', want: true},
		{character: 'Z', want: true},
		{character: '_', want: true},
		{character: 'é', want: true},
		{character: '名', want: true},
		{character: '0', want: false},
		{character: '9', want: false},
		{character: ' ', want: false},
		{character: '-', want: false},
		{character: '.', want: false},
		{character: '$', want: false},
	}
	for _, testCase := range testCases {
		assert.Equalf(t, testCase.want, dialectkit.IsIdentStart(testCase.character), "IsIdentStart(%q)", testCase.character)
	}
}

func TestIsIdentPart(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		character rune
		want      bool
	}{
		{character: 'a', want: true},
		{character: '_', want: true},
		{character: '0', want: true},
		{character: '9', want: true},
		{character: 'é', want: true},
		{character: '名', want: true},
		{character: ' ', want: false},
		{character: '-', want: false},
		{character: '.', want: false},
		{character: '$', want: false},
	}
	for _, testCase := range testCases {
		assert.Equalf(t, testCase.want, dialectkit.IsIdentPart(testCase.character), "IsIdentPart(%q)", testCase.character)
	}
}

func TestDigitClassifiers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		classify  func(byte) bool
		name      string
		character byte
		want      bool
	}{
		{name: "IsDigit", classify: dialectkit.IsDigit, character: '0', want: true},
		{name: "IsDigit", classify: dialectkit.IsDigit, character: '5', want: true},
		{name: "IsDigit", classify: dialectkit.IsDigit, character: '9', want: true},
		{name: "IsHexDigit", classify: dialectkit.IsHexDigit, character: '0', want: true},
		{name: "IsHexDigit", classify: dialectkit.IsHexDigit, character: '9', want: true},
		{name: "IsHexDigit", classify: dialectkit.IsHexDigit, character: 'a', want: true},
		{name: "IsHexDigit", classify: dialectkit.IsHexDigit, character: 'f', want: true},
		{name: "IsHexDigit", classify: dialectkit.IsHexDigit, character: 'A', want: true},
		{name: "IsHexDigit", classify: dialectkit.IsHexDigit, character: 'F', want: true},
		{name: "IsHexDigit", classify: dialectkit.IsHexDigit, character: 'g', want: false},
		{name: "IsHexDigit", classify: dialectkit.IsHexDigit, character: 'G', want: false},
		{name: "IsHexDigit", classify: dialectkit.IsHexDigit, character: ' ', want: false},
		{name: "IsOctalDigit", classify: dialectkit.IsOctalDigit, character: '0', want: true},
		{name: "IsOctalDigit", classify: dialectkit.IsOctalDigit, character: '7', want: true},
		{name: "IsOctalDigit", classify: dialectkit.IsOctalDigit, character: '8', want: false},
		{name: "IsBinaryDigit", classify: dialectkit.IsBinaryDigit, character: '0', want: true},
		{name: "IsBinaryDigit", classify: dialectkit.IsBinaryDigit, character: '1', want: true},
		{name: "IsBinaryDigit", classify: dialectkit.IsBinaryDigit, character: '2', want: false},
	}
	for _, testCase := range testCases {
		assert.Equalf(t, testCase.want, testCase.classify(testCase.character), "%s(%q)", testCase.name, testCase.character)
	}
}
