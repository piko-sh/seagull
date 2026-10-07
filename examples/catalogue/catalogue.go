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

package catalogue

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	// ConceptsFile is the curriculum's file name, in the examples directory.
	ConceptsFile = "concepts.yaml"

	// VariantFile is each variant directory's manifest.
	VariantFile = "example.yaml"

	// StatusVariant marks an engine with its own directory.
	StatusVariant = "variant"

	// StatusEquivalent marks an engine whose generated code equals its twin column's.
	StatusEquivalent = "equivalent"

	// StatusUnsupported marks an engine that cannot do what the concept shows.
	StatusUnsupported = "unsupported"

	// StatusOmitted marks an engine where a variant would teach nothing new.
	StatusOmitted = "omitted"

	// StatusBlocked marks a variant waiting on a change to Seagull.
	StatusBlocked = "blocked"

	// maxFileBytes is the largest file the catalogue reads or exports, a manifest, an
	// sgl.yaml, an entry query file or any file of a variant.
	maxFileBytes = 4 << 20
)

// Engine describes one database engine.
type Engine struct {
	// Title is the engine's display name.
	Title string `yaml:"title" json:"title,omitempty"`

	// Dialect is the sgl.yaml dialect name.
	Dialect string `yaml:"dialect" json:"dialect,omitempty"`

	// Module is the examples module holding the engine's variants.
	Module string `yaml:"module" json:"module,omitempty"`

	// Twin is the engine whose variants this engine's equivalent cells reuse.
	Twin string `yaml:"twin" json:"twin,omitempty"`
}

// Column is one engine and driver; variant directories carry its suffix.
type Column struct {
	// ID names the column in concept cells, such as postgres-pgx.
	ID string `yaml:"id" json:"id,omitempty"`

	// Engine is the column's engine.
	Engine string `yaml:"engine" json:"engine,omitempty"`

	// Driver is the sgl.yaml go.driver the column's variants use.
	Driver string `yaml:"driver" json:"driver,omitempty"`

	// Suffix ends the column's variant directory names, such as _pgx.
	Suffix string `yaml:"suffix" json:"suffix,omitempty"`
}

// Track groups concepts on the website.
type Track struct {
	// ID names the track in concepts.
	ID string `yaml:"id" json:"id,omitempty"`

	// Title is the track's heading.
	Title string `yaml:"title" json:"title,omitempty"`

	// Tone is the badge colour the website uses for the track.
	Tone string `yaml:"tone" json:"tone,omitempty"`
}

// Cell records what one column does with a concept.
type Cell struct {
	// Status is variant, equivalent, unsupported, omitted or blocked.
	Status string `yaml:"status" json:"status,omitempty"`

	// Twin is the column an equivalent cell reuses.
	Twin string `yaml:"twin" json:"twin,omitempty"`

	// Reason explains an unsupported, omitted or blocked cell.
	Reason string `yaml:"reason" json:"reason,omitempty"`

	// Diagnostic is the Q-code Seagull reports for an unsupported cell, if any.
	Diagnostic string `yaml:"diagnostic" json:"diagnostic,omitempty"`

	// See is the number of a related concept.
	See int `yaml:"see" json:"see,omitempty"`
}

// Concept is one idea in the curriculum.
type Concept struct {
	// Slug names the concept in URLs and, with underscores for hyphens, in directory names.
	Slug string `yaml:"slug" json:"slug,omitempty"`

	// Title is the concept's heading.
	Title string `yaml:"title" json:"title,omitempty"`

	// Track is the concept's track ID.
	Track string `yaml:"track" json:"track,omitempty"`

	// Summary is one sentence about the concept.
	Summary string `yaml:"summary" json:"summary,omitempty"`

	// Teaches lists the features the concept shows.
	Teaches []string `yaml:"teaches" json:"teaches,omitempty"`

	// Diagnostics lists the Q-codes the concept explains.
	Diagnostics []string `yaml:"diagnostics" json:"diagnostics,omitempty"`

	// Docs lists the documentation pages and anchors the concept illustrates.
	Docs []string `yaml:"docs" json:"docs,omitempty"`

	// Module overrides the examples module for every variant, such as tooling.
	Module string `yaml:"module" json:"module,omitempty"`

	// Cells records each column's status.
	Cells map[string]Cell `yaml:"cells" json:"cells,omitempty"`

	// Scope limits the concept to these engines.
	Scope []string `yaml:"scope" json:"scope,omitempty"`

	// Number orders the concept in the learning path.
	Number int `yaml:"number" json:"number,omitempty"`
}

