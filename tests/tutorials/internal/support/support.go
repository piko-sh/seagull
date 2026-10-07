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

// Package support supplies the connection setup shared by tutorial checkpoints.
package support

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	// The pure-Go SQLite driver registers itself with database/sql as "sqlite", the driver
	// Open asks for.
	_ "modernc.org/sqlite"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
)

// Open creates a SQLite connection and applies the supplied migrations.
func Open(ctx context.Context, files fs.FS, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	migrator, err := migrate.NewMigrator(db, migrate_backend_sqlite.New(), files, "db/migrations")
	if err == nil {
		_, err = migrator.Up(ctx)
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Main runs one checkpoint against a disposable SQLite file.
func Main(files fs.FS, run func(context.Context, *sql.DB, io.Writer) error) error {
	directory, err := os.MkdirTemp("", "seagull-tutorial-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	ctx := context.Background()
	db, err := Open(ctx, files, filepath.Join(directory, "tasks.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := run(ctx, db, os.Stdout); err != nil {
		return fmt.Errorf("tutorial: %w", err)
	}
	return nil
}
