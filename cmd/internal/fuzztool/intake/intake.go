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

package intake

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/cmd/internal/fuzztool/fuzztext"
)

const (
	// maxFileBytes bounds one suite file.
	maxFileBytes = 1 << 20

	// maxFiles bounds the files read from one suite.
	maxFiles = 5000

	// maxSeedRunes bounds one seed's migration and query together.
	maxSeedRunes = 16 << 10

	// maxMigrationStatements bounds the schema statements kept before a query.
	maxMigrationStatements = 64

	// queryDirective introduces every admitted query.
	queryDirective = "-- sgl.query(External, many)\n"

	// adaptation describes how a suite's SQL becomes seeds.
	adaptation = "each query, with the schema statements read before it, as a FuzzGenerate seed " +
		"under an sgl.query directive; onlyif and skipif select its dialects"

	// directoryMode and fileMode are the permissions of written output.
	directoryMode = 0o750

	// fileMode is the permission of a written file.
	fileMode = 0o600
)

// ErrOutputExists refuses to write over an earlier intake.
var ErrOutputExists = errors.New("the output directory already exists")

// Report counts what an intake admitted and refused.
type Report struct {
	// Admitted holds each admitted file.
	Admitted []AdmittedFile `json:"admitted"`

	// Rejected holds each refused file and why.
	Rejected []RejectedFile `json:"rejected"`

	// BeyondFileLimit counts files past the file limit, which are not listed.
	BeyondFileLimit int `json:"beyond_file_limit"`

	// Duplicates counts seeds already admitted.
	Duplicates int `json:"duplicates"`

	// Seeds counts the admitted seeds.
	Seeds int `json:"seeds"`
}

// AdmittedFile records one admitted suite file.
type AdmittedFile struct {
	// Path is the file's path in the suite.
	Path string `json:"path"`

	// SHA256 is the hash of the admitted content.
	SHA256 string `json:"sha256"`

	// Bytes is the file's size.
	Bytes int `json:"bytes"`

	// Seeds counts the seeds the file yielded.
	Seeds int `json:"seeds"`
}

// RejectedFile records one refused suite file.
type RejectedFile struct {
	// Path is the file's path in the suite.
	Path string `json:"path"`

	// Reason says why it was refused.
	Reason string `json:"reason"`
}

// seed is one line of seeds.jsonl.
type seed struct {
	// Migration holds the schema statements read before the query.
	Migration string `json:"migration"`

	// Query is the directive and query.
	Query string `json:"query"`

	// Dialects are the dialects the seed applies to.
	Dialects []string `json:"dialects"`

	// Origin is where the query came from.
	Origin origin `json:"origin"`
}

// origin is the place in the suite a seed came from.
type origin struct {
	// Path is the file's path in the suite.
	Path string `json:"path"`

	// Line is the query's first line.
	Line int `json:"line"`
}

// suiteFile is one listed file, with an empty absolute path when it was refused.
type suiteFile struct {
	// relative is the path within the suite.
	relative string

	// absolute is the path to read, empty for a link or an escape.
	absolute string
}

// intakeRun holds the state of one intake.
type intakeRun struct {
	// seen holds the hash of every admitted seed.
	seen map[string]bool

	// seeds receives seeds.jsonl.
	seeds io.Writer

	// output is the intake directory.
	output string

	// manifest describes the suite.
	manifest Manifest

	// report counts what was admitted and refused.
	report Report
}

