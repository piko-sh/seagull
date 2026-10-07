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
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"piko.sh/safedisk"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/sdk/dialect"
)

const (
	// migrationsHazard explains why fmt leaves migrations alone; %s is the program name.
	migrationsHazard = "%s records the checksum of every applied migration, so rewriting one makes the next migrate up " +
		"fail with a checksum mismatch; pass --include-migrations only for migrations that have not been applied anywhere"

	// maxFormattedFiles is the most SQL files one fmt run formats.
	maxFormattedFiles = 10_000
)

// fmtFlags holds the fmt command's flags.
type fmtFlags struct {
	// rewritePrefix, when set, is the prefix every directive is rewritten to.
	rewritePrefix string

	// list prints the files whose formatting differs.
	list bool

	// write rewrites the files in place.
	write bool

	// check lists the files whose formatting differs and exits 1 when there are any.
	check bool

	// includeMigrations formats the files of the configured migrations directories too.
	includeMigrations bool
}

// fmtDirectory is a configured directory of SQL files and the dialect its files are
// written in.
type fmtDirectory struct {
	// engine is the dialect of the database that configures the directory.
	engine dialect.Dialect

	// path is the directory as configured, absolute.
	path string

	// resolved is the directory with its symbolic links resolved, for comparisons.
	resolved string

	// migrations reports that the directory holds a database's migrations.
	migrations bool
}

// fmtFile is one file to format and the dialect to read it with.
type fmtFile struct {
	// engine is the dialect the file is written in, or nil when no configured directory
	// holds the file, which is then read with neutral rules.
	engine dialect.Dialect

	// path is the file.
	path string
}

// fmtPlan is what one fmt run formats and how.
type fmtPlan struct {
	// canonical is the prefix directives are rewritten to.
	canonical string

	// aliases are the prefixes rewritten to it.
	aliases []string

	// directories holds the configured directories, most specific first.
	directories []fmtDirectory
}

// owner finds the configured directory that holds a path most closely.
//
// Takes path (string) which is an absolute file or directory.
//
// Returns fmtDirectory which is the directory.
// Returns bool which is false when no configured directory holds the path.
func (plan *fmtPlan) owner(path string) (fmtDirectory, bool) {
	resolved := resolvedPath(path)
	for _, directory := range plan.directories {
		if within(resolved, directory.resolved) {
			return directory, true
		}
	}
	return fmtDirectory{engine: dialect.Dialect{}, path: "", resolved: "", migrations: false}, false
}

// isSkippedMigrations reports whether a directory is a migrations directory fmt leaves
// out.
//
// Takes path (string) which is a directory found while walking.
// Takes includeMigrations (bool) which is --include-migrations.
//
// Returns bool which is true when the directory is a configured migrations directory and
// migrations are not included.
func (plan *fmtPlan) isSkippedMigrations(path string, includeMigrations bool) bool {
	if includeMigrations {
		return false
	}
	resolved := resolvedPath(path)
	return slices.ContainsFunc(plan.directories, func(directory fmtDirectory) bool {
		return directory.migrations && directory.resolved == resolved
	})
}

// sqlFileWalker collects the .sql files beneath fmt's roots.
type sqlFileWalker struct {
	// inv reports a migrations directory left out.
	inv *invocation

	// plan holds the configured directories.
	plan *fmtPlan

	// paths holds the files found.
	paths []string

	// includeMigrations is --include-migrations.
	includeMigrations bool

	// explicit reports that the roots were named on the command line.
	explicit bool
}

// walk collects the .sql files beneath one root, or the root itself when it is a file.
//
// Takes root (string) which is a file or directory.
//
// Returns error when the root cannot be read, too many files are found, or the run is
// cancelled.
func (walker *sqlFileWalker) walk(ctx context.Context, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		switch {
		case walkErr != nil:
			return walkErr
		case ctx.Err() != nil:
			return context.Cause(ctx)
		case entry.IsDir():
			return walker.enter(root, path)
		case path != root && (!strings.HasSuffix(path, ".sql") || !isFileOrLink(entry)):
			return nil
		case len(walker.paths) >= maxFormattedFiles:
			return usageError("there are more than %d SQL files to format; name fewer paths", maxFormattedFiles)
		default:
			walker.paths = append(walker.paths, path)
			return nil
		}
	})
}

