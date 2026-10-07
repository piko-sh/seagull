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
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// ManifestVersion is the format version of the exported manifest.
	ManifestVersion = 1

	// exportDirPermissions is the mode of the directories Export creates.
	exportDirPermissions = 0o750

	// exportFilePermissions is the mode of the files Export writes, readable like the
	// sources they are copied from.
	exportFilePermissions = 0o644

	// maxVariantFiles is the most files an export copies from one variant directory.
	maxVariantFiles = 1_000

	// maxExportBytes is the most an export copies in all.
	maxExportBytes = 1 << 30
)

// File roles in an exported variant, derived from each file's path.
const (
	RoleCode       = "code"
	RoleWiring     = "wiring"
	RoleTest       = "test"
	RoleGenerated  = "generated"
	RoleMigration  = "migration"
	RoleQuery      = "query"
	RoleSeed       = "seed"
	RoleConfig     = "config"
	RoleOutput     = "output"
	RoleTranscript = "transcript"
	RoleSupport    = "support"
)

// ExportedFile describes one file of an exported variant.
type ExportedFile struct {
	// Path is the file's path within the variant directory.
	Path string `json:"path"`

	// Role says what the file is for, such as query or generated.
	Role string `json:"role"`

	// Lang is the file's language for highlighting: go, sql, yaml or text.
	Lang string `json:"lang"`
}

// ExportedVariant is one variant directory in the manifest.
type ExportedVariant struct {
	Variant

	// Concept is the concept's slug.
	Concept string `json:"concept"`

	// Column is the column's ID.
	Column string `json:"column"`

	// Engine is the column's engine.
	Engine string `json:"engine"`

	// Driver is the column's driver.
	Driver string `json:"driver"`

	// Directory is the variant directory, relative to the examples directory.
	Directory string `json:"dir"`

	// Files lists the variant's files.
	Files []ExportedFile `json:"files"`
}

// Manifest is the JSON the website reads: the curriculum and every variant, with file
// lists, so the site needs no YAML parser.
type Manifest struct {
	// Engines lists the engines in the curriculum's order.
	Engines []ExportedEngine `json:"engines"`

	// Columns lists the engine and driver pairs.
	Columns []Column `json:"columns"`

	// Tracks lists the tracks in display order.
	Tracks []Track `json:"tracks"`

	// Concepts lists the concepts in learning order.
	Concepts []Concept `json:"concepts"`

	// Variants lists every variant directory.
	Variants []ExportedVariant `json:"variants"`

	// Version is ManifestVersion.
	Version int `json:"version"`
}

// ExportedEngine is an engine with its name.
type ExportedEngine struct {
	Engine

	// Name is the engine's key, such as postgres.
	Name string `json:"name"`
}

// Export builds the manifest for the examples directory and copies every variant's files
// into out, keeping their paths relative to the examples directory. The examples must
// pass Validate. The export stops before the next variant once the context is done.
//
// Takes examplesDirectory (string) which is the examples directory.
// Takes out (string) which receives manifest.json and the files; it must be empty or
// absent.
//
// Returns *Manifest which is the exported manifest.
// Returns error when out is not empty, the curriculum is invalid, a file cannot be copied
// or is larger than the export allows, or the context is done.
func Export(ctx context.Context, examplesDirectory, out string) (*Manifest, error) {
	if err := requireEmpty(out); err != nil {
		return nil, err
	}
	curriculum, err := Load(examplesDirectory)
	if err != nil {
		return nil, err
	}
	if err := Validate(examplesDirectory); err != nil {
		return nil, fmt.Errorf("the examples are not valid: %w", err)
	}
	variants := slices.Sorted(maps.Keys(curriculum.VariantDirectories()))
	return writeExport(ctx, curriculum, examplesDirectory, out, variants)
}

// writeExport copies the named variants into out and writes the manifest beside them.
//
// Takes curriculum (*Curriculum) which is the loaded curriculum.
// Takes examplesDirectory (string) which is the examples directory.
// Takes out (string) which receives manifest.json and the files.
// Takes variants ([]string) which lists the variant directories to export, relative to
// examplesDirectory and sorted.
//
// Returns *Manifest which is the exported manifest.
// Returns error when a file cannot be copied or is larger than the export allows, or the
// context is done.
func writeExport(ctx context.Context, curriculum *Curriculum, examplesDirectory, out string, variants []string) (*Manifest, error) {
	if err := os.MkdirAll(out, exportDirPermissions); err != nil {
		return nil, err
	}
	target, err := os.OpenRoot(out)
	if err != nil {
		return nil, err
	}
	defer func() { _ = target.Close() }()
	manifest := &Manifest{
		Version:  ManifestVersion,
		Engines:  orderedEngines(curriculum),
		Columns:  curriculum.Columns,
		Tracks:   curriculum.Tracks,
		Concepts: curriculum.Concepts,
		Variants: nil,
	}
	directories := curriculum.VariantDirectories()
	remaining := int64(maxExportBytes)
	for _, relative := range variants {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("exporting %s: %w", relative, context.Cause(ctx))
		}
		exported, err := exportVariant(examplesDirectory, target, relative, directories[relative], &remaining)
		if err != nil {
			return nil, fmt.Errorf("exporting %s: %w", relative, err)
		}
		manifest.Variants = append(manifest.Variants, *exported)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return manifest, target.WriteFile("manifest.json", append(data, '\n'), exportFilePermissions)
}

