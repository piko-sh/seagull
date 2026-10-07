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

package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	// jsonTagKey is the struct tag key go.json_tags shapes.
	jsonTagKey = "json"

	// maxReportedProblems is how many problems one error lists; the rest are counted.
	maxReportedProblems = 50

	// maxNamedDatabases is how many databases one problem names; the rest are counted.
	maxNamedDatabases = 8

	// problemIndent starts each problem on its own line under the error's first line.
	problemIndent = "\n  "
)

var (
	// prefixPattern is the shape of a directive prefix.
	prefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

	// identifierPattern is the shape of a namespace or history table name.
	identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

	// goPackagePattern is the shape of a Go package name.
	goPackagePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	// driverPattern is the shape of a go.driver name. The stock names are database/sql, pgx
	// and clickhouse; a custom sgl binary can register more, so the CLI checks the name
	// against its emitters.
	driverPattern = regexp.MustCompile(`^[a-z][a-z0-9_./-]{0,63}$`)

	// locks lists the accepted migrate.lock values.
	locks = []string{"", "advisory", "table", "none"}

	// nullabilities lists the accepted custom function nullable values.
	nullabilities = []string{"", "null_on_null", "never_null", "called_on_null"}

	// dataAccesses lists the accepted custom function data_access values.
	dataAccesses = []string{"", "read_only", "modifies_data"}

	// jsonCases lists the accepted go.json_tags.case values.
	jsonCases = []string{"", "snake", "camel", "pascal"}
)

// problemList collects the problems a check finds, keeping the first maxReportedProblems
// and counting the rest, so a configuration with thousands of mistakes still yields an
// error of bounded size.
type problemList struct {
	// kept holds the problems that are reported, in the order they were found.
	kept []string

	// omitted counts the problems found after the list was full.
	omitted int
}

// newProblemList creates an empty problem list.
//
// Returns *problemList which holds no problems.
func newProblemList() *problemList {
	return &problemList{kept: nil, omitted: 0}
}

// add records problems, keeping them while the list has room and counting them after.
//
// Takes problems (...string) which are recorded in order.
func (list *problemList) add(problems ...string) {
	room := max(maxReportedProblems-len(list.kept), 0)
	kept := min(room, len(problems))
	list.kept = append(list.kept, problems[:kept]...)
	list.omitted += len(problems) - kept
}

// empty reports whether no problem was recorded.
//
// Returns bool which is true when the list holds nothing and counted nothing.
func (list *problemList) empty() bool {
	return len(list.kept) == 0 && list.omitted == 0
}

// text lists the kept problems one per line, then how many more were found.
//
// Returns string which holds the problems separated by problemIndent.
func (list *problemList) text() string {
	lines := list.kept
	if list.omitted > 0 {
		lines = append(slices.Clip(lines), fmt.Sprintf("and %d more %s", list.omitted, plural(list.omitted, "problem", "problems")))
	}
	return strings.Join(lines, problemIndent)
}

// outputDirectory is one go.out directory and the databases that write to it.
type outputDirectory struct {
	// path is the directory, cleaned.
	path string

	// components splits path into its root and its elements, so a directory sorts directly
	// before every directory inside it.
	components []string

	// databases names the databases that write to the directory, in configuration order.
	databases []string
}

// Validate checks the configuration for mistakes that decoding cannot catch.
//
// Returns error which wraps ErrInvalid and lists the problems found, the first
// maxReportedProblems of them followed by a count of the rest.
func (c *Config) Validate() error {
	return c.validate("")
}

// validate checks the configuration for mistakes that decoding cannot catch. A
// configuration with more than MaxDatabases databases is refused before anything else is
// checked.
//
// Takes directory (string) which is the directory the configuration's relative paths were
// resolved against, so a problem can name a go.out directory as the file does, or is
// empty when they were not resolved.
//
// Returns error which wraps ErrInvalid and lists the problems found.
func (c *Config) validate(directory string) error {
	if len(c.Databases) > MaxDatabases {
		return fmt.Errorf("%w: databases: %d databases are more than the %d one configuration may declare", ErrInvalid, len(c.Databases), MaxDatabases)
	}
	problems := newProblemList()
	if c.Version != CurrentVersion {
		problems.add(fmt.Sprintf("version %d is not supported (the supported version is %d)", c.Version, CurrentVersion))
	}
	problems.add(validatePrefixes(c.Directives.Prefixes)...)
	if len(c.Databases) == 0 {
		problems.add("databases: at least one database is required")
	}
	seen := make(map[string]bool, len(c.Databases))
	for index := range c.Databases {
		database := &c.Databases[index]
		label := databaseLabel(index, database)
		if seen[database.Name] {
			problems.add(fmt.Sprintf("%s: the name is used more than once", label))
		}
		seen[database.Name] = true
		for _, problem := range validateDatabase(database) {
			problems.add(label + ": " + problem)
		}
	}
	problems.add(validateOutputDirectories(c.Databases, directory)...)
	if problems.empty() {
		return nil
	}
	return fmt.Errorf("%w:%s%s", ErrInvalid, problemIndent, problems.text())
}

