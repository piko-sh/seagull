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
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// vetFindingsExit is the status sgl vet exits with when it reports an error; any other
	// non-zero status means vet itself failed.
	vetFindingsExit = 1

	// severityError is the severity of a finding that makes sgl vet exit 1.
	severityError = "error"
)

// VetResult is the outcome of checking one variant's expected_diagnostics.
type VetResult struct {
	// Variant is the variant's directory, relative to the examples directory, such as
	// sqlite/014_runtime_builder.
	Variant string

	// Skipped, when set, is why the variant was not vetted.
	Skipped string

	// Failure, when set, is why sgl vet could not report on the variant.
	Failure string

	// Expected lists the codes example.yaml expects, sorted and without repeats.
	Expected []string

	// Reported lists the codes sgl vet reported, sorted and without repeats.
	Reported []string

	// Missing lists the expected codes sgl vet did not report.
	Missing []string

	// Unexpected lists the codes sgl vet reported that example.yaml does not expect.
	Unexpected []string
}

// Differs reports whether the variant failed the check: sgl vet could not run, or the
// codes it reported are not the codes the variant expects.
//
// Returns bool which is true when the variant needs attention.
func (r *VetResult) Differs() bool {
	return r.Failure != "" || len(r.Missing) > 0 || len(r.Unexpected) > 0
}

// String describes the result for a report.
//
// Returns string which names the variant and what was wrong, or that it matched.
func (r *VetResult) String() string {
	switch {
	case r.Skipped != "":
		return r.Variant + ": skipped; " + r.Skipped
	case r.Failure != "":
		return r.Variant + ": " + r.Failure
	case !r.Differs():
		return r.Variant + ": reports " + codeList(r.Reported) + " as expected"
	}
	parts := make([]string, 0, 2)
	if len(r.Missing) > 0 {
		parts = append(parts, "missing "+strings.Join(r.Missing, ", "))
	}
	if len(r.Unexpected) > 0 {
		parts = append(parts, "unexpected "+strings.Join(r.Unexpected, ", "))
	}
	return fmt.Sprintf("%s: %s (expected_diagnostics %s, sgl vet reported %s)",
		r.Variant, strings.Join(parts, "; "), codeList(r.Expected), codeList(r.Reported))
}

// vetSpecial says how the diagnostics check treats a variant that the stock sgl cannot
// vet in the variant's own directory.
type vetSpecial struct {
	// overlay maps a file in the variant to the file in the variant copied over it before
	// sgl vet runs, as the variant's transcript does. The copy is made in a temporary
	// directory, never in the variant.
	overlay map[string]string

	// skip, when set, is why the variant is not vetted at all.
	skip string

	// ownSGL runs the variant's own main package, which is a custom sgl, in place of the
	// stock one, as hack/generate/examples.sh does when it generates the variant.
	ownSGL bool
}

// vetSpecials lists the variants the diagnostics check cannot treat like the others, by
// directory relative to the examples directory. A variant with more than one sgl.yaml,
// such as tooling/041's broken/ project, needs no entry: every sgl.yaml it holds is
// vetted, as its transcript does with -c.
var vetSpecials = map[string]vetSpecial{
	"embedding/050_schema_from_live_database": {
		skip: "schema_from: file:club.db reads a live database that is not committed (the program builds one from legacy/schema.sql when it runs), and sgl refuses a schema source that does not exist",
	},
	"embedding/053_custom_sgl_binary": {
		ownSGL: true,
	},
	"tooling/042_ci_pipeline": {
		overlay: map[string]string{"db/queries/tasks.sql": "change/tasks.sql"},
	},
}

// vetFinding is the part of one line of sgl vet --format json the check reads.
type vetFinding struct {
	// Code is the diagnostic code, such as Q022.
	Code string `json:"code"`

	// Severity is error, warning or hint.
	Severity string `json:"severity"`

	// Message describes the problem, for a finding that has no code.
	Message string `json:"message"`
}

// CompareCodes compares the codes sgl vet reported with the codes a variant expects, as
// sets: order and repeats do not matter.
//
// Takes expected ([]string) which is the variant's expected_diagnostics.
// Takes reported ([]string) which is what sgl vet reported.
//
// Returns missing ([]string) which lists the expected codes not reported, sorted.
// Returns unexpected ([]string) which lists the reported codes not expected, sorted.
func CompareCodes(expected, reported []string) (missing, unexpected []string) {
	want := codeSet(expected)
	got := codeSet(reported)
	for _, code := range sortedCodes(want) {
		if _, ok := got[code]; !ok {
			missing = append(missing, code)
		}
	}
	for _, code := range sortedCodes(got) {
		if _, ok := want[code]; !ok {
			unexpected = append(unexpected, code)
		}
	}
	return missing, unexpected
}

