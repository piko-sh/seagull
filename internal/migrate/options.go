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
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqlfile"
)

const (
	// defaultMaxFiles caps the entries in a migration or seed directory when WithMaxFiles is
	// not given.
	defaultMaxFiles = sqlfile.DefaultMaxFiles

	// defaultMaxTotalBytes caps the bytes one call reads across every migration or seed file
	// when WithMaxTotalBytes is not given: 1 GiB.
	defaultMaxTotalBytes int64 = 1 << 30
)

// Option configures a Migrator or a Seeder. Options that concern only migrations, such as
// the hooks, are ignored by a Seeder.
type Option func(*settings)

// settings holds the configuration Options set.
type settings struct {
	// logger, when set, replaces the logger carried by each call's context.
	logger *slog.Logger

	// observer, when set, replaces the observer carried by each call's context.
	observer observe.ObserverPort

	// beforeMigrationHooks holds hooks invoked before each individual migration.
	beforeMigrationHooks []BeforeMigrationHook

	// afterMigrationHooks holds hooks invoked after each individual migration.
	afterMigrationHooks []AfterMigrationHook

	// beforeRunHooks holds hooks invoked before a batch of migrations begins.
	beforeRunHooks []BeforeRunHook

	// afterRunHooks holds hooks invoked after a batch of migrations completes.
	afterRunHooks []AfterRunHook

	// prefixes holds the directive prefixes recognised when scanning a migration for a
	// no-transaction directive.
	prefixes namespace.Set

	// maxFileBytes caps the size of each migration or seed file.
	maxFileBytes int64

	// maxTotalBytes caps the bytes one call reads across every migration or seed file.
	maxTotalBytes int64

	// maxFiles caps the entries in the migration or seed directory.
	maxFiles int

	// lockTimeout bounds how long lock acquisition waits; zero waits until the call's
	// context ends.
	lockTimeout time.Duration

	// nonBlockingLock indicates whether lock acquisition should be non-blocking.
	nonBlockingLock bool
}

// newSettings applies options over the defaults.
//
// Takes options ([]Option) which configure the run time.
//
// Returns settings which holds the configuration.
// Returns error which wraps ErrInvalidConfig when an option is invalid.
func newSettings(options []Option) (settings, error) {
	configured := settings{
		logger:               nil,
		observer:             nil,
		beforeMigrationHooks: nil,
		afterMigrationHooks:  nil,
		beforeRunHooks:       nil,
		afterRunHooks:        nil,
		prefixes:             namespace.Default(),
		maxFileBytes:         sqlfile.DefaultMaxFileBytes,
		maxTotalBytes:        defaultMaxTotalBytes,
		maxFiles:             defaultMaxFiles,
		lockTimeout:          0,
		nonBlockingLock:      false,
	}
	for _, option := range options {
		option(&configured)
	}
	if err := configured.validate(); err != nil {
		return settings{}, err
	}
	return configured, nil
}

// validate checks the configuration the options set.
//
// Returns error which wraps ErrInvalidConfig naming the first invalid option, or nil.
func (configured *settings) validate() error {
	if err := configured.validateLimits(); err != nil {
		return err
	}
	if configured.lockTimeout < 0 {
		return fmt.Errorf("WithLockTimeout: timeout must not be negative, got %s: %w", configured.lockTimeout, ErrInvalidConfig)
	}
	for _, prefix := range configured.prefixes.Accepted() {
		if !namespace.ValidPrefix(prefix) {
			return fmt.Errorf("WithDirectivePrefixes: prefix %q must match [a-z][a-z0-9_]{0,31}: %w", prefix, ErrInvalidConfig)
		}
	}
	return configured.validateHooks()
}

