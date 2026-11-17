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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgramCacheRetainsVerifiedBuildsAndEvictsOldest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compiled")
	cache, err := OpenProgramCache(root, 12, 2)
	require.NoError(t, err)
	defer cache.Close()
	source := filepath.Join(t.TempDir(), "program")
	require.NoError(t, os.WriteFile(source, []byte("binary"), 0o700))
	first, second, third := ProgramKey([]byte("first")), ProgramKey([]byte("second")), ProgramKey([]byte("third"))
	retained, err := cache.Retain(first, source)
	require.NoError(t, err)
	_, err = cache.Retain(second, source)
	require.NoError(t, err)
	found, err := cache.Find(first)
	require.NoError(t, err)
	require.Equal(t, retained, found)
	_, err = cache.Retain(third, source)
	require.NoError(t, err)
	found, err = cache.Find(second)
	require.NoError(t, err)
	require.Empty(t, found)
	require.NoError(t, cache.Close())
	reopened, err := OpenProgramCache(root, 12, 2)
	require.NoError(t, err)
	defer reopened.Close()
	found, err = reopened.Find(first)
	require.NoError(t, err)
	require.Equal(t, retained, found)
	_, err = cache.Find(first)
	require.Error(t, err)
}

func TestProgramCacheRejectsWrongOwnershipAndUnsafePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compiled")
	cache, err := OpenProgramCache(root, 12, 2)
	require.NoError(t, err)
	defer cache.Close()
	_, err = OpenProgramCache(root, 12, 2)
	require.Error(t, err)
	_, err = cache.Find("../outside")
	require.Error(t, err)
	external := t.TempDir()
	sentinel := filepath.Join(external, "keep")
	require.NoError(t, os.WriteFile(sentinel, []byte("owned elsewhere"), 0o600))
	_, err = OpenProgramCache(external, 12, 2)
	require.Error(t, err)
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(external, link))
	_, err = OpenProgramCache(filepath.Join(link, "child"), 12, 2)
	require.Error(t, err)
	key := ProgramKey([]byte("linked"))
	require.NoError(t, os.Symlink(external, filepath.Join(root, key)))
	_, err = cache.Find(key)
	require.Error(t, err)
	_, err = os.Stat(sentinel)
	require.NoError(t, err)
}

func TestProgramCacheRejectsOversizedAndCorruptedExecutables(t *testing.T) {
	cache, err := OpenProgramCache(filepath.Join(t.TempDir(), "compiled"), 6, 2)
	require.NoError(t, err)
	defer cache.Close()
	source := filepath.Join(t.TempDir(), "program")
	require.NoError(t, os.WriteFile(source, []byte("oversized"), 0o700))
	key := ProgramKey([]byte("fixture"))
	_, err = cache.Retain(key, source)
	require.ErrorContains(t, err, "budget")
	require.NoError(t, os.WriteFile(source, []byte("binary"), 0o700))
	retained, err := cache.Retain(key, source)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(retained, []byte("broken"), 0o700))
	_, err = cache.Find(key)
	require.ErrorContains(t, err, "integrity")
}

func TestProgramKeySeparatesBuildInputsAndToolchainVersions(t *testing.T) {
	require.NotEqual(t, ProgramKey([]byte("ab"), []byte("c")), ProgramKey([]byte("a"), []byte("bc")))
	source := []byte("generated source")
	require.NotEqual(t, ProgramKey(source, []byte("go1")), ProgramKey(source, []byte("go2")))
	require.Equal(t, ProgramKey(source), strings.ToLower(ProgramKey(source)))
}

func TestProgramCacheRecordsItsWorkerAndRetainsFailedCloseOwnership(t *testing.T) {
	t.Setenv("SEAGULL_FUZZ_COMMAND_TOKEN", "isolated-command")
	root := filepath.Join(t.TempDir(), "compiled")
	cache, err := OpenProgramCache(root, 12, 2)
	require.NoError(t, err)
	ownerPath := filepath.Join(root, "lease", "owner.json")
	content, err := os.ReadFile(ownerPath)
	require.NoError(t, err)
	var owner struct {
		CommandToken string
		PID          int
	}
	require.NoError(t, json.Unmarshal(content, &owner))
	require.Equal(t, "isolated-command", owner.CommandToken)
	require.Equal(t, os.Getpid(), owner.PID)
	extra := filepath.Join(root, "lease", "unexpected")
	require.NoError(t, os.WriteFile(extra, []byte("keep"), 0o600))
	require.Error(t, cache.Close())
	_, err = OpenProgramCache(root, 12, 2)
	require.Error(t, err)
	require.NoError(t, os.Remove(extra))
	require.NoError(t, cache.Close())
	require.NoError(t, cache.Close())
	_, err = os.Stat(filepath.Join(root, "lease"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestProgramCacheRemovesStagesLeftByAnInterruptedWorker(t *testing.T) {
	root := filepath.Join(t.TempDir(), "compiled")
	cache, err := OpenProgramCache(root, 12, 2)
	require.NoError(t, err)
	require.NoError(t, cache.Close())
	stale := filepath.Join(root, cacheStagePrefix+"interrupted")
	require.NoError(t, os.Mkdir(stale, 0o700))
	reopened, err := OpenProgramCache(root, 12, 2)
	require.NoError(t, err)
	defer reopened.Close()
	_, err = os.Stat(stale)
	require.ErrorIs(t, err, os.ErrNotExist)
	source := filepath.Join(t.TempDir(), "program")
	require.NoError(t, os.WriteFile(source, []byte("binary"), 0o700))
	_, err = reopened.Retain(ProgramKey([]byte("after")), source)
	require.NoError(t, err)
}

func TestProgramCacheRejectsAnOversizedOwnershipMarker(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, cacheOwnerMarkerName), []byte(strings.Repeat("x", cacheManifestLimit+1)), 0o600))
	_, err := OpenProgramCache(root, 12, 2)
	require.ErrorContains(t, err, "exceeds")
}

func TestProgramCacheRejectsAnOversizedManifest(t *testing.T) {
	cache, err := OpenProgramCache(filepath.Join(t.TempDir(), "compiled"), 12, 2)
	require.NoError(t, err)
	defer cache.Close()
	source := filepath.Join(t.TempDir(), "program")
	require.NoError(t, os.WriteFile(source, []byte("binary"), 0o700))
	key := ProgramKey([]byte("manifest"))
	_, err = cache.Retain(key, source)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cache.root, key, cacheManifestName), []byte(strings.Repeat(" ", cacheManifestLimit+1)), 0o600))
	_, err = cache.Find(key)
	require.ErrorContains(t, err, "exceeds")
}