// Run reads a suite and writes its seeds, originals and intake manifest.
//
// Takes source (string) which is the suite directory.
// Takes manifest (Manifest) which describes the suite.
// Takes output (string) which is a new directory for the intake.
//
// Returns Report which counts what was admitted and refused.
// Returns error which is ErrOutputExists when the output exists or reports a failure
// reading the suite or writing the output.
func Run(source string, manifest Manifest, output string) (Report, error) {
	if _, err := os.Lstat(output); err == nil {
		return Report{}, ErrOutputExists
	}
	files, err := listSuite(source)
	if err != nil {
		return Report{}, err
	}
	if err := os.MkdirAll(filepath.Join(output, "originals"), directoryMode); err != nil {
		return Report{}, fmt.Errorf("creating %s: %w", output, err)
	}
	seeds, err := os.OpenFile(filepath.Join(output, "seeds.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode) //nolint:gosec // Inside the new output.
	if err != nil {
		return Report{}, fmt.Errorf("creating the seeds file: %w", err)
	}
	run := intakeRun{
		seen:     make(map[string]bool),
		seeds:    seeds,
		output:   output,
		manifest: manifest,
		report:   Report{Admitted: []AdmittedFile{}, Rejected: []RejectedFile{}, BeyondFileLimit: 0, Duplicates: 0, Seeds: 0},
	}
	for count, file := range files {
		if count >= maxFiles {
			run.report.BeyondFileLimit++
			continue
		}
		if err := run.admit(file); err != nil {
			_ = seeds.Close()
			return Report{}, err
		}
	}
	if err := seeds.Close(); err != nil {
		return Report{}, fmt.Errorf("writing the seeds file: %w", err)
	}
	return run.report, run.writeManifest()
}

// admit reads one suite file and writes the seeds it yields.
//
// Takes file (suiteFile) which is the listed file.
//
// Returns error when the output cannot be written.
func (run *intakeRun) admit(file suiteFile) error {
	if file.absolute == "" {
		run.reject(file.relative, "a link or a path outside the source")
		return nil
	}
	content, reason := readBounded(file.absolute)
	if reason != "" {
		run.reject(file.relative, reason)
		return nil
	}
	if !utf8.Valid(content) {
		run.reject(file.relative, "not UTF-8")
		return nil
	}
	yielded, err := run.writeSeeds(file.relative, string(content))
	if err != nil {
		return err
	}
	original := filepath.Join(run.output, "originals", file.relative)
	if err := os.MkdirAll(filepath.Dir(original), directoryMode); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(original), err)
	}

	if err := writeNew(original, content); err != nil {
		return err
	}
	digest := sha256.Sum256(content)
	run.report.Admitted = append(run.report.Admitted, AdmittedFile{
		Path: file.relative, SHA256: hex.EncodeToString(digest[:]), Bytes: len(content), Seeds: yielded,
	})
	run.report.Seeds += yielded
	return nil
}

// writeSeeds writes the seeds of one file's queries.
//
// Takes relative (string) which is the file's path in the suite.
// Takes text (string) which is the file's content.
//
// Returns int which counts the seeds written.
// Returns error when the seeds file cannot be written.
func (run *intakeRun) writeSeeds(relative string, text string) (int, error) {
	records := plainRecords(text)
	if run.manifest.Format == formatLogic {
		records = logicRecords(text)
	}
	var schema []string
	yielded := 0
	for _, record := range records {
		if !record.query {
			schema = addSchema(schema, record.sql)
			continue
		}
		admitted, isAdmitted := run.seedFor(record, schema)
		if !isAdmitted {
			continue
		}
		migration, query := admitted.Migration, admitted.Query
		key := sha256.Sum256([]byte(migration + "\x00" + query))
		if run.seen[string(key[:])] {
			run.report.Duplicates++
			continue
		}
		run.seen[string(key[:])] = true
		admitted.Origin = origin{Path: relative, Line: record.line}
		encoded, err := fuzztext.MarshalASCII(admitted, false)
		if err != nil {
			return 0, err
		}
		if _, err := run.seeds.Write(append(encoded, '\n')); err != nil {
			return 0, fmt.Errorf("writing the seeds file: %w", err)
		}
		yielded++
	}
	return yielded, nil
}

// reject records a refused file.
//
// Takes relative (string) which is the file's path in the suite.
// Takes reason (string) which says why.
func (run *intakeRun) reject(relative string, reason string) {
	run.report.Rejected = append(run.report.Rejected, RejectedFile{Path: relative, Reason: reason})
}

