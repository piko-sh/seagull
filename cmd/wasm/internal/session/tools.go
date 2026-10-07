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

package session

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

const (
	// vcsRevisionKey is the build setting that holds the commit a binary was built from.
	vcsRevisionKey = "vcs.revision"

	// maxCodeRunes is the longest diagnostic code Explain looks up; a code such as " q012 "
	// is far shorter.
	maxCodeRunes = 32

	// maxFormatPrefixes is the most directive prefixes a FormatRequest may list.
	maxFormatPrefixes = 16

	// maxFormatNameRunes is the longest dialect name or prefix a FormatRequest may hold.
	maxFormatNameRunes = 64

	// excerptEllipsis ends an excerpt of text that continues past the cut.
	excerptEllipsis = "..."
)

// VersionView describes the build.
type VersionView struct {
	// Version is the Seagull library's version, or devel.
	Version string `json:"version"`

	// Revision is the commit the binary was built from, or empty.
	Revision string `json:"revision"`

	// GoVersion is the Go release the binary was built with.
	GoVersion string `json:"goVersion"`
}

// ExplainView documents one diagnostic code.
type ExplainView struct {
	// Code is the code, such as Q001.
	Code string `json:"code"`

	// Title is the code's short title.
	Title string `json:"title"`

	// Severity is the severity the code is normally reported with: error, warning or hint.
	Severity string `json:"severity"`

	// Explanation is a Markdown body describing the code.
	Explanation string `json:"explanation"`
}

// FormatRequest asks for one SQL file's directives to be rewritten into canonical form,
// as sgl fmt does.
type FormatRequest struct {
	// Source is the SQL file's content.
	Source string `json:"source"`

	// Dialect names the built-in dialect whose comment, string and quoting rules the file is
	// read with, as sgl fmt reads each database's files; empty reads it with neutral rules.
	Dialect string `json:"dialect"`

	// Canonical is the prefix directives are rewritten to; empty means the first of
	// Prefixes.
	Canonical string `json:"canonical"`

	// Prefixes lists the accepted directive prefixes, canonical first, as sgl.yaml's
	// directives.prefixes does; empty means sgl and seagull.
	Prefixes []string `json:"prefixes"`
}

// FormatResponse is the outcome of a FormatRequest.
type FormatResponse struct {
	// Source is the formatted content, or empty when Error is set.
	Source string `json:"source"`

	// Error describes why the source could not be formatted, or is empty.
	Error string `json:"error"`
}

// Version describes the build.
//
// Returns VersionView which holds the library version, revision and Go release.
func Version() VersionView {
	revision := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == vcsRevisionKey {
				revision = setting.Value
			}
		}
	}
	return VersionView{Version: seagull.Version, Revision: revision, GoVersion: runtime.Version()}
}

// Explain documents a diagnostic code.
//
// Takes code (string) which names the code in any case, with or without its letter and
// zero padding, such as "Q012", "q12" or "12".
//
// Returns ExplainView which documents it.
// Returns error when no code matches, quoting at most the code's first maxCodeRunes
// characters.
func Explain(code string) (ExplainView, error) {
	if utf8.RuneCountInString(code) > maxCodeRunes {
		return ExplainView{}, fmt.Errorf("unknown diagnostic code %q", excerpt(code, maxCodeRunes))
	}
	info, found := seagull.LookupCode(code)
	if !found {
		return ExplainView{}, fmt.Errorf("unknown diagnostic code %q", code)
	}
	return explainView(info), nil
}

// Codes documents every diagnostic code in ascending order.
//
// Returns []ExplainView which holds one entry per code.
func Codes() []ExplainView {
	codes := seagull.Codes()
	views := make([]ExplainView, 0, len(codes))
	for _, info := range codes {
		views = append(views, explainView(info))
	}
	return views
}

