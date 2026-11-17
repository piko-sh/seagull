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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/internal/testkit/fuzznative"
)

const (
	// runtimeModeByte locates the shape and mode choices of a version 12 input.
	runtimeModeByte = 4

	// runtimeModeMask clears the prepared and native choices of a version 12 input.
	runtimeModeMask = ^byte(nativePreparedShape | nativeDriverShape)

	// messageBound bounds one observation in a failure message.
	messageBound = 600

	// recordingDigestBytes keeps recording names short while still unique per input.
	recordingDigestBytes = 12

	// recordingVersion invalidates recordings written by an older comparison.
	recordingVersion = "1"

	// recordingsVariable names the directory the SQLite suites share recordings in.
	recordingsVariable = "SEAGULL_FUZZ_RECORDINGS"
)

// FuzzNativeModes runs each version 12 input through every execution mode of the same
// compiled catalogue and requires identical responses. The modes are database/sql direct,
// prepared, and pgx or native ClickHouse.
//
// Takes f (*testing.F) which owns the workers and seeds.
// Takes suite (NativeFuzzSuite) which supplies native connections and the driver.
func FuzzNativeModes(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	seen := map[string]bool{}
	for _, seed := range fuzzcase.RuntimeShapeSeeds(suite.Engine) {
		base := runtimeBase(seed)
		if !seen[string(base)] {
			seen[string(base)] = true
			f.Add(base)
		}
	}
	workers := map[string]*nativeWorker{}
	for _, mode := range executionModes(suite.Engine) {
		workers[mode] = &nativeWorker{process: nil, cache: nil, temporary: f.TempDir(), shape: -1, metadata: sync.Once{}}
	}
	f.Cleanup(func() {
		for _, worker := range workers {
			assert.NoError(f, worker.close())
		}
	})
	f.Fuzz(func(t *testing.T, data []byte) {
		if _, ok := fuzzcase.DecodeRuntime(suite.Engine, data); !ok {
			t.Skip("unsupported native runtime envelope")
		}
		base := runtimeBase(data)
		responses := map[string]fuzznative.Response{}
		for _, mode := range executionModes(suite.Engine) {
			responses[mode] = nativeModeResponse(t, suite, workers[mode], runtimeVariant(base, mode))
		}
		baseline := responses[modeSQL]
		problems := nativeModeProblems(suite.Engine, responses)
		problems = append(problems, recordAcrossDrivers(t, suite, "native-modes", base, comparableResponse(&baseline))...)
		require.Empty(t, problems, "execution modes differ for input %v:\n%s", base, strings.Join(problems, "\n"))
	})
}

// runtimeVariant selects one mode in a version 12 input whose mode choices are clear.
//
// Takes base ([]byte) which selects the direct database/sql mode.
// Takes mode (string) which names the execution mode.
//
// Returns []byte which selects that mode.
func runtimeVariant(base []byte, mode string) []byte {
	variant := slices.Clone(base)
	switch mode {
	case modePrepared:
		variant[runtimeModeByte] |= nativePreparedShape
	case modeNative:
		variant[runtimeModeByte] |= nativeDriverShape
	}
	return variant
}

// runtimeBase clears the mode choices of a version 12 input, padding its mode byte.
//
// Takes data ([]byte) which holds a version 12 input.
//
// Returns []byte which selects the direct database/sql mode.
func runtimeBase(data []byte) []byte {
	base := slices.Clone(data)
	for len(base) <= runtimeModeByte {
		base = append(base, 0)
	}
	base[runtimeModeByte] &= runtimeModeMask
	return base
}

// nativeModeResponse runs one version 12 input in one mode on a fresh database.
//
// Takes t (*testing.T) which reports setup failures.
// Takes suite (NativeFuzzSuite) which supplies connections and the driver.
// Takes worker (*nativeWorker) which owns that mode's helper.
// Takes data ([]byte) which holds the input with its mode choices.
//
// Returns fuzznative.Response which records every generated observation.
func nativeModeResponse(t *testing.T, suite NativeFuzzSuite, worker *nativeWorker, data []byte) fuzznative.Response {
	t.Helper()
	model, ok := fuzzcase.DecodeRuntime(suite.Engine, data)
	require.True(t, ok, "a mode variant of a valid input no longer decodes")
	shape := model.Shape
	if model.Prepared {
		shape += nativePreparedShape
	}
	if model.Native {
		shape += nativeDriverShape
	}
	worker.prepare(t, suite, shape)
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
	defer cancel()
	database, connection := suite.OpenNative(t)
	database.SetMaxOpenConns(1)

	fuzzApply(ctx, t, database, fuzzcase.RuntimeSchema(suite.Engine))
	require.NoError(t, database.Close())
	response, err := worker.process.Exchange(ctx, connection, model.Data)
	require.NoError(t, err)
	return response
}