// orderedEngines lists the engines in the order their columns first appear.
//
// Takes curriculum (*Curriculum) which holds the engines and columns.
//
// Returns []ExportedEngine which holds the engines with their names.
func orderedEngines(curriculum *Curriculum) []ExportedEngine {
	var engines []ExportedEngine
	for _, column := range curriculum.Columns {
		if slices.ContainsFunc(engines, func(engine ExportedEngine) bool { return engine.Name == column.Engine }) {
			continue
		}
		engines = append(engines, ExportedEngine{Engine: curriculum.Engines[column.Engine], Name: column.Engine})
	}
	return engines
}

// exportVariant copies one variant directory's files and describes them. It reads and
// writes through an os.Root on each side, so neither a file in the variant nor its path
// can reach outside the variant directory or the export. It copies only regular files, at
// most maxVariantFiles of them, none larger than maxFileBytes, and no more bytes in all
// than remaining, which it reduces by what it copies.
//
// Takes examplesDirectory (string) which is the examples directory.
// Takes target (*os.Root) which is the export directory and receives the files.
// Takes relative (string) which is the variant directory relative to examplesDirectory.
// Takes ref (VariantRef) which is the variant's concept and column.
// Takes remaining (*int64) which is how many more bytes the export may copy.
//
// Returns *ExportedVariant which describes the variant.
// Returns error when a file cannot be read or written, or a bound is exceeded.
func exportVariant(examplesDirectory string, target *os.Root, relative string, ref VariantRef, remaining *int64) (*ExportedVariant, error) {
	directory := filepath.Join(examplesDirectory, relative)
	variant, err := LoadVariant(directory)
	if err != nil {
		return nil, err
	}
	source, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer func() { _ = source.Close() }()
	files := source.FS()
	exported := &ExportedVariant{
		Variant:   *variant,
		Concept:   ref.Concept.Slug,
		Column:    ref.Column.ID,
		Engine:    ref.Column.Engine,
		Driver:    ref.Column.Driver,
		Directory: filepath.ToSlash(relative),
		Files:     nil,
	}
	err = fs.WalkDir(files, ".", func(inner string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if inner == VariantFile {
			return nil
		}
		if len(exported.Files) >= maxVariantFiles {
			return fmt.Errorf("%w: more than %d", ErrTooManyFiles, maxVariantFiles)
		}
		if copyErr := copyVariantFile(files, target, relative, inner, remaining); copyErr != nil {
			return copyErr
		}
		exported.Files = append(exported.Files, ExportedFile{Path: inner, Role: roleOf(inner), Lang: langOf(inner)})
		return nil
	})
	return exported, err
}

// copyVariantFile copies one regular file of a variant into the export, at the same path
// under the variant's directory, and takes its size from remaining.
//
// Takes files (fs.FS) which is the variant directory.
// Takes target (*os.Root) which is the export directory.
// Takes relative (string) which is the variant directory relative to the examples
// directory.
// Takes inner (string) which is the file's slash-separated path in the variant.
// Takes remaining (*int64) which is how many more bytes the export may copy.
//
// Returns error when the file cannot be read or written, is not a regular file, is larger
// than maxFileBytes, or does not fit in remaining.
func copyVariantFile(files fs.FS, target *os.Root, relative, inner string, remaining *int64) error {
	data, err := readRegular(files, inner)
	if err != nil {
		return err
	}
	if int64(len(data)) > *remaining {
		return fmt.Errorf("%s: %w: more than %d bytes", inner, ErrExportTooLarge, maxExportBytes)
	}
	*remaining -= int64(len(data))
	copied := filepath.Join(relative, filepath.FromSlash(inner))
	if err := target.MkdirAll(filepath.Dir(copied), exportDirPermissions); err != nil {
		return err
	}
	return target.WriteFile(copied, data, exportFilePermissions)
}

// roleOf derives a file's role from its path within a variant directory.
//
// Takes path (string) which is the slash-separated path.
//
// Returns string which is the role.
func roleOf(path string) string {
	switch {
	case path == "example.go":
		return RoleCode
	case path == "main.go":
		return RoleWiring
	case strings.HasSuffix(path, "_test.go"):
		return RoleTest
	case strings.HasPrefix(path, "internal/store/"):
		return RoleGenerated
	case strings.HasPrefix(path, "db/migrations/"):
		return RoleMigration
	case strings.HasPrefix(path, "db/queries/"):
		return RoleQuery
	case strings.HasPrefix(path, "db/seeds/"):
		return RoleSeed
	case path == "sgl.yaml":
		return RoleConfig
	case path == "output.txt":
		return RoleOutput
	case path == "transcript.txt":
		return RoleTranscript
	default:
		return RoleSupport
	}
}

// langOf derives a file's highlighting language from its extension.
//
// Takes path (string) which is the file's path.
//
// Returns string which is go, sql, yaml or text.
func langOf(path string) string {
	switch filepath.Ext(path) {
	case ".go":
		return "go"
	case ".sql":
		return "sql"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return "text"
	}
}
