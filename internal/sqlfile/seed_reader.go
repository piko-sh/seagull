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

package sqlfile

import (
	"cmp"
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	// seedFilePattern matches seed files named like 001_demo_data.sql.
	seedFilePattern = regexp.MustCompile(`^(\d+)_(.+)\.sql$`)
)

// ReadSeedFiles reads and parses seed SQL files matching {version}_{name}.sql from the
// given directory, returning them sorted by version ascending and keeping directory order
// among files that share a version.
//
// Named pipes, sockets and devices are left out, as directories are. It does not reject
// two files that share a version; DuplicateSeed finds them, so each caller reports the
// collision in its own terms.
//
// Takes fileReader (FileReader) which provides filesystem access.
// Takes directory (string) which is the path to the seed files directory.
//
// Returns []SeedFile which holds the sorted seed files.
// Returns error when the directory cannot be read or a file cannot be loaded.
func ReadSeedFiles(
	ctx context.Context,
	fileReader FileReader,
	directory string,
) ([]SeedFile, error) {
	entries, err := fileReader.ReadDir(ctx, directory)
	if err != nil {
		return nil, fmt.Errorf("reading seed directory %s: %w", directory, err)
	}

	var files []SeedFile
	for _, entry := range entries {
		if entry.IsDir() || isSpecialFile(entry) {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		matches := seedFilePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}

		version, parseErr := strconv.ParseInt(matches[1], 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing version from %s: %w", entry.Name(), parseErr)
		}

		filePath := path.Join(directory, entry.Name())
		content, readErr := fileReader.ReadFile(ctx, filePath)
		if readErr != nil {
			return nil, fmt.Errorf("reading seed file %s: %w", filePath, readErr)
		}

		files = append(files, SeedFile{
			Version:  version,
			Name:     matches[2],
			Filename: entry.Name(),
			Content:  content,
			Checksum: computeChecksum(content),
		})
	}

	slices.SortStableFunc(files, func(left, right SeedFile) int {
		return cmp.Compare(left.Version, right.Version)
	})

	return files, nil
}

// DuplicateSeed finds the first two seed files that share a version, such as two
// 001_*.sql files.
//
// Takes files ([]SeedFile) which are sorted as ReadSeedFiles returns them.
//
// Returns first (SeedFile) which is the file seen first for the shared version.
// Returns second (SeedFile) which is the conflicting file.
// Returns found (bool) which is false when every version is distinct.
func DuplicateSeed(files []SeedFile) (first, second SeedFile, found bool) {
	for index := 1; index < len(files); index++ {
		if files[index-1].Version == files[index].Version {
			return files[index-1], files[index], true
		}
	}
	return SeedFile{}, SeedFile{}, false
}