// nativeModeProblems compares every mode's response with database/sql's.
//
// Takes engine (string) which names the engine for messages.
// Takes responses (map[string]fuzznative.Response) which holds each mode's response.
//
// Returns []string which lists each differing observation.
func nativeModeProblems(engine string, responses map[string]fuzznative.Response) []string {
	var problems []string
	sqlResponse := responses[modeSQL]
	baseline := comparableResponse(&sqlResponse)
	for _, mode := range executionModes(engine)[1:] {
		response := responses[mode]
		other := comparableResponse(&response)
		expected, observed := reflect.ValueOf(baseline), reflect.ValueOf(other)
		for index := range expected.NumField() {
			if !reflect.DeepEqual(expected.Field(index).Interface(), observed.Field(index).Interface()) {
				problems = append(problems, fmt.Sprintf("%s mode %s: database/sql %s, this mode %s", mode, expected.Type().Field(index).Name,
					shortened(expected.Field(index).Interface()), shortened(observed.Field(index).Interface())))
			}
		}
	}
	return problems
}

// comparableResponse removes the fields a mode legitimately changes. These are the
// request identity, the mode flags and the driver's error wording, keeping whether it
// failed.
//
// Takes original (*fuzznative.Response) which is one mode's response.
//
// Returns fuzznative.Response which compares equal across modes that agree.
func comparableResponse(original *fuzznative.Response) fuzznative.Response {
	response := *original
	response.ID, response.Prepared, response.Native = 0, false, false
	if response.Error != "" {
		response.Error = "failed"
	}
	return response
}

// recordAcrossDrivers stores an observation for its input and driver, and compares it
// with the observation another driver of the same engine stored, so that the modernc and
// mattn SQLite suites check each other across separate runs.
//
// Takes t (*testing.T) which reports unwritable recordings.
// Takes suite (NativeFuzzSuite) which names the engine and driver.
// Takes target (string) which separates recordings of different oracles.
// Takes data ([]byte) which identifies the input.
// Takes observation (any) which is the driver-independent observation.
//
// Returns []string which describes a difference from another driver's recording.
func recordAcrossDrivers(t *testing.T, suite NativeFuzzSuite, target string, data []byte, observation any) []string {
	t.Helper()
	if suite.Engine != engineSQLite {
		return nil
	}
	directory := os.Getenv(recordingsVariable)
	if directory == "" {
		directory = filepath.Join(nativeRepositoryRoot(t), ".cache", "seagull-fuzz", "recordings")
	}
	digest := sha256.Sum256(append([]byte(recordingVersion+target), data...))
	key := target + "-" + hex.EncodeToString(digest[:recordingDigestBytes])
	encoded, err := json.MarshalIndent(observation, "", " ")
	require.NoError(t, err)
	// #nosec G703 -- The directory is the campaign's own recording root.
	require.NoError(t, os.MkdirAll(directory, directoryMode))
	// #nosec G703 -- The name is a digest and a fixed driver name inside the recording root.
	require.NoError(t, os.WriteFile(filepath.Join(directory, key+"-"+suite.Driver.Name+".json"), encoded, fileMode))
	names, err := fs.Glob(os.DirFS(directory), key+"-*.json")
	require.NoError(t, err)
	var problems []string
	for _, name := range names {
		if name == key+"-"+suite.Driver.Name+".json" {
			continue
		}
		// #nosec G304 G703 -- The name was listed inside the recording root.
		other, err := os.ReadFile(filepath.Join(directory, name))
		if errors.Is(err, fs.ErrNotExist) {

			continue
		}
		require.NoError(t, err)
		if !bytes.Equal(other, encoded) {
			problems = append(problems, fmt.Sprintf("driver %s observed differently from %s:\n%s\n%s:\n%s", suite.Driver.Name, name, encoded, name, other))
		}
	}
	return problems
}

// shortened writes a value for a failure message within a bounded length.
//
// Takes value (any) which is an observation.
//
// Returns string which is cut after a fixed number of characters.
func shortened(value any) string {
	text := fmt.Sprintf("%+v", value)
	count := 0
	for index := range text {
		if count == messageBound {
			return text[:index] + "..."
		}
		count++
	}
	return text
}
