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

package emittest

import (
	"embed"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing/fstest"
)

const (
	// pgxModule is the module path of the pgx driver.
	pgxModule = "github.com/jackc/pgx/v5"

	// clickHouseModule is the module path of the clickhouse-go driver.
	clickHouseModule = "github.com/ClickHouse/clickhouse-go/v2"

	// stubsRoot is the directory holding the stand-in modules.
	stubsRoot = "internal/stubs"

	// pgxStub is the directory, under the stubs root, of the pgx module's stand-in, named
	// after the package it declares.
	pgxStub = "/pgx"

	// clickHouseStub is the directory, under the stubs root, of the clickhouse-go module's
	// stand-in, named after the package it declares.
	clickHouseStub = "/clickhouse"

	// stubsPackage is the import path of the directory holding the stand-in modules.
	stubsPackage = "seagull.sh/seagull/internal/emit/emittest/" + stubsRoot
)

var (
	// databaseSQLRecorder is the source of the database/sql harness.
	//
	//go:embed internal/sqlrecorder/recorder.go
	databaseSQLRecorder string

	// pgxRecorder is the source of the pgx harness.
	//
	//go:embed internal/pgxrecorder/recorder.go
	pgxRecorder string

	// clickHouseRecorder is the source of the native ClickHouse harness.
	//
	//go:embed internal/chrecorder/recorder.go
	clickHouseRecorder string

	// stubs holds the stand-in modules the pgx and native ClickHouse harnesses build with.
	//
	//go:embed internal/stubs
	stubs embed.FS

	// stubImports rewrites the stand-ins' import paths, which compile in this module, to the
	// paths of the modules they stand in for.
	stubImports = strings.NewReplacer(
		`"`+stubsPackage+pgxStub+`/pgconn"`, `"`+pgxModule+`/pgconn"`,
		`"`+stubsPackage+pgxStub+`"`, `"`+pgxModule+`"`,
		`"`+stubsPackage+clickHouseStub+`/lib/driver"`, `"`+clickHouseModule+`/lib/driver"`,
		`"`+stubsPackage+clickHouseStub+`/lib/chcol"`, `"`+clickHouseModule+`/lib/chcol"`,
		`"`+stubsPackage+clickHouseStub+`"`, `"`+clickHouseModule+`"`,
	)

	// packageClause matches a Go file's package clause.
	packageClause = regexp.MustCompile(`(?m)^package [A-Za-z_][A-Za-z0-9_]*$`)
)

// Harness is the runtime lane's stand-in for the driver an emitter writes Go for. The
// suite builds the generated packages into a program with it, calls every method of the
// runtime cases, and examines the statements the stand-in received.
//
// Source is one Go file, compiled into the program's package main whatever package clause
// it carries, that declares:
//
//	type Statement struct {
//		SQL       string
//		Arguments []any
//	}
//
//	func Connect() T
//	func Take() []Statement
//
// Connect returns the connection the generated package's New takes. Take returns the
// statements the stand-in received since it was last called, in order, and forgets them;
// each argument is as the generated code passed it, a named one as a sql.NamedArg or a
// driver.NamedValue. The stand-in answers every query with no rows.
type Harness struct {
	// Modules supplies stand-in module files for imports outside the standard library.
	// Missing modules exclude the generated package from the runtime lane.
	Modules map[string]fs.FS

	// Name names the harness in messages, such as database/sql.
	Name string

	// Source is the harness's Go source.
	Source string
}

// mainSource returns the harness's source as a file of package main.
//
// Returns string which is the source.
func (harness *Harness) mainSource() string {
	location := packageClause.FindStringIndex(harness.Source)
	if location == nil {
		return "package main\n\n" + harness.Source
	}
	return harness.Source[:location[0]] + "package main" + harness.Source[location[1]:]
}

// DatabaseSQL returns the harness for code written against database/sql: a driver that
// records every statement and its arguments as the generated code passed them.
//
// Returns *Harness which needs no module beside the standard library.
func DatabaseSQL() *Harness {
	return &Harness{Modules: nil, Name: "database/sql", Source: databaseSQLRecorder}
}

// Pgx returns the harness for code written against pgx: a connection that records every
// statement and its arguments, each statement of a batch, and each row CopyFrom sends as
// the INSERT the row stands for, with a stand-in for the pgx module that declares only
// what generated code uses.
//
// Returns *Harness which provides the pgx module's stand-in.
func Pgx() *Harness {
	return &Harness{Modules: map[string]fs.FS{pgxModule: stubModule(pgxStub)}, Name: "pgx", Source: stubImports.Replace(pgxRecorder)}
}

// ClickHouseNative returns a runtime harness that records native ClickHouse calls and
// batch rows.
//
// Returns *Harness which provides the clickhouse-go module's stand-in.
func ClickHouseNative() *Harness {
	return &Harness{
		Modules: map[string]fs.FS{clickHouseModule: stubModule(clickHouseStub)},
		Name:    "clickhouse native",
		Source:  stubImports.Replace(clickHouseRecorder),
	}
}

// stubModule returns the files of one stand-in module, at the module's root, their
// imports of other stand-ins rewritten to the modules they stand in for.
//
// Takes stub (string) which is the stand-in's directory under the stubs root, such as
// pgxStub.
//
// Returns fs.FS which holds the module's Go files.
func stubModule(stub string) fs.FS {
	root := stubsRoot + stub
	files := fstest.MapFS{}
	walkError := fs.WalkDir(stubs, root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || path.Ext(current) != ".go" || strings.HasSuffix(current, "_test.go") {
			return err
		}
		content, readError := fs.ReadFile(stubs, current)
		if readError != nil {
			return readError
		}
		files[strings.TrimPrefix(current, root+"/")] = &fstest.MapFile{Data: []byte(stubImports.Replace(string(content)))}
		return nil
	})
	if walkError != nil {
		return fstest.MapFS{}
	}
	return files
}
