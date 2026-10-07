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

package cli

import (
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

const (
	// fileURIScheme opens a SQLite URI filename, such as file:app.db?mode=ro.
	fileURIScheme = "file:"

	// memoryDatabase is the SQLite file name of a private in-memory database.
	memoryDatabase = ":memory:"

	// parameterSeparator starts the driver parameters after a SQLite file name.
	parameterSeparator = "?"
)

var (
	// urlScheme matches the scheme and "://" that open a URL, such as libsql://, which names
	// a remote database rather than a file.
	urlScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

	// uriPathEscaper escapes the characters that would end or escape the path of a SQLite
	// URI filename.
	uriPathEscaper = strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23")

	// inMemoryParameters lists the query parameters that make a SQLite connection string
	// name an in-memory database rather than a file.
	inMemoryParameters = []string{"mode=memory", "vfs=memdb"}
)

// requireSchemaFile refuses a SQLite schema source whose file does not exist. Opening it
// would create an empty database and generate code against an empty schema.
//
// Takes family (string) which is the database's dialect family.
// Takes dsn (string) which is the anchored connection string of the schema source.
//
// Returns error when the connection string names a SQLite file that is not there.
func (inv *invocation) requireSchemaFile(family, dsn string) error {
	name, isFile := sqliteFile(family, dsn)
	if !isFile {
		return nil
	}
	if _, err := os.Stat(name); errors.Is(err, fs.ErrNotExist) {
		return usageError("schema source %s does not exist; %s reads a schema from an existing database", name, inv.program)
	}
	return nil
}

// anchorDSN resolves relative SQLite database paths against a directory.
//
// Absolute paths, in-memory databases, remote URLs and other dialects' connection strings
// are unchanged.
//
// Takes family (string) which is the database's dialect family.
// Takes dsn (string) which is the connection string.
// Takes directory (string) which is the absolute directory a relative file is in.
//
// Returns string which is the connection string to open.
func anchorDSN(family, dsn, directory string) string {
	switch {
	case family != dialect_engine_sqlite.Family, dsn == "", urlScheme.MatchString(dsn):
		return dsn
	case strings.HasPrefix(dsn, fileURIScheme):
		return anchorURI(dsn, directory)
	default:
		return anchorFileName(dsn, directory)
	}
}

// anchorFileName anchors a plain SQLite file name, whose driver parameters follow the
// first "?".
//
// Takes dsn (string) which is the file name and any parameters.
// Takes directory (string) which is the absolute directory a relative file is in.
//
// Returns string which is the anchored file name and its parameters.
func anchorFileName(dsn, directory string) string {
	name, parameters, hasParameters := strings.Cut(dsn, parameterSeparator)
	if !namesARelativeFile(name, parameters) {
		return dsn
	}
	anchored := filepath.Join(directory, name)
	if hasParameters {
		anchored += parameterSeparator + parameters
	}
	return anchored
}

// anchorURI anchors a SQLite file: URI whose path is relative. A URI with an authority
// (file://) or an absolute path already names its file.
//
// Takes dsn (string) which is the URI.
// Takes directory (string) which is the absolute directory a relative file is in.
//
// Returns string which is the anchored URI, with its query and fragment as written.
func anchorURI(dsn, directory string) string {
	rest := strings.TrimPrefix(dsn, fileURIScheme)
	end := strings.IndexAny(rest, "?#")
	if end < 0 {
		end = len(rest)
	}
	name, suffix := rest[:end], rest[end:]
	query, _, _ := strings.Cut(strings.TrimPrefix(suffix, parameterSeparator), "#")
	if strings.HasPrefix(suffix, "#") {
		query = ""
	}
	if strings.HasPrefix(name, "/") || !namesARelativeFile(name, query) {
		return dsn
	}
	base := filepath.ToSlash(directory)
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return fileURIScheme + path.Join(uriPathEscaper.Replace(base), name) + suffix
}

// namesARelativeFile reports whether a SQLite file name and its parameters name a file on
// disk by a relative path, rather than an absolute one, a temporary database or an
// in-memory one.
//
// Takes name (string) which is the file name.
// Takes parameters (string) which is the query after "?", without it.
//
// Returns bool which is true when the name needs anchoring.
func namesARelativeFile(name, parameters string) bool {
	if name == "" || name == memoryDatabase || filepath.IsAbs(filepath.FromSlash(name)) {
		return false
	}
	return !inMemory(parameters)
}

// sqliteFile returns the file on disk an anchored SQLite connection string opens, and
// whether it names one at all: in-memory and temporary databases, URIs with a host, URLs
// and other dialect families name none.
//
// Takes family (string) which is the database's dialect family.
// Takes dsn (string) which is an anchored connection string.
//
// Returns string which is the file's path.
// Returns bool which is true when the connection string names a file.
func sqliteFile(family, dsn string) (string, bool) {
	if family != dialect_engine_sqlite.Family || dsn == "" {
		return "", false
	}
	if !strings.HasPrefix(dsn, fileURIScheme) {
		if urlScheme.MatchString(dsn) {
			return "", false
		}
		name, parameters, _ := strings.Cut(dsn, parameterSeparator)
		return name, name != "" && name != memoryDatabase && !inMemory(parameters)
	}
	rest := strings.TrimPrefix(dsn, fileURIScheme)
	end := strings.IndexAny(rest, "?#")
	if end < 0 {
		end = len(rest)
	}
	name, suffix := rest[:end], rest[end:]
	query, _, _ := strings.Cut(strings.TrimPrefix(suffix, parameterSeparator), "#")
	if afterAuthority, hasAuthority := strings.CutPrefix(name, "//"); hasAuthority {
		if !strings.HasPrefix(afterAuthority, "/") {
			return "", false
		}
		name = afterAuthority
	}
	unescaped, err := url.PathUnescape(name)
	if err != nil || unescaped == "" || unescaped == memoryDatabase || inMemory(query) {
		return "", false
	}
	return filepath.FromSlash(unescaped), true
}

// inMemory reports whether SQLite connection parameters ask for an in-memory database.
//
// Takes parameters (string) which is the query after "?", without it.
//
// Returns bool which is true for mode=memory or vfs=memdb.
func inMemory(parameters string) bool {
	for parameter := range strings.SplitSeq(parameters, "&") {
		for _, memory := range inMemoryParameters {
			if strings.EqualFold(parameter, memory) {
				return true
			}
		}
	}
	return false
}