// enter decides whether to walk into a directory: a migrations directory below the root
// is left out unless migrations are included, with a note when the root was named.
//
// Takes root (string) which is the root being walked.
// Takes path (string) which is the directory.
//
// Returns error which is filepath.SkipDir to leave the directory out, or nil.
func (walker *sqlFileWalker) enter(root, path string) error {
	if path == root || !walker.plan.isSkippedMigrations(path, walker.includeMigrations) {
		return nil
	}
	if walker.explicit {
		fmt.Fprintf(walker.inv.streams.Stderr, "%s: leaving out the migrations directory %s; pass --include-migrations to format it\n",
			walker.inv.program, walker.inv.display(path))
	}
	return filepath.SkipDir
}

// runFmt canonicalises the directive comments of SQL files. Without paths it formats the
// configured query and seed directories, and the migrations directories only with
// --include-migrations; a path inside a migrations directory needs the flag too.
//
// With -w and -l or --check, each file that changes is listed and then rewritten. A
// cancelled run stops before the next file.
//
// Takes arguments ([]string) which holds the flags and optional paths.
//
// Returns int which is the exit status.
func (inv *invocation) runFmt(ctx context.Context, arguments []string) int {
	flags := inv.newFlags("fmt", "fmt [-l|-w|--check] [--rewrite-prefix p] [--include-migrations] [paths]")
	options := fmtFlags{rewritePrefix: "", list: false, write: false, check: false, includeMigrations: false}
	flags.BoolVar(&options.list, "l", false, "list files whose directives are not canonical")
	flags.BoolVar(&options.write, "w", false, "rewrite files in place")
	flags.BoolVar(&options.check, "check", false, "list files that need formatting and exit 1 when there are any")
	flags.StringVar(&options.rewritePrefix, "rewrite-prefix", "", "rewrite every directive to prefix `p`")
	flags.BoolVar(&options.includeMigrations, "include-migrations", false, "format migration files too, which changes the checksum of an applied one")
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status
	}

	plan, roots, err := inv.fmtTargets(positional, options)
	if err != nil {
		return inv.fail(err)
	}
	files, err := inv.sqlFiles(ctx, plan, roots, options.includeMigrations, len(positional) > 0)
	if err != nil {
		return inv.fail(err)
	}

	unformatted := 0
	for _, file := range files {
		if ctx.Err() != nil {
			return inv.fail(context.Cause(ctx))
		}
		changed, err := inv.formatFile(file, plan, options)
		if err != nil {
			return inv.fail(err)
		}
		if changed {
			unformatted++
		}
	}
	if options.check && unformatted > 0 {
		return exitFindings
	}
	return exitOK
}

// fmtTargets resolves the prefixes, the configured directories and the paths fmt works
// on.
//
// Takes paths ([]string) which are the paths given, or none for the configured
// directories.
// Takes options (fmtFlags) which hold --rewrite-prefix and --include-migrations.
//
// Returns *fmtPlan which holds the prefixes and the configured directories.
// Returns []string which are the files and directories to format.
// Returns error when the configuration is needed but cannot be loaded, a dialect cannot
// be built, or a path is inside a migrations directory without --include-migrations.
func (inv *invocation) fmtTargets(paths []string, options fmtFlags) (*fmtPlan, []string, error) {
	prefixes := config.DefaultPrefixes()
	plan := &fmtPlan{canonical: "", aliases: nil, directories: nil}
	loaded, loadErr := inv.loadProject(inv.database)
	switch {
	case loadErr == nil:
		prefixes = loaded.config.Directives.Prefixes
		directories, err := inv.fmtDirectories(loaded)
		if err != nil {
			return nil, nil, err
		}
		plan.directories = directories
	case len(paths) == 0 || !errors.Is(loadErr, config.ErrNotFound):
		return nil, nil, loadErr
	}
	roots, err := inv.fmtRoots(plan, paths, options.includeMigrations)
	if err != nil {
		return nil, nil, err
	}
	plan.canonical, plan.aliases = canonicalPrefix(prefixes, options.rewritePrefix)
	return plan, roots, nil
}