// validatePrefixes checks the directive prefix list.
//
// Takes prefixes ([]string) which is the configured list.
//
// Returns []string which describes each problem.
func validatePrefixes(prefixes []string) []string {
	var problems []string
	seen := make(map[string]bool, len(prefixes))
	for _, prefix := range prefixes {
		if !prefixPattern.MatchString(prefix) {
			problems = append(problems, fmt.Sprintf("directives.prefixes: %q must be lower case letters, digits and underscores, starting with a letter", prefix))
		}
		if seen[prefix] {
			problems = append(problems, fmt.Sprintf("directives.prefixes: %q is listed twice", prefix))
		}
		seen[prefix] = true
	}
	return problems
}

// validateDatabase checks one database entry.
//
// Takes database (*Database) which is checked.
//
// Returns []string which describes each problem.
func validateDatabase(database *Database) []string {
	var problems []string
	if database.Name == "" {
		problems = append(problems, "name is required")
	}
	if database.Dialect == "" {
		problems = append(problems, "dialect is required")
	}
	if !goPackagePattern.MatchString(database.Go.Package) {
		problems = append(problems, fmt.Sprintf("go.package %q is not a valid package name", database.Go.Package))
	}
	if !driverPattern.MatchString(database.Go.Driver) {
		problems = append(problems, fmt.Sprintf("go.driver %q is not a driver name such as database/sql, pgx or clickhouse", database.Go.Driver))
	}
	if !slices.Contains(locks, database.Migrate.Lock) {
		problems = append(problems, fmt.Sprintf("migrate.lock %q must be advisory, table or none", database.Migrate.Lock))
	}
	if !identifierPattern.MatchString(database.Migrate.Namespace) {
		problems = append(problems, fmt.Sprintf("migrate.namespace %q is not a valid SQL identifier", database.Migrate.Namespace))
	}
	if database.Migrate.HistoryTable != "" && !identifierPattern.MatchString(database.Migrate.HistoryTable) {
		problems = append(problems, fmt.Sprintf("migrate.history_table %q is not a valid SQL identifier", database.Migrate.HistoryTable))
	}
	problems = append(problems, validateGeneratedFields(&database.Go)...)
	for index, override := range database.Go.Types.Overrides {
		problems = append(problems, validateOverride(index, override)...)
	}
	for index, function := range database.CustomFunctions {
		problems = append(problems, validateCustomFunction(index, function)...)
	}
	return problems
}

// validateOutputDirectories checks that no two databases write generated code to the same
// go.out directory, or to one inside another. Generation prunes the files it no longer
// produces, so a shared directory would lose one database's code to the other's.
//
// It reports one problem per shared directory, naming every database that writes to it,
// and one per directory inside another, naming the nearest enclosing one. Sorting the
// directories by their elements puts each one directly before those inside it, so the
// check takes O(n log n) comparisons rather than one per pair.
//
// Takes databases ([]Database) which are checked.
// Takes directory (string) which the messages name go.out directories relative to, or is
// empty.
//
// Returns []string which describes each overlap.
func validateOutputDirectories(databases []Database, directory string) []string {
	byPath := make(map[string]*outputDirectory, len(databases))
	ordered := make([]*outputDirectory, 0, len(databases))
	for index := range databases {
		path := filepath.Clean(databases[index].Go.Out)
		entry, found := byPath[path]
		if !found {
			entry = &outputDirectory{path: path, components: pathComponents(path), databases: nil}
			byPath[path] = entry
			ordered = append(ordered, entry)
		}
		entry.databases = append(entry.databases, databases[index].Name)
	}
	var problems []string
	for _, entry := range ordered {
		if len(entry.databases) > 1 {
			problems = append(problems, sharedOutputProblem(entry, directory))
		}
	}
	return append(problems, nestedOutputProblems(ordered, directory)...)
}

