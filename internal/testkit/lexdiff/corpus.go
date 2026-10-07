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

package lexdiff

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// CorpusDialect names the dialect golden cases under internal/dialect/<engine>/testdata.
	CorpusDialect = "dialect"

	// CorpusCompatibility names the compatibility inputs under
	// internal/dialect/dialecttest/testdata.
	CorpusCompatibility = "compatibility"

	// CorpusIntegration names the integration cases under tests/integration/<engine>.
	CorpusIntegration = "integration"

	// moduleLine identifies the root go.mod of the seagull.sh/seagull module.
	moduleLine = "module seagull.sh/seagull\n"

	// sqlExtension is the extension of a SQL file.
	sqlExtension = ".sql"
)

var (
	// errModuleRootNotFound is returned when no parent directory holds the module's go.mod.
	errModuleRootNotFound = errors.New("the seagull.sh/seagull module root was not found")
)

// File is one SQL file of a corpus.
type File struct {
	// Path is the file's path relative to the module root, with forward slashes.
	Path string

	// Corpus names the corpus the file belongs to, such as CorpusDialect.
	Corpus string

	// Name is the file's path relative to its corpus directory, with forward slashes.
	Name string

	// Content is the file's text.
	Content string
}

// corpusDirectory is one directory that holds SQL test data.
type corpusDirectory struct {
	// corpus names the corpus.
	corpus string

	// path is the directory relative to the module root.
	path string
}

// Corpus returns every SQL file the repository holds as test data for the engines named:
// the dialect golden cases under internal/dialect/<engine>/testdata, the compatibility
// inputs under internal/dialect/dialecttest/testdata/*/inputs/<engine>, and the
// integration cases under tests/integration/<engine>/testdata.
//
// Takes engines (...string) which name the engines, as their directory names spell them.
//
// Returns []File which holds the files sorted by path.
// Returns error when the module root cannot be found or a file cannot be read.
func Corpus(engines ...string) ([]File, error) {
	root, err := moduleRoot()
	if err != nil {
		return nil, err
	}
	var files []File
	for _, engine := range engines {
		directories, globErr := corpusDirectories(root, engine)
		if globErr != nil {
			return nil, globErr
		}
		for _, directory := range directories {
			found, walkErr := sqlFiles(root, directory)
			if walkErr != nil {
				return nil, walkErr
			}
			files = append(files, found...)
		}
	}
	slices.SortFunc(files, func(left, right File) int { return strings.Compare(left.Path, right.Path) })
	return files, nil
}

// corpusDirectories lists the directories that hold an engine's SQL test data.
//
// Takes root (string) which is the module root.
// Takes engine (string) which names the engine.
//
// Returns []corpusDirectory which holds the directories.
// Returns error when the compatibility inputs cannot be globbed.
func corpusDirectories(root, engine string) ([]corpusDirectory, error) {
	directories := []corpusDirectory{
		{corpus: CorpusDialect, path: filepath.Join("internal", "dialect", engine, "testdata")},
		{corpus: CorpusIntegration, path: filepath.Join("tests", "integration", engine, "testdata")},
	}
	compatibility, err := filepath.Glob(filepath.Join(root, "internal", "dialect", "dialecttest", "testdata", "*", "inputs", engine))
	if err != nil {
		return nil, fmt.Errorf("globbing the compatibility inputs: %w", err)
	}
	for _, match := range compatibility {
		relative, relativeErr := filepath.Rel(root, match)
		if relativeErr != nil {
			return nil, fmt.Errorf("locating %s: %w", match, relativeErr)
		}
		directories = append(directories, corpusDirectory{corpus: CorpusCompatibility, path: relative})
	}
	return directories, nil
}

// sqlFiles reads every SQL file under directory, which may be missing.
//
// Takes root (string) which is the module root.
// Takes directory (corpusDirectory) which is the directory and its corpus.
//
// Returns []File which holds the files found.
// Returns error when the directory cannot be walked or a file cannot be read.
func sqlFiles(root string, directory corpusDirectory) ([]File, error) {
	var files []File
	tree := os.DirFS(filepath.Join(root, directory.path))
	walkErr := fs.WalkDir(tree, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() || path.Ext(name) != sqlExtension {
			return nil
		}
		content, readErr := fs.ReadFile(tree, name)
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", name, readErr)
		}
		files = append(files, File{
			Path:    path.Join(filepath.ToSlash(directory.path), name),
			Corpus:  directory.corpus,
			Name:    name,
			Content: string(content),
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walking %s: %w", directory.path, walkErr)
	}
	return files, nil
}

// moduleRoot finds the root of the seagull.sh/seagull module from the working directory.
//
// Returns string which is the root directory.
// Returns error when no parent directory holds the module's go.mod.
func moduleRoot() (string, error) {
	directory, err := filepath.Abs(".")
	if err != nil {
		return "", fmt.Errorf("resolving the working directory: %w", err)
	}
	for {
		content, readErr := fs.ReadFile(os.DirFS(directory), "go.mod")
		if readErr == nil && strings.Contains(string(content), moduleLine) {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errModuleRootNotFound
		}
		directory = parent
	}
}