// ParseVetJSON reads the output of sgl vet --format json, one JSON object per finding.
//
// Takes output ([]byte) which is what sgl vet wrote to standard output.
//
// Returns codes ([]string) which lists each finding's code, in output order.
// Returns hasError (bool) which is true when a finding is an error, the only reason sgl
// vet exits 1 after printing its findings.
// Returns err (error) when a line is not a finding or a finding has no code.
func ParseVetJSON(output []byte) (codes []string, hasError bool, err error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var finding vetFinding
		if decodeErr := decoder.Decode(&finding); decodeErr != nil {
			if errors.Is(decodeErr, io.EOF) {
				return codes, hasError, nil
			}
			return nil, false, fmt.Errorf("reading sgl vet --format json: %w", decodeErr)
		}
		if finding.Code == "" {
			return nil, false, fmt.Errorf("sgl vet reported a finding without a code: %q", finding.Message)
		}
		codes = append(codes, finding.Code)
		hasError = hasError || finding.Severity == severityError
	}
}

// CheckDiagnostics runs sgl vet --format json on every variant directory in
// examplesDirectory that holds an sgl.yaml, and compares the codes it reports with the
// variant's expected_diagnostics. A variant with more than one sgl.yaml is vetted once
// for each, with -c, and the codes are pooled. The variants in vetSpecials are skipped,
// vetted with their own sgl, or vetted with the change their transcript makes first, in a
// copy made under scratch. The check stops before the next variant once the context is
// done.
//
// Takes examplesDirectory (string) which is the examples directory.
// Takes scratch (string) which is an empty or absent directory the check may copy
// variants into; the caller removes it.
// Takes sgl (string) which is the sgl binary to run.
// Takes goTool (string) which is the go command, for the variants that are their own sgl.
// Takes run (CommandRunner) which runs each sgl, such as RunCommand.
//
// Returns []VetResult which holds one result per variant, in directory order.
// Returns error when the variants cannot be listed, scratch is not empty, or the context
// is done; a variant that differs, or that sgl cannot vet, is a result, not an error.
func CheckDiagnostics(ctx context.Context, examplesDirectory, scratch, sgl, goTool string, run CommandRunner) ([]VetResult, error) {
	variants, err := vetVariants(examplesDirectory)
	if err != nil {
		return nil, err
	}
	for name := range vetSpecials {
		if !slices.Contains(variants, name) {
			return nil, fmt.Errorf("vetSpecials names %s, which is not a variant with an sgl.yaml", name)
		}
	}
	if err := requireEmpty(scratch); err != nil {
		return nil, err
	}

	results := make([]VetResult, 0, len(variants))
	for _, variant := range variants {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("vetting %s: %w", variant, context.Cause(ctx))
		}
		special := vetSpecials[variant]
		result := VetResult{Variant: variant, Skipped: special.skip}
		if special.skip == "" {
			command := []string{sgl}
			if special.ownSGL {
				command = []string{goTool, "run", "."}
			}
			checkVariant(ctx, &result, filepath.Join(examplesDirectory, variant), filepath.Join(scratch, filepath.FromSlash(variant)), command, special.overlay, run)
		}
		results = append(results, result)
	}
	return results, nil
}

// codeList renders codes as a YAML flow list, such as [Q009, Q027].
//
// Takes codes ([]string) which are rendered.
//
// Returns string which is the list.
func codeList(codes []string) string {
	return "[" + strings.Join(codes, ", ") + "]"
}

// codeSet collects codes into a set.
//
// Takes codes ([]string) which are collected.
//
// Returns map[string]struct{} which holds each code once.
func codeSet(codes []string) map[string]struct{} {
	set := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		set[code] = struct{}{}
	}
	return set
}

// sortedCodes lists a set's codes in order.
//
// Takes set (map[string]struct{}) which holds the codes.
//
// Returns []string which is sorted, or nil for an empty set.
func sortedCodes(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(set))
}