// sharedOutputProblem describes a go.out directory several databases write to.
//
// Takes entry (*outputDirectory) which is the directory and its databases.
// Takes directory (string) which the message names the directory relative to, or is
// empty.
//
// Returns string which names the databases and the directory.
func sharedOutputProblem(entry *outputDirectory, directory string) string {
	verb := "all write"
	if len(entry.databases) == 2 {
		verb = "both write"
	}
	return fmt.Sprintf("databases %s %s generated code to go.out %q; give each database its own go.out",
		quotedNames(entry.databases), verb, displayPath(entry.path, directory))
}

// nestedOutputProblems describes each go.out directory that lies inside another.
//
// Takes entries ([]*outputDirectory) which are the distinct directories, in configuration
// order; they are not changed.
// Takes directory (string) which the messages name directories relative to, or is empty.
//
// Returns []string which holds one problem per nested directory, naming the nearest
// directory that holds it.
func nestedOutputProblems(entries []*outputDirectory, directory string) []string {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(left, right *outputDirectory) int { return slices.Compare(left.components, right.components) })
	var problems []string
	var enclosing []*outputDirectory
	for _, entry := range sorted {
		for len(enclosing) > 0 && !isBelow(entry.components, enclosing[len(enclosing)-1].components) {
			enclosing = enclosing[:len(enclosing)-1]
		}
		if len(enclosing) > 0 {
			outer := enclosing[len(enclosing)-1]
			problems = append(problems, fmt.Sprintf("database %q go.out %q is inside database %q go.out %q; generated-code directories must not overlap",
				entry.databases[0], displayPath(entry.path, directory), outer.databases[0], displayPath(outer.path, directory)))
		}
		enclosing = append(enclosing, entry)
	}
	return problems
}

// displayPath names a cleaned path for a message: relative to directory when it lies
// inside it, as the configuration file would spell it, and otherwise as it is.
//
// Takes path (string) which is the cleaned path.
// Takes directory (string) which is the configuration file's directory, or empty.
//
// Returns string which is the path to print, with forward slashes.
func displayPath(path, directory string) string {
	if directory != "" && (path == filepath.Clean(directory) || isInside(path, filepath.Clean(directory))) {
		if relative, err := filepath.Rel(directory, path); err == nil {
			return filepath.ToSlash(relative)
		}
	}
	return filepath.ToSlash(path)
}