// Curriculum is concepts.yaml.
type Curriculum struct {
	// Engines describes each engine by name.
	Engines map[string]Engine `yaml:"engines" json:"engines,omitempty"`

	// Columns lists the engine and driver pairs.
	Columns []Column `yaml:"columns" json:"columns,omitempty"`

	// Tracks lists the tracks in display order.
	Tracks []Track `yaml:"tracks" json:"tracks,omitempty"`

	// Concepts lists the concepts in learning order.
	Concepts []Concept `yaml:"concepts" json:"concepts,omitempty"`

	// Version is the file format's version.
	Version int `yaml:"version" json:"version,omitempty"`
}

// Column returns the column with the given ID.
//
// Takes id (string) which is the column's ID.
//
// Returns Column which is the column.
// Returns bool which is false when no column has the ID.
func (c *Curriculum) Column(id string) (Column, bool) {
	for _, column := range c.Columns {
		if column.ID == id {
			return column, true
		}
	}
	return Column{}, false
}

// VariantDirectory returns a variant's directory, relative to the examples directory,
// such as postgres/010_slices_pgx.
//
// Takes concept (*Concept) which is the concept.
// Takes column (Column) which is the variant's column.
//
// Returns string which is the relative directory.
func (c *Curriculum) VariantDirectory(concept *Concept, column Column) string {
	module := concept.Module
	if module == "" {
		module = c.Engines[column.Engine].Module
	}
	name := fmt.Sprintf("%03d_%s%s", concept.Number, strings.ReplaceAll(concept.Slug, "-", "_"), column.Suffix)
	return filepath.Join(module, name)
}

// VariantDirectories returns every variant directory the curriculum names, mapped to its
// concept and column.
//
// Returns map[string]VariantRef which maps relative directories to their cells.
func (c *Curriculum) VariantDirectories() map[string]VariantRef {
	directories := make(map[string]VariantRef)
	for index := range c.Concepts {
		concept := &c.Concepts[index]
		for columnID, cell := range concept.Cells {
			if cell.Status != StatusVariant {
				continue
			}
			column, ok := c.Column(columnID)
			if !ok {
				continue
			}
			directories[c.VariantDirectory(concept, column)] = VariantRef{Concept: concept, Column: column}
		}
	}
	return directories
}

// Entry names the file and query a variant's walkthrough starts from.
type Entry struct {
	// File is the query file, relative to the variant directory.
	File string `yaml:"file" json:"file,omitempty"`

	// Query is the query's name in that file.
	Query string `yaml:"query" json:"query,omitempty"`
}

// Variant is a variant directory's example.yaml.
type Variant struct {
	// Summary says what this engine's version shows.
	Summary string `yaml:"summary" json:"summary,omitempty"`

	// Differs lists what this engine does differently from the others.
	Differs []string `yaml:"differs" json:"differs,omitempty"`

	// Entry names the query the walkthrough starts from.
	Entry Entry `yaml:"entry" json:"entry,omitzero"`

	// Live is yes, partial or no: whether the browser playground can run it.
	Live string `yaml:"live" json:"live,omitempty"`

	// Test is how CI runs the variant: in-process, cgo, integration, transcript or
	// generate-only.
	Test string `yaml:"test" json:"test,omitempty"`

	// ExpectedDiagnostics lists the Q-codes sgl vet reports for the variant.
	ExpectedDiagnostics []string `yaml:"expected_diagnostics" json:"expectedDiagnostics,omitempty"`
}

// VariantRef ties a variant directory to its concept and column.
type VariantRef struct {
	// Concept is the variant's concept.
	Concept *Concept

	// Column is the variant's column.
	Column Column
}