// fmtRoots chooses the files and directories fmt walks: the paths given, or without them
// the configured directories, leaving out migrations unless they are included.
//
// Takes plan (*fmtPlan) which holds the configured directories.
// Takes paths ([]string) which are the paths given.
// Takes includeMigrations (bool) which is --include-migrations.
//
// Returns []string which holds the absolute roots.
// Returns error which is a usage error for a path inside a migrations directory when
// migrations are not included.
func (inv *invocation) fmtRoots(plan *fmtPlan, paths []string, includeMigrations bool) ([]string, error) {
	var roots []string
	if len(paths) == 0 {
		for _, directory := range plan.directories {
			if !directory.migrations || includeMigrations {
				roots = append(roots, directory.path)
			}
		}
		return roots, nil
	}
	for _, path := range paths {
		absolute := inv.path(path)
		if owner, found := plan.owner(absolute); found && owner.migrations && !includeMigrations {
			return nil, usageError("%s is in the migrations directory %s, which %s fmt leaves alone: %s",
				inv.display(absolute), inv.display(owner.path), inv.program, fmt.Sprintf(migrationsHazard, inv.program))
		}
		roots = append(roots, absolute)
	}
	return roots, nil
}

// fmtDirectories lists the migration, query and seed directories of the selected
// databases that exist, with each database's dialect, most specific first.
//
// Takes loaded (*project) which holds the databases.
//
// Returns []fmtDirectory which holds each directory once, for the first database that
// configures it.
// Returns error when a database's dialect cannot be built.
func (inv *invocation) fmtDirectories(loaded *project) ([]fmtDirectory, error) {
	var directories []fmtDirectory
	for _, database := range loaded.databases {
		engine, err := inv.buildDialect(database)
		if err != nil {
			return nil, err
		}
		directories = appendDirectory(directories, engine, database.Migrations, true)
		directories = appendDirectory(directories, engine, database.Queries, false)
		directories = appendDirectory(directories, engine, database.Seeds, false)
	}
	slices.SortStableFunc(directories, func(left, right fmtDirectory) int { return cmp.Compare(len(right.resolved), len(left.resolved)) })
	return directories, nil
}

// sqlFiles expands files and directories into the .sql files beneath them, leaving out
// the migrations directories unless they are included, and any entry that is neither a
// regular file nor a symbolic link, such as a named pipe.
//
// Takes plan (*fmtPlan) which holds the configured directories.
// Takes roots ([]string) which are files and directories.
// Takes includeMigrations (bool) which is --include-migrations.
// Takes explicit (bool) which reports that the roots were named on the command line, so a
// migrations directory left out of one is reported.
//
// Returns []fmtFile which holds each .sql file once, sorted, with its dialect.
// Returns error when a root cannot be read, the roots hold more than maxFormattedFiles
// files, or the run is cancelled.
func (inv *invocation) sqlFiles(ctx context.Context, plan *fmtPlan, roots []string, includeMigrations, explicit bool) ([]fmtFile, error) {
	walker := &sqlFileWalker{inv: inv, plan: plan, paths: nil, includeMigrations: includeMigrations, explicit: explicit}
	for _, root := range roots {
		if err := walker.walk(ctx, root); err != nil {
			return nil, err
		}
	}
	slices.Sort(walker.paths)
	paths := slices.Compact(walker.paths)
	files := make([]fmtFile, 0, len(paths))
	for _, path := range paths {
		owner, _ := plan.owner(path)
		files = append(files, fmtFile{engine: owner.engine, path: path})
	}
	return files, nil
}

// formatFile formats one file according to the mode, under its database's dialect, or
// neutral rules when no configured directory holds it.
//
// The file is read through a sandbox rooted at its directory, after resolving symbolic
// links, and must be a regular file of at most maxSourceFileBytes. A file that changes is
// listed under -l or --check and then rewritten under -w.
//
// Takes file (fmtFile) which is the file and its dialect.
// Takes plan (*fmtPlan) which holds the prefixes.
// Takes options (fmtFlags) which select the mode.
//
// Returns bool which is true when the file's formatting differs.
// Returns error when the file cannot be read, formatted or written.
func (inv *invocation) formatFile(file fmtFile, plan *fmtPlan, options fmtFlags) (bool, error) {
	path, err := filepath.EvalSymlinks(file.path)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", file.path, err)
	}
	mode := safedisk.ModeReadOnly
	if options.write {
		mode = safedisk.ModeReadWrite
	}
	sandbox, err := safedisk.NewSandbox(filepath.Dir(path), mode)
	if err != nil {
		return false, fmt.Errorf("opening %s: %w", filepath.Dir(path), err)
	}
	changed, err := inv.formatIn(sandbox, file, path, plan, options)
	return changed, errors.Join(err, sandbox.Close())
}

