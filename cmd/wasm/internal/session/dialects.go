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
	"errors"
	"fmt"
	"slices"

	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

const (
	// optionGroupByUseNulls is the sgl.yaml key of ClickHouse's group_by_use_nulls setting.
	optionGroupByUseNulls = "group_by_use_nulls"

	// optionANSIQuotes is the sgl.yaml key of the ANSI_QUOTES sql_mode of MySQL and MariaDB.
	optionANSIQuotes = "ansi_quotes"

	// optionSearchPath is the sgl.yaml key of the search path of the PostgreSQL family.
	optionSearchPath = "search_path"
)

// DialectView describes one dialect the playground offers.
type DialectView struct {
	// Name is the dialect's name, as sgl.yaml spells it.
	Name string `json:"name"`

	// Family names the base dialect whose drivers the dialect shares.
	Family string `json:"family"`

	// Placeholder is a sample parameter placeholder, such as $1 or ?.
	Placeholder string `json:"placeholder"`

	// Drivers lists the go.driver values the dialect accepts: database/sql always, pgx for
	// the PostgreSQL family and clickhouse for ClickHouse.
	Drivers []string `json:"drivers"`

	// PreparedDrivers lists the drivers among Drivers that take go.prepared.
	PreparedDrivers []string `json:"preparedDrivers"`

	// ReturningOn lists the statements that accept a RETURNING clause as SQL keywords, such
	// as INSERT and DELETE on MariaDB.
	ReturningOn []string `json:"returningOn"`

	// Options lists the dialect_options keys among those the playground's controls set
	// (group_by_use_nulls, ansi_quotes and search_path) that the dialect takes.
	Options []string `json:"options"`

	// Returning reports whether any statement accepts a RETURNING clause.
	Returning bool `json:"returning"`
}

// dialectOptionProbe is one dialect option the playground's controls set.
type dialectOptionProbe struct {
	// set sets the option on a dialect's options.
	set func(options *dialect_registry.Options)

	// key is the option's sgl.yaml key under dialect_options.
	key string
}

var (
	// dataChanges lists each statement a dialect may accept a RETURNING clause on, in the
	// order the playground lists them.
	dataChanges = []dialect.DataChange{dialect.DataChangeInsert, dialect.DataChangeUpdate, dialect.DataChangeDelete, dialect.DataChangeMerge}

	// dialectOptionProbes lists the dialect options the playground's controls set, in the
	// order DialectView lists them, each with a way to set it, so the dialects package
	// itself says which dialects take it.
	dialectOptionProbes = []dialectOptionProbe{
		{key: optionGroupByUseNulls, set: func(options *dialect_registry.Options) { options.GroupByUseNulls = new(true) }},
		{key: optionANSIQuotes, set: func(options *dialect_registry.Options) { options.ANSIQuotes = new(true) }},
		{key: optionSearchPath, set: func(options *dialect_registry.Options) { options.SearchPath = []string{"public"} }},
	}
)

// Dialects describes every built-in dialect, sorted by name.
//
// Returns []DialectView which holds one entry per dialect.
// Returns error when a dialect cannot be built, which would be a fault in the library.
func Dialects() ([]DialectView, error) {
	names := dialect_registry.Names()
	views := make([]DialectView, 0, len(names))
	for _, name := range names {
		built, err := dialect_registry.New(name, dialect_registry.Options{})
		if err != nil {
			return nil, fmt.Errorf("dialect %s: %w", name, err)
		}
		info := built.Info()
		drivers := assemble.Drivers(&info)
		views = append(views, DialectView{
			Name:            name,
			Family:          info.Family,
			Placeholder:     info.Placeholders.Execution.Render(1, "name", "Type"),
			Drivers:         drivers,
			PreparedDrivers: slices.DeleteFunc(slices.Clone(drivers), func(driver string) bool { return !assemble.SupportsPrepared(driver) }),
			Returning:       info.Returning != 0,
			ReturningOn:     statementKeywords(info.Returning),
			Options:         takenDialectOptions(name),
		})
	}
	return views, nil
}

// statementKeywords lists the statements of a set as SQL keywords.
//
// Takes statements (dialect.DataChange) which is the set to list.
//
// Returns []string which holds one keyword per statement, empty for the empty set.
func statementKeywords(statements dialect.DataChange) []string {
	keywords := []string{}
	for _, change := range dataChanges {
		if statements.Includes(change) {
			keywords = append(keywords, change.String())
		}
	}
	return keywords
}

// takenDialectOptions lists the keys of the options among dialectOptionProbes a dialect
// takes.
//
// Takes name (string) which is the dialect's name.
//
// Returns []string which holds the keys in the probes' order, empty when it takes none.
func takenDialectOptions(name string) []string {
	keys := []string{}
	for _, probe := range dialectOptionProbes {
		if takesDialectOption(name, probe.key) {
			keys = append(keys, probe.key)
		}
	}
	return keys
}

// takesDialectOption reports whether a dialect accepts an option listed in
// dialectOptionProbes. Unrelated build failures are left for the run to report.
//
// Takes name (string) which is the dialect's name.
// Takes key (string) which is the option's sgl.yaml key.
//
// Returns bool which is true when the dialect takes the option, or the key is not one the
// probes list.
func takesDialectOption(name, key string) bool {
	index := slices.IndexFunc(dialectOptionProbes, func(probe dialectOptionProbe) bool { return probe.key == key })
	if index < 0 {
		return true
	}
	options := dialect_registry.Options{}
	dialectOptionProbes[index].set(&options)
	_, err := dialect_registry.New(name, options)
	return !errors.Is(err, dialect_registry.ErrInvalidOption)
}