// String names a variant reference for messages.
//
// Returns string which is the concept number, slug and column.
func (r VariantRef) String() string {
	return strconv.Itoa(r.Concept.Number) + " " + r.Concept.Slug + " (" + r.Column.ID + ")"
}

// Load reads concepts.yaml from the examples directory.
//
// Takes examplesDirectory (string) which is the examples directory.
//
// Returns *Curriculum which is the parsed curriculum.
// Returns error when the file cannot be read or parsed, names a key the curriculum has no
// field for, or holds other than one YAML document.
func Load(examplesDirectory string) (*Curriculum, error) {
	data, err := readIn(examplesDirectory, ConceptsFile)
	if err != nil {
		return nil, err
	}
	var curriculum Curriculum
	if err := decodeDocument(data, &curriculum); err != nil {
		return nil, fmt.Errorf("%s: %w", ConceptsFile, err)
	}
	return &curriculum, nil
}

// LoadVariant reads a variant directory's example.yaml.
//
// Takes directory (string) which is the variant directory.
//
// Returns *Variant which is the parsed manifest.
// Returns error when the file cannot be read or parsed, names a key the manifest has no
// field for, or holds other than one YAML document.
func LoadVariant(directory string) (*Variant, error) {
	data, err := readIn(directory, VariantFile)
	if err != nil {
		return nil, err
	}
	var variant Variant
	if err := decodeDocument(data, &variant); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(directory, VariantFile), err)
	}
	return &variant, nil
}

// readIn reads the file name within directory through an os.Root, so a name taken from a
// manifest cannot reach outside the directory, whether by .. or by a symbolic link. Only
// a regular file is opened, and no more of it is read than maxFileBytes.
//
// Takes directory (string) which is the directory to read in.
// Takes name (string) which is the file's path within directory.
//
// Returns []byte which is the file's contents.
// Returns error when the file cannot be read, lies outside directory, is not a regular
// file, or is larger than maxFileBytes.
func readIn(directory, name string) ([]byte, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return readRegular(root.FS(), filepath.ToSlash(name))
}

// readRegular reads a regular file of files, refusing anything else before it is opened,
// since opening a named pipe would wait for a writer, and reading no more than
// maxFileBytes of it.
//
// Takes files (fs.FS) which holds the file.
// Takes name (string) which is the file's slash-separated path in files.
//
// Returns []byte which is the file's contents.
// Returns error when the file cannot be read, is not a regular file, or is larger than
// maxFileBytes.
func readRegular(files fs.FS, name string) ([]byte, error) {
	info, err := fs.Stat(files, name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf(wrapFormat, name, ErrNotRegular)
	}
	file, err := files.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("%s: %w: more than %d bytes", name, ErrFileTooLarge, maxFileBytes)
	}
	return data, nil
}

// decodeDocument decodes the one YAML document data holds into target. A key target has
// no field for is an error, so a misspelt key fails rather than leaving its field empty.
//
// Takes data ([]byte) which is the file's contents.
// Takes target (any) which receives the decoded values.
//
// Returns error when the document is malformed or names an unknown key, or wraps
// ErrDocumentCount when data holds no document or more than one.
func decodeDocument(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: the file holds none", ErrDocumentCount)
		}
		return err
	}
	var extra yaml.Node
	switch err := decoder.Decode(&extra); {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return err
	default:
		return fmt.Errorf("%w: the file holds more than one", ErrDocumentCount)
	}
}

// requireEmpty checks that directory is absent or an empty directory, reading no more
// than one of its entries.
//
// Takes directory (string) which is the directory to check.
//
// Returns error which wraps ErrNotEmpty when the directory holds an entry, or is the
// error met reading it, such as when it is a file.
func requireEmpty(directory string) error {
	root, err := os.OpenRoot(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	handle, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	names, err := handle.Readdirnames(1)
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return err
	case len(names) > 0:
		return fmt.Errorf(wrapFormat, directory, ErrNotEmpty)
	}
	return nil
}
