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

package helpers

import (
	"fmt"
	"io/fs"
	"path"
	"sync"

	"seagull.sh/seagull/internal/sqlmodel"
)

// Sources lazily prepares a fixed directory of helper templates.
//
// Its cache admits only files present in that directory, never names or source text
// supplied by a query. Prepared syntax trees are read-only; each rendering owns its edits
// and imports.
type Sources struct {
	// load initialises and returns the fixed set of cached source loaders.
	load func() (map[string]func() (*renderer, error), error)
}

// NewSources prepares a helper directory on first use, parsing each file at most once.
//
// Takes files (fs.FS) which contains the immutable helper source files.
// Takes directory (string) which names their directory.
//
// Returns *Sources which can render the files concurrently.
func NewSources(files fs.FS, directory string) *Sources {
	return &Sources{load: sync.OnceValues(func() (map[string]func() (*renderer, error), error) {
		entries, err := fs.ReadDir(files, directory)
		if err != nil {
			return nil, err
		}
		prepared := make(map[string]func() (*renderer, error), len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			filename := path.Join(directory, entry.Name())
			prepared[entry.Name()] = sync.OnceValues(func() (*renderer, error) {
				source, err := fs.ReadFile(files, filename)
				if err != nil {
					return nil, err
				}
				return newRenderer(source, nil, nil)
			})
		}
		return prepared, nil
	})}
}

// File renders a prepared source as a new file, without sharing mutable render state.
//
// Takes name (string) which names the output file.
// Takes sourceName (string) which names the source within the directory.
// Takes vocabulary (Vocabulary) which names generated identifiers.
// Takes data (*Data) which holds the specialisation values.
//
// Returns sqlmodel.GeneratedFile which owns its content.
// Returns error when the source cannot be read or the result cannot be rendered.
func (sources *Sources) File(name, sourceName string, vocabulary Vocabulary, data *Data) (sqlmodel.GeneratedFile, error) {
	prepared, err := sources.load()
	if err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("helper source %s: %w", sourceName, err)
	}
	load, found := prepared[sourceName]
	if !found {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("helper source %s: %w", sourceName, fs.ErrNotExist)
	}
	template, err := load()
	if err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("helper source %s: %w", sourceName, err)
	}
	state := *template
	state.vocabulary = vocabulary
	state.data = data
	state.inlined = make(map[string]any)
	if err := state.plan(); err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("rendering %s: %w", name, err)
	}
	content, err := finish([]byte(state.text(0, len(state.source), nil)), state.imports)
	if err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("rendering %s: %w", name, err)
	}
	return sqlmodel.GeneratedFile{Name: name, Content: content}, nil
}