// formatIn formats one file held by a sandbox rooted at its directory.
//
// Takes sandbox (safedisk.Sandbox) which holds the file.
// Takes file (fmtFile) which is the file as found, and its dialect.
// Takes path (string) which is the file with its symbolic links resolved.
// Takes plan (*fmtPlan) which holds the prefixes.
// Takes options (fmtFlags) which select the mode.
//
// Returns bool which is true when the file's formatting differs.
// Returns error when the file cannot be read, formatted or written.
func (inv *invocation) formatIn(sandbox safedisk.Sandbox, file fmtFile, path string, plan *fmtPlan, options fmtFlags) (bool, error) {
	name := filepath.Base(path)
	source, err := readFileIn(sandbox, name, file.path, maxSourceFileBytes)
	if err != nil {
		return false, err
	}
	var formatted []byte
	if file.engine != (dialect.Dialect{}) {
		formatted, err = seagull.FormatDirectivesFor(file.engine, source, plan.canonical, plan.aliases...)
	} else {
		formatted, err = seagull.FormatDirectives(source, plan.canonical, plan.aliases...)
	}
	if err != nil {
		return false, usageError("%w", err)
	}
	changed := !bytes.Equal(source, formatted)
	if changed && (options.list || options.check) {
		fmt.Fprintln(inv.streams.Stdout, inv.display(file.path))
	}
	switch {
	case options.write && changed:
		return true, rewritePreservingMode(sandbox, name, path, formatted)
	case options.write, options.list, options.check:
		return changed, nil
	default:
		_, _ = inv.streams.Stdout.Write(formatted)
		return changed, nil
	}
}

// canonicalPrefix chooses the prefix directives are rewritten to and the aliases
// rewritten to it.
//
// Takes prefixes ([]string) which are the accepted prefixes, canonical first.
// Takes rewritePrefix (string) which is --rewrite-prefix, or empty.
//
// Returns string which is the canonical prefix.
// Returns []string which holds every other accepted prefix.
func canonicalPrefix(prefixes []string, rewritePrefix string) (string, []string) {
	canonical := prefixes[0]
	if rewritePrefix != "" {
		canonical = rewritePrefix
	}
	var aliases []string
	for _, prefix := range prefixes {
		if prefix != canonical {
			aliases = append(aliases, prefix)
		}
	}
	return canonical, aliases
}

// appendDirectory adds a configured directory that exists and is not listed yet.
//
// Takes directories ([]fmtDirectory) which holds the directories listed so far.
// Takes engine (dialect.Dialect) which is the dialect of the database that configures it.
// Takes path (string) which is the directory, or empty when it is not configured.
// Takes migrations (bool) which reports that it holds migrations.
//
// Returns []fmtDirectory which holds the directories with it added.
func appendDirectory(directories []fmtDirectory, engine dialect.Dialect, path string, migrations bool) []fmtDirectory {
	if path == "" {
		return directories
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return directories
	}
	resolved := resolvedPath(path)
	if slices.ContainsFunc(directories, func(directory fmtDirectory) bool { return directory.resolved == resolved }) {
		return directories
	}
	return append(directories, fmtDirectory{engine: engine, path: path, resolved: resolved, migrations: migrations})
}

// resolvedPath returns a path cleaned and with its symbolic links resolved where they can
// be, so two spellings of one directory compare equal.
//
// Takes path (string) which is an absolute path.
//
// Returns string which is the resolved path, or the cleaned path when it cannot be
// resolved.
func resolvedPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// within reports whether a path is a directory or inside it.
//
// Takes path (string) which is the path.
// Takes directory (string) which is the directory.
//
// Returns bool which is true when path is directory or below it.
func within(path, directory string) bool {
	relative, err := filepath.Rel(directory, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// rewritePreservingMode rewrites a file atomically, keeping its permissions.
//
// Takes sandbox (safedisk.Sandbox) which holds the file and may be written.
// Takes name (string) which is the file's name within the sandbox.
// Takes path (string) which names the file in an error.
// Takes content ([]byte) which is the new content.
//
// Returns error when the file cannot be written.
func rewritePreservingMode(sandbox safedisk.Sandbox, name, path string, content []byte) error {
	info, err := sandbox.Stat(name)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := sandbox.WriteFileAtomic(name, content, info.Mode().Perm()); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// isFileOrLink reports whether a directory entry is a regular file or a symbolic link,
// rather than a named pipe, a device or a socket.
//
// Takes entry (fs.DirEntry) which is the entry.
//
// Returns bool which is true for a regular file or a symbolic link.
func isFileOrLink(entry fs.DirEntry) bool {
	return entry.Type().IsRegular() || entry.Type()&fs.ModeSymlink != 0
}
