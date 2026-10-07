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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoleOfDerivesTheRoleFromThePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{path: "example.go", want: RoleCode},
		{path: "main.go", want: RoleWiring},
		{path: "main_test.go", want: RoleTest},
		{path: "transcript_test.go", want: RoleTest},
		{path: "internal/store/models.go", want: RoleGenerated},
		{path: "internal/store/store_test.go", want: RoleTest},
		{path: "db/migrations/0001_init.up.sql", want: RoleMigration},
		{path: "db/queries/tasks.sql", want: RoleQuery},
		{path: "db/seeds/0001_tasks.sql", want: RoleSeed},
		{path: "sgl.yaml", want: RoleConfig},
		{path: "output.txt", want: RoleOutput},
		{path: "transcript.txt", want: RoleTranscript},
		{path: "README", want: RoleSupport},
		{path: "broken/sgl.yaml", want: RoleSupport},
		{path: "legacy/main.go", want: RoleSupport},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, roleOf(test.path))
		})
	}
}

func TestLangOfDerivesTheLanguageFromTheExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{path: "main.go", want: "go"},
		{path: "db/queries/tasks.sql", want: "sql"},
		{path: "sgl.yaml", want: "yaml"},
		{path: "adopt.yml", want: "yaml"},
		{path: "output.txt", want: "text"},
		{path: "README", want: "text"},
		{path: "Makefile.go.txt", want: "text"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, langOf(test.path))
		})
	}
}

func TestExportCopiesEveryVariantAndWritesTheManifest(t *testing.T) {
	t.Parallel()

	examples := writeExamples(t, nil)
	out := filepath.Join(t.TempDir(), "export")
	manifest, err := Export(t.Context(), examples, out)
	require.NoError(t, err)

	require.Equal(t, ManifestVersion, manifest.Version)
	engines := make([]string, 0, len(manifest.Engines))
	for _, engine := range manifest.Engines {
		engines = append(engines, engine.Name)
	}
	require.Equal(t, []string{"sqlite", "postgres", "cockroachdb"}, engines)
	require.Len(t, manifest.Variants, 2)

	tests := []struct {
		directory  string
		engine     string
		wantLive   string
		wantCodes  []string
		wantFilesN int
	}{
		{directory: "postgres/001_first_query", engine: "postgres", wantLive: "no", wantCodes: []string{}, wantFilesN: len(fixtureVariant("postgres", "[]")) - 1},
		{directory: "sqlite/001_first_query", engine: "sqlite", wantLive: "yes", wantCodes: []string{"Q022"}, wantFilesN: len(fixtureVariant("sqlite", "[]")) - 1},
	}
	for index, test := range tests {
		t.Run(test.directory, func(t *testing.T) {
			variant := manifest.Variants[index]
			require.Equal(t, test.directory, variant.Directory)
			require.Equal(t, test.engine, variant.Engine)
			require.Equal(t, "first-query", variant.Concept)
			require.Equal(t, test.engine, variant.Column)
			require.Equal(t, "database/sql", variant.Driver)
			require.Equal(t, test.wantLive, variant.Live)
			require.Equal(t, test.wantCodes, variant.ExpectedDiagnostics)
			require.Len(t, variant.Files, test.wantFilesN)
			for _, file := range variant.Files {
				require.NotEqual(t, VariantFile, file.Path)
				require.Equal(t, roleOf(file.Path), file.Role)
				require.Equal(t, langOf(file.Path), file.Lang)
				copied, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(test.directory), filepath.FromSlash(file.Path)))
				require.NoError(t, err)
				original, err := os.ReadFile(filepath.Join(examples, filepath.FromSlash(test.directory), filepath.FromSlash(file.Path)))
				require.NoError(t, err)
				require.Equal(t, original, copied)
			}
			require.NoFileExists(t, filepath.Join(out, filepath.FromSlash(test.directory), VariantFile))
		})
	}

	data, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	require.NoError(t, err)
	var written Manifest
	require.NoError(t, json.Unmarshal(data, &written))
	require.Equal(t, manifest.Version, written.Version)
	require.Len(t, written.Variants, len(manifest.Variants))
	require.Len(t, written.Concepts, 1)
}