// validateLimits checks the caps on the files a call reads.
//
// Returns error which wraps ErrInvalidConfig naming the first invalid cap, or nil.
func (configured *settings) validateLimits() error {
	if configured.maxFileBytes <= 0 {
		return fmt.Errorf("WithMaxFileBytes: limit must be positive, got %d: %w", configured.maxFileBytes, ErrInvalidConfig)
	}
	if configured.maxFileBytes > sqlfile.MaxFileBytesCeiling {
		return fmt.Errorf("WithMaxFileBytes: limit must be at most %d, got %d: %w", sqlfile.MaxFileBytesCeiling, configured.maxFileBytes, ErrInvalidConfig)
	}
	if configured.maxFiles <= 0 {
		return fmt.Errorf("WithMaxFiles: limit must be positive, got %d: %w", configured.maxFiles, ErrInvalidConfig)
	}
	if configured.maxTotalBytes <= 0 {
		return fmt.Errorf("WithMaxTotalBytes: limit must be positive, got %d: %w", configured.maxTotalBytes, ErrInvalidConfig)
	}
	return nil
}

// callReader returns the reader one call reads its files through: the configured reader,
// charging what it reads to a budget of its own when it can take one, so each call may
// read up to the WithMaxTotalBytes cap.
//
// Takes reader (sqlfile.FileReader) which is the configured reader.
//
// Returns sqlfile.FileReader which reads the call's files.
func (configured *settings) callReader(reader sqlfile.FileReader) sqlfile.FileReader {
	if budgeted, ok := reader.(*sqlfile.FSReader); ok {
		return budgeted.WithBudget(sqlfile.NewByteBudget(configured.maxTotalBytes))
	}
	return reader
}

// validateHooks refuses a nil hook, which would otherwise fail only when a run reached
// it, while the lock is held.
//
// Returns error which wraps ErrInvalidConfig naming the option given a nil hook, or nil.
func (configured *settings) validateHooks() error {
	nilHooks := []struct {
		option string
		found  bool
	}{
		{option: "WithBeforeMigration", found: slices.ContainsFunc(configured.beforeMigrationHooks, func(hook BeforeMigrationHook) bool { return hook == nil })},
		{option: "WithAfterMigration", found: slices.ContainsFunc(configured.afterMigrationHooks, func(hook AfterMigrationHook) bool { return hook == nil })},
		{option: "WithBeforeRun", found: slices.ContainsFunc(configured.beforeRunHooks, func(hook BeforeRunHook) bool { return hook == nil })},
		{option: "WithAfterRun", found: slices.ContainsFunc(configured.afterRunHooks, func(hook AfterRunHook) bool { return hook == nil })},
	}
	for _, check := range nilHooks {
		if check.found {
			return fmt.Errorf("%s: hook must not be nil: %w", check.option, ErrInvalidConfig)
		}
	}
	return nil
}

// contextFor attaches the configured logger and observer, when set, to a context.
//
// Returns context.Context which carries the logger and observer.
func (configured *settings) contextFor(ctx context.Context) context.Context {
	if configured.logger != nil {
		ctx = logging.ContextWithLogger(ctx, configured.logger)
	}
	if configured.observer != nil {
		ctx = observe.ContextWithObserver(ctx, configured.observer)
	}
	return ctx
}

// acquireWithin runs a lock acquisition under the configured lock timeout. The context
// the acquisition receives ends when the timeout passes, with a cause naming the lock and
// the timeout, and again once the acquisition returns.
//
// Takes lock (string) which names the lock for the timeout's cause, such as migration.
// Takes acquire (func(context.Context) error) which takes the lock.
//
// Returns error which is the acquisition's error, wrapped with the timeout's cause when
// the timeout cut it short.
func (configured *settings) acquireWithin(ctx context.Context, lock string, acquire func(context.Context) error) error {
	if configured.lockTimeout <= 0 {
		return acquire(ctx)
	}
	cause := fmt.Errorf("%w: the %s lock was still held elsewhere after %s", ErrLockTimeout, lock, configured.lockTimeout)
	lockContext, cancel := context.WithTimeoutCause(ctx, configured.lockTimeout, cause)
	defer cancel()
	err := acquire(lockContext)
	if err == nil || ctx.Err() != nil || !errors.Is(context.Cause(lockContext), ErrLockTimeout) || errors.Is(err, ErrLockTimeout) {
		return err
	}
	return fmt.Errorf("%w: %w", cause, err)
}

