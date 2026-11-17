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

package dialecttest

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"testing"
)

const (
	// externalSeedsDirectory is the directory, relative to the module root, the fuzz intake
	// writes each external suite's seeds under.
	externalSeedsDirectory = "testdata/external"

	// externalSeedsFile is the file of one suite's seeds, one JSON object per line.
	externalSeedsFile = "seeds.jsonl"

	// maxExternalSeedLine bounds one line of a seeds file, above the intake's own bound on a
	// seed.
	maxExternalSeedLine = 1 << 20

	// maxExternalSeeds bounds the seeds one suite's file may hold.
	maxExternalSeeds = 100_000

	// externalSeedBuffer is the size a seeds file's line buffer starts at.
	externalSeedBuffer = 64 << 10
)

var (
	// errModuleRootNotFound reports a working directory outside any Go module.
	errModuleRootNotFound = errors.New("no go.mod above the working directory")

	// errTooManyExternalSeeds reports a seeds file above maxExternalSeeds.
	errTooManyExternalSeeds = errors.New("the seeds file holds more seeds than the kit reads")
)

// externalSeed is one seed the fuzz intake admitted from an external test suite.
type externalSeed struct {
	// Migration holds the schema statements read before the query.
	Migration string `json:"migration"`

	// Query holds the query under its sgl.query directive.
	Query string `json:"query"`

	// Dialects names the dialects the seed's SQL is written for.
	Dialects []string `json:"dialects"`
}

// addExternalSeeds adds every admitted external seed written for the dialect, from each
// suite's seeds.jsonl under the module's testdata/external.
//
// A module without the directory has none. A seeds file that cannot be read or holds a
// malformed line fails the fuzz test, so an intake's seeds are never dropped unnoticed.
//
// Takes f (*testing.F) which receives the seeds.
// Takes dialectName (string) which is the dialect's canonical name, such as postgres.
func addExternalSeeds(f *testing.F, dialectName string) {
	f.Helper()

	root, err := moduleRoot()
	if err != nil {
		f.Fatalf("finding external seeds: %v", err)
	}
	seedsRoot, err := os.OpenRoot(filepath.Join(root, externalSeedsDirectory))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		f.Fatalf("reading external seeds: %v", err)
	}
	defer func() { _ = seedsRoot.Close() }()
	suites, err := fs.ReadDir(seedsRoot.FS(), ".")
	if err != nil {
		f.Fatalf("reading external seeds: %v", err)
	}
	for _, suite := range suites {
		if suite.IsDir() {
			addSuiteSeeds(f, seedsRoot, suite.Name(), dialectName)
		}
	}
}

// addSuiteSeeds adds the seeds of one suite written for the dialect.
//
// A suite without a seeds file has none.
//
// Takes f (*testing.F) which receives the seeds.
// Takes root (*os.Root) which confines access to the seeds directory.
// Takes suite (string) which names the suite's directory.
// Takes dialectName (string) which is the dialect's canonical name.
func addSuiteSeeds(f *testing.F, root *os.Root, suite, dialectName string) {
	f.Helper()
	seeds, err := openExternalSeeds(root, suite)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		f.Fatalf("reading external seeds of %s: %v", suite, err)
	}
	for _, seed := range seeds {
		if slices.Contains(seed.Dialects, dialectName) {
			f.Add(seed.Migration, seed.Query)
		}
	}
}

// openExternalSeeds reads one suite's seeds file inside the seeds directory.
//
// Takes root (*os.Root) which confines access to the seeds directory.
// Takes suite (string) which names the suite's directory.
//
// Returns []externalSeed which holds the seeds in file order.
// Returns error which wraps fs.ErrNotExist when the suite has no seeds file.
func openExternalSeeds(root *os.Root, suite string) ([]externalSeed, error) {
	file, err := root.Open(path.Join(suite, externalSeedsFile))
	if err != nil {
		return nil, fmt.Errorf("opening the seeds file: %w", err)
	}
	defer func() { _ = file.Close() }()
	return readExternalSeeds(file)
}

// readExternalSeeds reads one suite's seeds, one JSON object per line.
//
// Takes reader (io.Reader) which supplies the seeds file.
//
// Returns []externalSeed which holds the seeds in file order.
// Returns error when a line is not a seed, is too long, or the file holds too many.
func readExternalSeeds(reader io.Reader) ([]externalSeed, error) {
	var seeds []externalSeed
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, externalSeedBuffer), maxExternalSeedLine)
	line := 0
	for scanner.Scan() {
		line++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		if len(seeds) == maxExternalSeeds {
			return nil, fmt.Errorf("line %d: %w", line, errTooManyExternalSeeds)
		}
		var seed externalSeed
		if err := json.Unmarshal(scanner.Bytes(), &seed); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		seeds = append(seeds, seed)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("after line %d: %w", line, err)
	}
	return seeds, nil
}

// moduleRoot finds the directory holding go.mod, from the working directory upwards.
//
// Returns string which is the module root.
// Returns error which wraps errModuleRootNotFound when no go.mod is found.
func moduleRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("reading the working directory: %w", err)
	}
	for {
		_, statErr := os.Stat(filepath.Join(directory, "go.mod"))
		if statErr == nil {
			return directory, nil
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", fmt.Errorf("looking for go.mod: %w", statErr)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errModuleRootNotFound
		}
		directory = parent
	}
}