// writeManifest writes the intake manifest with the suite's fields, the adaptation
// and the report.
//
// Returns error when the manifest cannot be written.
func (run *intakeRun) writeManifest() error {
	record := make(map[string]any, len(requiredFields)+1)
	for _, field := range requiredFields {
		record[field] = run.manifest.fields[field]
	}
	record["adaptation"] = adaptation
	record["admitted"] = run.report.Admitted
	record["rejected"] = run.report.Rejected
	record["beyond_file_limit"] = run.report.BeyondFileLimit
	record["duplicates"] = run.report.Duplicates
	record["seeds"] = run.report.Seeds
	encoded, err := fuzztext.MarshalASCII(record, true)
	if err != nil {
		return err
	}
	return writeNew(filepath.Join(run.output, "manifest.json"), append(encoded, '\n'))
}

// listSuite lists a suite's files depth first, sorted, refusing links and escapes.
//
// Directory links are not followed and unreadable directories are skipped.
//
// Takes source (string) which is the suite directory.
//
// Returns []suiteFile which holds every listed file.
// Returns error when the suite directory cannot be resolved.
func listSuite(source string) ([]suiteFile, error) {
	root, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", source, err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", source, err)
	}
	var files []suiteFile
	listDirectory(root, root, &files)
	return files, nil
}

// listDirectory appends a directory's files, then its subdirectories' files.
//
// Takes root (string) which is the resolved suite directory.
// Takes directory (string) which is the directory to list.
// Takes files (*[]suiteFile) which receives each file.
func listDirectory(root string, directory string, files *[]suiteFile) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	var subdirectories []string
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		info, err := os.Stat(path)
		isLink := entry.Type()&fs.ModeSymlink != 0
		if err == nil && info.IsDir() {
			if !isLink {
				subdirectories = append(subdirectories, path)
			}
			continue
		}
		relative, _ := filepath.Rel(root, path)
		resolved, err := filepath.EvalSymlinks(path)
		if isLink || err != nil || !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
			*files = append(*files, suiteFile{relative: relative, absolute: ""})
			continue
		}
		*files = append(*files, suiteFile{relative: relative, absolute: path})
	}
	slices.Sort(subdirectories)
	for _, subdirectory := range subdirectories {
		listDirectory(root, subdirectory, files)
	}
}

// readBounded reads a regular file of at most maxFileBytes.
//
// Takes path (string) which is the file to read.
//
// Returns []byte which holds the content, nil when refused.
// Returns string which says why the file was refused, empty when it was read.
func readBounded(path string) ([]byte, string) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, "not a regular file"
	}
	file, err := os.Open(path) //nolint:gosec // A listed suite file inside the source.
	if err != nil {
		return nil, "not a regular file"
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return nil, "not a regular file"
	}
	if len(content) > maxFileBytes {
		return nil, "larger than the file bound"
	}
	return content, ""
}

// writeNew writes a file that must not exist yet.
//
// Takes path (string) which is the file to create.
// Takes content ([]byte) which is what to write.
//
// Returns error when the file exists or cannot be written.
func writeNew(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode) //nolint:gosec // Inside the new output.
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// seedFor builds a query's seed from the schema statements read before it.
//
// Takes record (sqlRecord) which is the query.
// Takes schema ([]string) which holds the schema statements read so far.
//
// Returns seed which holds the migration, query and dialects, without its origin.
// Returns bool which is false when no dialect applies or the seed is too large.
func (run *intakeRun) seedFor(record sqlRecord, schema []string) (seed, bool) {
	dialects := applies(run.manifest.Dialects, record.conditions, run.manifest.aliases)
	migration := ""
	if len(schema) > 0 {
		migration = strings.Join(schema, ";\n") + ";\n"
	}
	query := queryDirective + strings.TrimRight(record.sql, ";") + ";\n"
	if len(dialects) == 0 || utf8.RuneCountInString(migration)+utf8.RuneCountInString(query) > maxSeedRunes {
		return seed{}, false
	}
	return seed{Migration: migration, Query: query, Dialects: dialects, Origin: origin{Path: "", Line: 0}}, true
}

// addSchema keeps a statement that changes the schema, bounded to the latest ones.
//
// Takes schema ([]string) which holds the schema statements read so far.
// Takes sql (string) which is the statement.
//
// Returns []string which holds the kept statements.
func addSchema(schema []string, sql string) []string {
	if !schemaStatement.MatchString(sql) {
		return schema
	}
	schema = append(schema, sql)
	return schema[max(0, len(schema)-maxMigrationStatements):]
}