func TestExportRefusesWhatItCannotExport(t *testing.T) {
	t.Parallel()

	stopped := errors.New("the export was stopped")
	tests := []struct {
		want     error
		prepare  func(t *testing.T, examples, out string)
		name     string
		wantText string
		cancel   bool
	}{
		{
			name:     "an output directory that is not empty",
			prepare:  func(t *testing.T, _, out string) { writeFiles(t, out, map[string]string{"manifest.json": "{}"}) },
			want:     ErrNotEmpty,
			wantText: "",
		},
		{
			name: "examples that are not valid",
			prepare: func(t *testing.T, examples, _ string) {
				require.NoError(t, os.Remove(filepath.Join(examples, "sqlite", "001_first_query", "output.txt")))
			},
			want:     nil,
			wantText: "the examples are not valid",
		},
		{
			name: "a curriculum with a misspelt key",
			prepare: func(t *testing.T, examples, _ string) {
				writeFiles(t, examples, map[string]string{ConceptsFile: "versio: 1\n"})
			},
			want:     nil,
			wantText: "field versio not found",
		},
		{
			name: "a variant file larger than the export copies",
			prepare: func(t *testing.T, examples, _ string) {
				writeFiles(t, examples, map[string]string{"sqlite/001_first_query/large.bin": string(bytes.Repeat([]byte("x"), maxFileBytes+1))})
			},
			want:     ErrFileTooLarge,
			wantText: "exporting sqlite/001_first_query: large.bin",
		},
		{
			name: "a variant with more files than the export copies",
			prepare: func(t *testing.T, examples, _ string) {
				files := make(map[string]string, maxVariantFiles)
				for index := range maxVariantFiles {
					files[fmt.Sprintf("sqlite/001_first_query/notes/%04d.txt", index)] = "x"
				}
				writeFiles(t, examples, files)
			},
			want:     ErrTooManyFiles,
			wantText: "",
		},
		{
			name:     "a context already done",
			prepare:  func(*testing.T, string, string) {},
			cancel:   true,
			want:     stopped,
			wantText: "exporting postgres/001_first_query",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			examples, out := writeExamples(t, nil), t.TempDir()
			test.prepare(t, examples, out)
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			if test.cancel {
				cancel(stopped)
			}
			manifest, err := Export(ctx, examples, out)
			require.Error(t, err)
			require.Nil(t, manifest)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
			}
			require.ErrorContains(t, err, filepath.FromSlash(test.wantText))
		})
	}
}

func TestExportVariantCopiesOnlyRegularFilesWithinItsBudget(t *testing.T) {
	t.Parallel()

	examples := writeExamples(t, nil)
	size := int64(0)
	for name, content := range fixtureVariant("sqlite", "[Q022]") {
		if name != VariantFile {
			size += int64(len(content))
		}
	}
	tests := []struct {
		want      error
		name      string
		remaining int64
		wantLeft  int64
	}{
		{name: "a budget the variant fits", remaining: size + 10, want: nil, wantLeft: 10},
		{name: "a budget the variant exactly fills", remaining: size, want: nil, wantLeft: 0},
		{name: "a budget the variant overruns", remaining: size - 1, want: ErrExportTooLarge, wantLeft: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			target, err := os.OpenRoot(t.TempDir())
			require.NoError(t, err)
			defer func() { require.NoError(t, target.Close()) }()
			curriculum, err := Load(examples)
			require.NoError(t, err)
			relative := filepath.Join("sqlite", "001_first_query")
			remaining := test.remaining
			exported, err := exportVariant(examples, target, relative, curriculum.VariantDirectories()[relative], &remaining)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantLeft, remaining)
			require.NotEmpty(t, exported.Files)
		})
	}

	t.Run("a link to a directory", func(t *testing.T) {
		t.Parallel()

		linked := t.TempDir()
		writeFiles(t, linked, map[string]string{"sqlite/001_first_query/" + VariantFile: "summary: A first query.\n"})
		if err := os.Symlink("db", filepath.Join(linked, "sqlite", "001_first_query", "link")); err != nil {
			t.Skipf("cannot make a symbolic link here: %v", err)
		}
		require.NoError(t, os.MkdirAll(filepath.Join(linked, "sqlite", "001_first_query", "db"), 0o750))
		target, err := os.OpenRoot(t.TempDir())
		require.NoError(t, err)
		defer func() { require.NoError(t, target.Close()) }()
		remaining := int64(maxExportBytes)
		_, err = exportVariant(linked, target, filepath.Join("sqlite", "001_first_query"), VariantRef{Concept: &Concept{Number: 1, Slug: "first-query"}, Column: Column{ID: "sqlite", Engine: "sqlite"}}, &remaining)
		require.ErrorIs(t, err, ErrNotRegular)
		require.ErrorContains(t, err, "link")
	})
}
