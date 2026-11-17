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

//go:build linux || darwin

package supervise

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocatedBytesMatchesDuWithoutFollowingLinks(t *testing.T) {
	if _, err := exec.LookPath("du"); err != nil {
		t.Skip("du is not installed")
	}
	t.Parallel()

	temporary := t.TempDir()
	root := filepath.Join(temporary, "managed")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested", "deeper"), 0o700))
	payload := filepath.Join(root, "payload")
	require.NoError(t, os.WriteFile(payload, []byte(strings.Repeat("x", 8192)), 0o600))
	require.NoError(t, os.Link(payload, filepath.Join(root, "nested", "hardlink")))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested", "deeper", "small"), []byte("x"), 0o600))
	outside := filepath.Join(temporary, "outside")
	require.NoError(t, os.Mkdir(outside, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "large"), make([]byte, 2<<20), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))

	output, err := exec.Command("du", "-sk", root).Output()
	require.NoError(t, err)
	kibibytes, err := strconv.ParseUint(strings.Fields(string(output))[0], 10, 64)
	require.NoError(t, err)

	used, err := AllocatedBytes(root)

	require.NoError(t, err)
	require.Equal(t, kibibytes*1024, used)
}

func TestAllocatedBytesSkipsAChildRemovedMidWalk(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "vanished"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "kept"), []byte("x"), 0o600))
	walker := diskWalker{
		seen: make(map[fileIdentity]bool),
		openRoot: func(parent *os.Root, name string) (*os.Root, error) {
			if name == "vanished" {
				return nil, &os.PathError{Op: "openat", Path: name, Err: syscall.ENOENT}
			}
			return parent.OpenRoot(name)
		},
	}

	used, err := walker.measure(root)

	require.NoError(t, err)
	require.Positive(t, used)
}

func TestAllocatedBytesRefusesWhatItCannotMeasure(t *testing.T) {
	t.Parallel()

	temporary := t.TempDir()
	directory := filepath.Join(temporary, "directory")
	require.NoError(t, os.Mkdir(directory, 0o700))
	link := filepath.Join(temporary, "link")
	require.NoError(t, os.Symlink(directory, link))
	file := filepath.Join(temporary, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	tests := []struct {
		name string
		path string
	}{
		{name: "a missing root", path: filepath.Join(temporary, "missing")},
		{name: "a linked root", path: link},
		{name: "a file", path: file},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := AllocatedBytes(testCase.path)
			require.Error(t, err)
		})
	}
}

func TestAllocatedBytesFailsOnAnUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	t.Parallel()

	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	require.NoError(t, os.Mkdir(locked, 0o700))
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	_, err := AllocatedBytes(root)

	require.Error(t, err)
}