// vetVariants lists every variant directory in examplesDirectory that holds an sgl.yaml.
//
// Takes examplesDirectory (string) which is the examples directory.
//
// Returns []string which holds the directories relative to examplesDirectory, slash
// separated and sorted.
// Returns error when the directories cannot be listed.
func vetVariants(examplesDirectory string) ([]string, error) {
	directories, err := filepath.Glob(filepath.Join(examplesDirectory, "*", "[0-9][0-9][0-9]_*"))
	if err != nil {
		return nil, err
	}
	variants := make([]string, 0, len(directories))
	for _, directory := range directories {
		if info, statErr := os.Stat(filepath.Join(directory, configFile)); statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		relative, err := filepath.Rel(examplesDirectory, directory)
		if err != nil {
			return nil, err
		}
		variants = append(variants, filepath.ToSlash(relative))
	}
	slices.Sort(variants)
	return variants, nil
}

// checkVariant vets one variant and fills in its result.
//
// Takes result (*VetResult) which receives the outcome.
// Takes directory (string) which is the variant's directory.
// Takes scratch (string) which is where a copy is made when overlay is not empty; it must
// not exist.
// Takes command ([]string) which is the sgl to run, with any leading arguments.
// Takes overlay (map[string]string) which maps files to the files copied over them.
// Takes run (CommandRunner) which runs sgl.
func checkVariant(ctx context.Context, result *VetResult, directory, scratch string, command []string, overlay map[string]string, run CommandRunner) {
	variant, err := LoadVariant(directory)
	if err != nil {
		result.Failure = err.Error()
		return
	}
	result.Expected = sortedCodes(codeSet(variant.ExpectedDiagnostics))

	if len(overlay) > 0 {
		if err := prepareOverlay(directory, scratch, overlay); err != nil {
			result.Failure = "applying the transcript's change: " + err.Error()
			return
		}
		directory = scratch
	}
	configs, err := variantConfigs(directory)
	if err != nil {
		result.Failure = err.Error()
		return
	}
	var reported []string
	for _, config := range configs {
		codes, err := runVet(ctx, directory, config, command, run)
		if err != nil {
			result.Failure = err.Error()
			return
		}
		reported = append(reported, codes...)
	}
	result.Reported = sortedCodes(codeSet(reported))
	result.Missing, result.Unexpected = CompareCodes(result.Expected, result.Reported)
}

// variantConfigs lists every sgl.yaml in a variant, the variant's own first.
//
// Takes directory (string) which is the variant's directory.
//
// Returns []string which holds the configurations' paths relative to directory, slash
// separated.
// Returns error when the directory cannot be walked.
func variantConfigs(directory string) ([]string, error) {
	var nested []string
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != configFile || path == filepath.Join(directory, configFile) {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		nested = append(nested, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(nested)
	return append([]string{configFile}, nested...), nil
}

// runVet runs sgl vet --format json on one configuration and returns the codes it
// reported.
//
// Takes directory (string) which is the directory sgl runs in.
// Takes config (string) which is the configuration, relative to directory.
// Takes command ([]string) which is the sgl to run, with any leading arguments.
// Takes run (CommandRunner) which runs sgl.
//
// Returns []string which lists the reported codes.
// Returns error when sgl fails for any reason other than reporting an error finding.
func runVet(ctx context.Context, directory, config string, command []string, run CommandRunner) ([]string, error) {
	arguments := slices.Concat(command, []string{"-c", config, "vet", "--format", "json"})
	stdout, stderr, runErr := run(ctx, directory, arguments)

	described := strings.Join(arguments, " ")
	status, exited := exitStatus(runErr)
	if runErr != nil && !exited {
		return nil, fmt.Errorf("running %s: %w", described, runErr)
	}
	codes, hasError, parseErr := ParseVetJSON(stdout)
	switch {
	case parseErr != nil:
		return nil, fmt.Errorf("%s: %w; stderr: %s", described, parseErr, strings.TrimSpace(string(stderr)))
	case runErr != nil && (status != vetFindingsExit || !hasError):
		return nil, fmt.Errorf("%s exited %d: %s", described, status, strings.TrimSpace(string(stderr)))
	}
	return codes, nil
}

// prepareOverlay copies a variant into target and copies each overlay file over the file
// it replaces.
//
// Takes source (string) which is the variant's directory.
// Takes target (string) which receives the copy; it must not exist.
// Takes overlay (map[string]string) which maps each file to replace to the file, also in
// the variant, that replaces it.
//
// Returns error when the copy fails.
func prepareOverlay(source, target string, overlay map[string]string) error {
	if err := os.CopyFS(target, os.DirFS(source)); err != nil {
		return err
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for _, replaced := range slices.Sorted(maps.Keys(overlay)) {
		data, err := root.ReadFile(overlay[replaced])
		if err != nil {
			return err
		}
		if err := root.WriteFile(replaced, data, configPermissions); err != nil {
			return err
		}
	}
	return nil
}