// Format rewrites the directives of a SQL file into canonical form, as sgl fmt does. A
// source larger than MaxFileBytes, or more than maxFormatPrefixes prefixes, is refused
// before anything is read.
//
// Takes request (FormatRequest) which holds the source and the prefixes.
//
// Returns FormatResponse which holds the formatted source, or the error.
func Format(request FormatRequest) FormatResponse {
	if err := checkFormatRequest(&request); err != nil {
		return FormatResponse{Source: "", Error: err.Error()}
	}
	prefixes := request.Prefixes
	if len(prefixes) == 0 {
		prefixes = config.DefaultPrefixes()
	}
	canonical := request.Canonical
	if canonical == "" {
		canonical = prefixes[0]
	}
	aliases := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		if prefix != canonical && !slices.Contains(aliases, prefix) {
			aliases = append(aliases, prefix)
		}
	}
	formatted, err := formatDirectives(request.Dialect, []byte(request.Source), canonical, aliases)
	if err != nil {
		return FormatResponse{Source: "", Error: err.Error()}
	}
	return FormatResponse{Source: string(formatted), Error: ""}
}

// excerpt returns up to maxRunes runes from the start of text, cut on a rune boundary so
// no character is split, followed by an ellipsis when text continues past the cut. Each
// run of invalid UTF-8 is replaced with U+FFFD, so the excerpt always prints cleanly.
//
// Takes text (string) which is the text to quote.
// Takes maxRunes (int) which is the most runes of text to keep.
//
// Returns string which is the excerpt.
func excerpt(text string, maxRunes int) string {
	end := 0
	for range maxRunes {
		if end >= len(text) {
			break
		}
		_, width := utf8.DecodeRuneInString(text[end:])
		end += width
	}
	quoted := strings.ToValidUTF8(text[:end], string(utf8.RuneError))
	if end < len(text) {
		return quoted + excerptEllipsis
	}
	return quoted
}

// checkFormatRequest checks that a format request is no larger than the playground takes.
//
// Takes request (*FormatRequest) which is checked.
//
// Returns error which wraps errOverLimit when the source is larger than MaxFileBytes,
// more than maxFormatPrefixes prefixes are listed, or the dialect or a prefix is longer
// than maxFormatNameRunes.
func checkFormatRequest(request *FormatRequest) error {
	if len(request.Source) > MaxFileBytes {
		return fmt.Errorf("the source is %d bytes, %w: files of at most %d bytes", len(request.Source), errOverLimit, MaxFileBytes)
	}
	if len(request.Prefixes) > maxFormatPrefixes {
		return fmt.Errorf("%d directive prefixes are %w: at most %d", len(request.Prefixes), errOverLimit, maxFormatPrefixes)
	}
	names := append([]string{request.Dialect, request.Canonical}, request.Prefixes...)
	for _, name := range names {
		if length := utf8.RuneCountInString(name); length > maxFormatNameRunes {
			return fmt.Errorf("a dialect or prefix of %d characters is %w: at most %d", length, errOverLimit, maxFormatNameRunes)
		}
	}
	return nil
}

// formatDirectives formats source with the named dialect's lexical rules, or with neutral
// rules when no dialect is named.
//
// Takes dialectName (string) which names a built-in dialect, or is empty.
// Takes source ([]byte) which is the SQL file's content.
// Takes canonical (string) which is the prefix directives are rewritten to.
// Takes aliases ([]string) which are the prefixes rewritten to canonical.
//
// Returns []byte which is the formatted content.
// Returns error when the dialect is unknown or the prefixes are invalid.
func formatDirectives(dialectName string, source []byte, canonical string, aliases []string) ([]byte, error) {
	if dialectName == "" {
		return seagull.FormatDirectives(source, canonical, aliases...)
	}
	built, err := dialect_registry.New(dialectName, dialect_registry.Options{})
	if err != nil {
		return nil, err
	}
	return seagull.FormatDirectivesFor(built, source, canonical, aliases...)
}

// explainView converts a code's documentation.
//
// Takes info (seagull.CodeInfo) which documents the code.
//
// Returns ExplainView which holds the same.
func explainView(info seagull.CodeInfo) ExplainView {
	return ExplainView{
		Code:        info.Code,
		Title:       info.Title,
		Severity:    info.Severity.String(),
		Explanation: strings.TrimSpace(info.Explanation),
	}
}