// isInside reports whether a cleaned path lies below another cleaned path.
//
// Takes inner (string) which is the candidate subdirectory.
// Takes outer (string) which is the candidate parent.
//
// Returns bool which is true when inner is strictly below outer.
func isInside(inner, outer string) bool {
	relative, err := filepath.Rel(outer, inner)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// validateGeneratedFields checks the settings that shape the generated fields and the
// interface: the json tags' case, the struct tags, which leave json to go.json_tags, and
// the interface's name.
//
// Takes settings (*Go) which holds them.
//
// Returns []string which describes each problem.
func validateGeneratedFields(settings *Go) []string {
	var problems []string
	if !slices.Contains(jsonCases, settings.JSONTags.Case) {
		problems = append(problems, fmt.Sprintf("go.json_tags.case %q must be snake, camel or pascal", settings.JSONTags.Case))
	}
	if _, found := settings.StructTags[jsonTagKey]; found {
		problems = append(problems, "go.struct_tags cannot give json; set go.json_tags instead")
	}
	if settings.Interface.Enabled && !identifierPattern.MatchString(settings.Interface.Name) {
		problems = append(problems, fmt.Sprintf("go.interface.name %q is not a Go identifier", settings.Interface.Name))
	}
	return problems
}

// validateOverride checks one type override.
//
// Takes index (int) which locates the override.
// Takes override (Override) which is checked.
//
// Returns []string which describes each problem.
func validateOverride(index int, override Override) []string {
	var problems []string
	if (override.SQL == "") == (override.Column == "") {
		problems = append(problems, fmt.Sprintf("go.types.overrides[%d]: set exactly one of sql and column", index))
	}
	if override.Column != "" && !strings.Contains(override.Column, ".") {
		problems = append(problems, fmt.Sprintf("go.types.overrides[%d]: column %q must be table.column", index, override.Column))
	}
	if override.Go == "" && override.NonNullElements == nil && len(override.StructTags) == 0 {
		problems = append(problems, fmt.Sprintf("go.types.overrides[%d]: go, non_null_elements or struct_tags is required", index))
	}
	if override.SQL != "" && len(override.StructTags) > 0 {
		problems = append(problems, fmt.Sprintf("go.types.overrides[%d]: struct_tags apply to a column override, not to sql %q", index, override.SQL))
	}
	return problems
}

// validateCustomFunction checks one custom function declaration.
//
// Takes index (int) which locates the declaration.
// Takes function (CustomFunction) which is checked.
//
// Returns []string which describes each problem.
func validateCustomFunction(index int, function CustomFunction) []string {
	var problems []string
	if function.Name == "" {
		problems = append(problems, fmt.Sprintf("custom_functions[%d]: name is required", index))
	}
	if function.Returns == "" {
		problems = append(problems, fmt.Sprintf("custom_functions[%d]: returns is required", index))
	}
	if !slices.Contains(nullabilities, function.Nullable) {
		problems = append(problems, fmt.Sprintf("custom_functions[%d]: nullable %q must be null_on_null, never_null or called_on_null", index, function.Nullable))
	}
	if !slices.Contains(dataAccesses, function.DataAccess) {
		problems = append(problems, fmt.Sprintf("custom_functions[%d]: data_access %q must be read_only or modifies_data", index, function.DataAccess))
	}
	if function.MinArgs > len(function.Args) {
		problems = append(problems, fmt.Sprintf("custom_functions[%d]: min_args is larger than the number of args", index))
	}
	return problems
}

// databaseLabel names a database in a problem: by its name, or by its position when it
// has none.
//
// Takes index (int) which is the database's position in the configuration.
// Takes database (*Database) which is named.
//
// Returns string which reads like database "app" or databases[2].
func databaseLabel(index int, database *Database) string {
	if database.Name != "" {
		return fmt.Sprintf("database %q", database.Name)
	}
	return fmt.Sprintf("databases[%d]", index)
}

// quotedNames lists names for a message, quoted, naming at most maxNamedDatabases and
// counting the rest.
//
// Takes names ([]string) which holds at least two names.
//
// Returns string which reads like "a", "b" and "c", or "a", "b" and 7 more.
func quotedNames(names []string) string {
	shown := min(len(names), maxNamedDatabases)
	quoted := make([]string, 0, shown)
	for _, name := range names[:shown] {
		quoted = append(quoted, strconv.Quote(name))
	}
	if rest := len(names) - shown; rest > 0 {
		return strings.Join(quoted, ", ") + fmt.Sprintf(" and %d more", rest)
	}
	return strings.Join(quoted[:shown-1], ", ") + " and " + quoted[shown-1]
}

// pathComponents splits a cleaned path into its root and its elements. The root is the
// volume and separator of an absolute path, or for a relative path the run of ".."
// elements it starts with, so two paths have the same root exactly when one can lie
// inside the other.
//
// Takes path (string) which is a cleaned path.
//
// Returns []string which holds the root, then each element.
func pathComponents(path string) []string {
	separator := string(filepath.Separator)
	volume := filepath.VolumeName(path)
	rest := path[len(volume):]
	root := volume
	if afterSeparator, absolute := strings.CutPrefix(rest, separator); absolute {
		root += separator
		rest = afterSeparator
	}
	elements := strings.Split(rest, separator)
	parents := 0
	for parents < len(elements) && elements[parents] == ".." {
		parents++
	}
	components := []string{root + strings.Repeat(".."+separator, parents)}
	for _, element := range elements[parents:] {
		if element != "" && element != "." {
			components = append(components, element)
		}
	}
	return components
}

// isBelow reports whether a path's components continue another's, so the path lies inside
// the other.
//
// Takes inner ([]string) which are the candidate subdirectory's components.
// Takes outer ([]string) which are the candidate parent's components.
//
// Returns bool which is true when inner is strictly below outer.
func isBelow(inner, outer []string) bool {
	return len(inner) > len(outer) && slices.Equal(inner[:len(outer)], outer)
}

// plural chooses the form of a word for a count.
//
// Takes count (int) which is the count.
// Takes one (string) which is the form for a count of one.
// Takes many (string) which is the form for any other count.
//
// Returns string which is the form to use.
func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}
