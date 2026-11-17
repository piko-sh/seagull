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

package harness

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

const (
	// fuzzMetadataPermissions limits campaign metadata to its owner.
	fuzzMetadataPermissions = 0o600

	// fuzzMetadataDirectoryVariable names the directory a campaign collects metadata in.
	fuzzMetadataDirectoryVariable = "SEAGULL_FUZZ_METADATA_DIR"

	// fuzzManagedRootVariable names the directory a campaign keeps compiled helpers in.
	fuzzManagedRootVariable = "SEAGULL_FUZZ_MANAGED_ROOT"
)

// fuzzEngineMetadata records the engine observed by one native fuzz process.
type fuzzEngineMetadata struct {
	// Engine identifies the dialect independently of its driver.
	Engine string `json:"engine"`

	// Version is reported by the actual database instance.
	Version string `json:"version"`

	// Process distinguishes the coordinator and its workers.
	Process int `json:"process"`
}

// writeFuzzEngineMetadata records server versions for an explicitly requested campaign.
//
// Takes t (*testing.T) which reports missing or unwritable metadata.
// Takes database (*sql.DB) which is the input's isolated database.
// Takes engine (string) which identifies the suite's SQL dialect.
func writeFuzzEngineMetadata(ctx context.Context, t *testing.T, database *sql.DB, engine string) {
	t.Helper()
	directory := os.Getenv(fuzzMetadataDirectoryVariable)
	if directory == "" {
		return
	}
	ctx, cancel := context.WithTimeoutCause(ctx, scaled(databaseStatementDeadline), errFuzzStatementDeadline)
	defer cancel()
	statement := "SELECT version()"
	if engine == engineSQLite {
		statement = "SELECT sqlite_version()"
	}
	metadata := fuzzEngineMetadata{Engine: engine, Version: "", Process: os.Getpid()}
	require.NoError(t, database.QueryRowContext(ctx, statement).Scan(&metadata.Version), "read %s engine version", engine)
	require.NotEmpty(t, metadata.Version, "database returned an empty engine version")
	encoded, err := json.Marshal(metadata)
	require.NoError(t, err)
	name := fmt.Sprintf("%s-%d.json", engine, metadata.Process)
	writeFuzzMetadataFile(t, directory, name, encoded)
	writeFuzzCapabilities(t, "grammar", engine, fuzzcase.QueryGrammarCapabilities(engine))
}

// writeNativeCapabilities retains admitted operations and exclusions with campaign
// evidence.
//
// Takes t (*testing.T) which reports unsafe or unwritable artefact paths.
// Takes engine (string) which identifies the independent native contracts.
func writeNativeCapabilities(t *testing.T, engine string) {
	t.Helper()
	capabilities := slices.Concat(fuzzcase.RuntimeCapabilities(engine), fuzzcase.RuntimeOperationCapabilities(engine))
	writeFuzzCapabilities(t, "native", engine, capabilities)
}

// writeFuzzCapabilities retains independent capability contracts beside campaign
// evidence.
//
// Takes t (*testing.T) which reports unsafe or unwritable artefact paths.
// Takes lane (string) which identifies a fixed native fuzzing surface.
// Takes engine (string) which identifies the independent engine contracts.
// Takes capabilities ([]fuzzcase.Capability) which records admitted and excluded
// operations.
func writeFuzzCapabilities(t *testing.T, lane, engine string, capabilities []fuzzcase.Capability) {
	t.Helper()
	directory := os.Getenv(fuzzMetadataDirectoryVariable)
	if directory == "" {
		return
	}
	encoded, err := json.Marshal(capabilities)
	require.NoError(t, err)
	writeFuzzMetadataFile(t, directory, fmt.Sprintf("%s-capabilities-%s-%d.json", lane, engine, os.Getpid()), encoded)
}

// writeFuzzMetadataFile writes one artefact inside the campaign's metadata directory.
//
// Takes t (*testing.T) which reports unsafe or unwritable paths.
// Takes directory (string) which is the campaign's metadata directory.
// Takes name (string) which is the artefact's name inside it.
// Takes content ([]byte) which is the artefact.
func writeFuzzMetadataFile(t *testing.T, directory, name string, content []byte) {
	t.Helper()
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	require.NoError(t, root.WriteFile(name, content, fuzzMetadataPermissions), "writing %s", name)
	require.NoError(t, root.Close())
}