// WithNonBlockingLock makes the Migrator fail at once with ErrLockNotAcquired when
// another process holds the migration lock, instead of waiting for it, and a Seeder do
// the same with the seed lock. NewSeeder returns an error wrapping ErrInvalidConfig when
// the SeedExecutorPort does not implement SeedLockTrierPort.
//
// Returns Option which enables the non-blocking lock.
func WithNonBlockingLock() Option {
	return func(configured *settings) {
		configured.nonBlockingLock = true
	}
}

// WithLockTimeout limits how long migration or seed lock acquisition waits.
//
// It does not limit execution after the lock is acquired. Zero waits until context
// cancellation; an expired timeout returns ErrLockTimeout without making changes.
//
// Takes timeout (time.Duration) which is the longest wait; it must not be negative.
//
// Returns Option which sets the timeout.
func WithLockTimeout(timeout time.Duration) Option {
	return func(configured *settings) {
		configured.lockTimeout = timeout
	}
}

// WithDirectivePrefixes sets the directive prefixes recognised when a migration is
// scanned for a `-- <prefix>.migration(no_transaction: true)` directive.
//
// The default is the canonical sgl prefix with its seagull alias. Prefixes are matched
// literally, so each must be a lower-case identifier.
//
// Takes canonical (string) which is the canonical directive prefix.
// Takes aliases (...string) which are further prefixes accepted as synonyms.
//
// Returns Option which sets the recognised prefixes.
func WithDirectivePrefixes(canonical string, aliases ...string) Option {
	return func(configured *settings) {
		configured.prefixes = namespace.Set{
			Canonical: canonical,
			Aliases:   slices.Clone(aliases),
			Foreign:   nil,
		}
	}
}

// WithLogger sets the logger every call logs through. Without it a call logs through the
// logger carried by its context, or slog.Default().
//
// Takes logger (*slog.Logger) which receives the log records; nil keeps the default.
//
// Returns Option which sets the logger.
func WithLogger(logger *slog.Logger) Option {
	return func(configured *settings) {
		configured.logger = logger
	}
}

// WithObserver sets the observer that receives a span for each migration and seed
// operation. Without it a call reports to the observer carried by its context, or to
// none.
//
// Takes observer (observe.ObserverPort) which receives the spans; nil keeps the default.
//
// Returns Option which sets the observer.
func WithObserver(observer observe.ObserverPort) Option {
	return func(configured *settings) {
		configured.observer = observer
	}
}

// WithMaxFileBytes caps the size of each migration or seed file.
//
// A larger file fails the call with an error wrapping ErrFileTooLarge. The default is 4
// MiB, and the cap may be at most 1 TiB.
//
// Takes limit (int64) which is the maximum size in bytes; it must be positive and at most
// 1 TiB.
//
// Returns Option which sets the cap.
func WithMaxFileBytes(limit int64) Option {
	return func(configured *settings) {
		configured.maxFileBytes = limit
	}
}

// WithMaxFiles caps the entries in the migration or seed directory.
//
// A larger directory fails the call with an error wrapping ErrLimit before any file is
// read. The default is 10,000.
//
// Takes limit (int) which is the maximum number of entries; it must be positive.
//
// Returns Option which sets the cap.
func WithMaxFiles(limit int) Option {
	return func(configured *settings) {
		configured.maxFiles = limit
	}
}

// WithMaxTotalBytes caps the bytes one call reads across every migration or seed file.
//
// Each call reads the files afresh, such as Up, Status or Apply, and reading past the cap
// fails it with an error wrapping ErrLimit. The default is 1 GiB.
//
// Takes limit (int64) which is the maximum total size in bytes; it must be positive.
//
// Returns Option which sets the cap.
func WithMaxTotalBytes(limit int64) Option {
	return func(configured *settings) {
		configured.maxTotalBytes = limit
	}
}
