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

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/preset"
)

func TestDefaultPresetMatchesDefaultSettings(t *testing.T) {
	t.Parallel()

	defaults := DefaultPreset()

	assert.Equal(t, preset.DefaultSettings(), defaults.settings())
}

func TestPresetSettingsCarryEveryField(t *testing.T) {
	t.Parallel()

	candidate := DefaultPreset()
	candidate.Directives.RequireCanonical = true
	candidate.QueryNames.Enabled = true
	candidate.Types.JSONScanInto = Slot{NotNull: GoType{Package: "example.com/json", Name: "ScanInto"}}
	candidate.SQLConstantSuffix = "Query"

	settings := candidate.settings()

	assert.Equal(t, "sgl", settings.Prefixes.Canonical)
	assert.Equal(t, []string{"seagull"}, settings.Prefixes.Aliases)
	assert.Equal(t, candidate.Directives.Foreign, settings.Prefixes.Foreign)
	assert.True(t, settings.RequireCanonical)
	assert.Equal(t, candidate.Header, settings.Style.Header)
	assert.Equal(t, candidate.HelperPrefix, settings.Style.HelperPrefix)
	assert.Equal(t, candidate.ErrorPrefix, settings.Style.ErrorPrefix)
	assert.Equal(t, candidate.QueryNames, settings.Style.QueryNames)
	assert.Equal(t, "Query", settings.Style.SQLConstantSuffix)
	assert.Equal(t, candidate.Types.JSONScanInto.NotNull, settings.Style.JSONScanInto)
	assert.Equal(t, candidate.Types, settings.Types)
}

func TestPresetSettingsCarryKeepStars(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		keepStars bool
	}{
		{name: "the default expands stars", keepStars: false},
		{name: "a preset keeping stars", keepStars: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			candidate := DefaultPreset()
			candidate.KeepStars = testCase.keepStars

			require.Equal(t, testCase.keepStars, candidate.settings().Style.KeepStars)
		})
	}
}

func TestPresetSettingsShareNoSlices(t *testing.T) {
	t.Parallel()

	candidate := DefaultPreset()
	settings := candidate.settings()
	candidate.Directives.Aliases[0] = "changed"
	candidate.Directives.Foreign[0] = "changed"

	require.Equal(t, []string{"seagull"}, settings.Prefixes.Aliases)
	assert.Equal(t, preset.DefaultSettings().Prefixes.Foreign, settings.Prefixes.Foreign)
}

func TestPresetSettingsCarryModernIdioms(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		modernIdioms bool
	}{
		{name: "a preset reproducing older code leaves them off", modernIdioms: false},
		{name: "a preset writing current Go turns them on", modernIdioms: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			candidate := DefaultPreset()
			candidate.ModernIdioms = testCase.modernIdioms

			require.Equal(t, testCase.modernIdioms, candidate.settings().Style.ModernIdioms)
		})
	}
	assert.Equal(t, preset.DefaultSettings().Style.ModernIdioms, DefaultPreset().ModernIdioms)
}

func TestPresetSettingsCarryWrapErrors(t *testing.T) {
	testCases := []struct {
		name       string
		wrapErrors bool
	}{
		{name: "on", wrapErrors: true},
		{name: "off", wrapErrors: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := DefaultPreset()
			candidate.WrapErrors = testCase.wrapErrors
			require.Equal(t, testCase.wrapErrors, candidate.settings().Style.WrapErrors)
		})
	}
	assert.False(t, DefaultPreset().WrapErrors)
	assert.Equal(t, preset.DefaultSettings().Style.WrapErrors, DefaultPreset().WrapErrors)
}
