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

package migrate

import (
	"context"
	"fmt"

	"seagull.sh/seagull/internal/sqlfile"
)

// MigrationDirection identifies whether a migration file applies (up) or reverts (down)
// its version.
type MigrationDirection uint8

const (
	// MigrationDirectionUp marks a migration file that applies its version.
	MigrationDirectionUp MigrationDirection = iota

	// MigrationDirectionDown marks a migration file that reverts its version.
	MigrationDirectionDown
)

// DuplicateMigrationVersionError is returned when two migration files share a version and
// direction, for example two 001_*.up.sql files. Match it with errors.As.
//
// Such a collision is common after a branch merge. Left alone, whichever file a
// version-keyed map saw last would win, so an applied record could be validated against
// the wrong file (a false checksum mismatch, or a false pass hiding drift) and the wrong
// down file kept for rollback. The error names both files so the conflict is resolved
// before any migration runs.
type DuplicateMigrationVersionError struct {
	// FirstFilename holds the filename first seen for the version and direction.
	FirstFilename string

	// SecondFilename holds the conflicting filename for the same version and direction.
	SecondFilename string

	// Version holds the shared numeric version.
	Version int64

	// Direction holds the shared migration direction.
	Direction MigrationDirection
}

// Error returns a human-readable message naming both conflicting files.
//
// Returns string which contains the direction label, version, and both filenames.
func (e *DuplicateMigrationVersionError) Error() string {
	direction := "up"
	if e.Direction == MigrationDirectionDown {
		direction = "down"
	}
	return fmt.Sprintf(
		"duplicate %s migration for version %d: %s and %s",
		direction, e.Version, e.FirstFilename, e.SecondFilename,
	)
}

// DuplicateSeedVersionError is returned when two seed files share a version, for example
// two 001_*.sql files. Match it with errors.As.
//
// Left alone, a version-keyed map would silently drop one of them; the error names both
// files so the collision is resolved before any seed runs.
type DuplicateSeedVersionError struct {
	// FirstFilename holds the filename first seen for the version.
	FirstFilename string

	// SecondFilename holds the conflicting filename for the same version.
	SecondFilename string

	// Version holds the shared numeric version.
	Version int64
}

// Error returns a human-readable message naming both conflicting files.
//
// Returns string which contains the version and both filenames.
func (e *DuplicateSeedVersionError) Error() string {
	return fmt.Sprintf(
		"duplicate seed for version %d: %s and %s",
		e.Version, e.FirstFilename, e.SecondFilename,
	)
}

// readMigrationFiles reads the migration files of a directory and rejects two that share
// a version and direction.
//
// Takes fileReader (sqlfile.FileReader) which reads the files.
// Takes directory (string) which is the migration directory.
//
// Returns []sqlfile.MigrationFile which holds the files in version order, up before down.
// Returns error which is a *DuplicateMigrationVersionError for a collision, or the
// reader's error.
func readMigrationFiles(ctx context.Context, fileReader sqlfile.FileReader, directory string) ([]sqlfile.MigrationFile, error) {
	files, err := sqlfile.ReadMigrationFilesVersioned(ctx, fileReader, directory)
	if err != nil {
		return nil, err
	}
	if first, second, found := sqlfile.DuplicateMigration(files); found {
		return nil, &DuplicateMigrationVersionError{
			FirstFilename:  first.Filename,
			SecondFilename: second.Filename,
			Version:        second.Version,
			Direction:      directionOf(second),
		}
	}
	return files, nil
}

// readSeedFiles reads the seed files of a directory and rejects two that share a version.
//
// Takes fileReader (sqlfile.FileReader) which reads the files.
// Takes directory (string) which is the seed directory.
//
// Returns []sqlfile.SeedFile which holds the files in version order.
// Returns error which is a *DuplicateSeedVersionError for a collision, or the reader's
// error.
func readSeedFiles(ctx context.Context, fileReader sqlfile.FileReader, directory string) ([]sqlfile.SeedFile, error) {
	files, err := sqlfile.ReadSeedFiles(ctx, fileReader, directory)
	if err != nil {
		return nil, err
	}
	if first, second, found := sqlfile.DuplicateSeed(files); found {
		return nil, &DuplicateSeedVersionError{
			FirstFilename:  first.Filename,
			SecondFilename: second.Filename,
			Version:        second.Version,
		}
	}
	return files, nil
}

// directionOf returns the direction of a migration file.
//
// Takes file (sqlfile.MigrationFile) which is the file.
//
// Returns MigrationDirection which is MigrationDirectionDown for a down file and
// MigrationDirectionUp otherwise.
func directionOf(file sqlfile.MigrationFile) MigrationDirection {
	if file.Down {
		return MigrationDirectionDown
	}
	return MigrationDirectionUp
}
