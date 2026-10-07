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

// Package migrate applies versioned SQL migrations and seed files at run time.
//
// A Migrator reads numbered up and down files, verifies their checksums against the
// recorded history, and applies or reverts them through an ExecutorPort (running outside
// a transaction when a migration requires it), with hooks before and after each migration
// and each run. Force and Repair recover the history without running a migration: Force
// records one as applied, and Repair, given a plan from PlanRepair, records changed
// files' checksums and how much of a dirty migration ran. A Seeder applies seed files
// once each and can clear the seed history and apply them again. Both acquire the
// executor's lock, waiting for it, failing at once (WithNonBlockingLock) or giving up
// after a timeout (WithLockTimeout). Coordination across processes depends on that lock
// implementation.
//
// Executors for database/sql live in the sqlexec subpackage, whose NewMigrator and
// NewSeeder are the usual entry points; any other store implements ExecutorPort and
// SeedExecutorPort and calls New and NewSeeder here. The package never links the analyser
// or the code generator.
package migrate
