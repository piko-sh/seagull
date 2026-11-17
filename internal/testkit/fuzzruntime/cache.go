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

package fuzzruntime

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// cacheLeaseDirectory holds the exclusive worker ownership record.
	cacheLeaseDirectory = "lease"

	// cacheLeaseOwnerName names the worker identity inside the lease directory.
	cacheLeaseOwnerName = "owner.json"

	// cacheOwnerMarkerName names the file that marks a directory as owned by this cache.
	cacheOwnerMarkerName = "owner"

	// cacheManifestName names the metadata of one retained program.
	cacheManifestName = "manifest.json"

	// cacheStagePrefix starts the name of a directory used while retaining a program.
	cacheStagePrefix = "stage-"

	// cacheProgramName names an immutable compiled executable.
	cacheProgramName = "program"

	// cacheOwner identifies directories created by this compiled-program cache.
	cacheOwner = "seagull-native-programs-v1\n"

	// cacheCampaignVariable names the environment variable identifying the campaign command.
	cacheCampaignVariable = "SEAGULL_FUZZ_COMMAND_TOKEN"

	// cacheDirectoryMode restricts cache directories to their owner.
	cacheDirectoryMode = 0o700

	// cacheExecutableMode lets only the owner read and run a retained program.
	cacheExecutableMode = 0o700

	// cacheFileMode restricts cache metadata to its owner.
	cacheFileMode = 0o600

	// cacheExecutableBits selects executable permission bits.
	cacheExecutableBits = 0o111

	// cacheManifestLimit bounds untrusted cached metadata.
	cacheManifestLimit = 4096

	// cacheKeySizeBytes is the width of the length prefix of each key part.
	cacheKeySizeBytes = 8
)

// ProgramCache owns a bounded store of compiled fuzz helpers.
//
// A worker holds the exclusive lease until Close. A stale lease requires explicit
// operator inspection before removal.
type ProgramCache struct {
	// root contains only managed cache entries and the ownership marker.
	root string

	// maximumBytes bounds retained executable bytes.
	maximumBytes int64

	// maximumEntries bounds the number of retained programs.
	maximumEntries int

	// closed prevents use after ownership has been released.
	closed bool
}

// cachedProgram records the identity and integrity of one retained executable.
type cachedProgram struct {
	// Key identifies the complete build inputs.
	Key string

	// Digest detects modified or truncated executable bytes.
	Digest string

	// Size records the executable size in bytes.
	Size int64

	// Used records the most recent successful reuse.
	Used int64
}

// Close releases the worker's exclusive cache lease.
//
// Returns error when the lease cannot be released.
func (cache *ProgramCache) Close() error {
	if cache.closed {
		return nil
	}
	if err := os.Remove(filepath.Join(cache.root, cacheLeaseDirectory, cacheLeaseOwnerName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing compiled cache lease owner: %w", err)
	}
	if err := os.Remove(filepath.Join(cache.root, cacheLeaseDirectory)); err != nil {
		return fmt.Errorf("removing compiled cache lease: %w", err)
	}
	cache.closed = true
	return nil
}

// Find validates a retained executable before making it available for reuse.
//
// Takes key (string) which identifies every effective build input.
//
// Returns string which names the verified executable, or is empty on a cache miss.
// Returns error when ownership or retained metadata is invalid.
func (cache *ProgramCache) Find(key string) (string, error) {
	if err := cache.checkKey(key); err != nil {
		return "", err
	}
	entry, err := cache.readEntry(key)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	path := filepath.Join(cache.root, key, cacheProgramName)
	digest, size, err := cacheProgramDigest(path, cache.maximumBytes)
	if err != nil {
		return "", err
	}
	if digest != entry.Digest || size != entry.Size {
		return "", errors.New("compiled cache executable failed integrity verification")
	}
	entry.Used = time.Now().UnixNano()
	if err := cache.writeEntry(entry); err != nil {
		return "", err
	}
	return path, nil
}

// Retain atomically stores a compiled helper and evicts the oldest managed entries.
//
// Takes key (string) which fingerprints all effective build inputs.
// Takes binaryPath (string) which names the completed executable.
//
// Returns string which names the retained executable.
// Returns error when the executable is invalid or exceeds the cache budget.
func (cache *ProgramCache) Retain(key, binaryPath string) (string, error) {
	if err := cache.checkKey(key); err != nil {
		return "", err
	}
	digest, size, err := cacheProgramDigest(binaryPath, cache.maximumBytes)
	if err != nil {
		return "", err
	}
	if existing, findErr := cache.Find(key); findErr != nil || existing != "" {
		return existing, findErr
	}
	if err := cache.makeRoom(size); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(cache.root, cacheStagePrefix)
	if err != nil {
		return "", fmt.Errorf("creating compiled cache stage: %w", err)
	}
	defer os.RemoveAll(stage)
	if err := cache.stageProgram(stage, binaryPath, cachedProgram{Key: key, Digest: digest, Size: size, Used: time.Now().UnixNano()}); err != nil {
		return "", err
	}
	if err := os.Rename(stage, filepath.Join(cache.root, key)); err != nil {
		return "", fmt.Errorf("publishing compiled cache entry: %w", err)
	}
	return filepath.Join(cache.root, key, cacheProgramName), nil
}

// stageProgram copies and verifies an executable and writes its manifest in a stage.
//
// Takes stage (string) which is the private staging directory.
// Takes binaryPath (string) which names the completed executable.
// Takes entry (cachedProgram) which holds the digest and size measured before copying.
//
// Returns error when the copy differs from the measured executable or cannot be written.
func (cache *ProgramCache) stageProgram(stage, binaryPath string, entry cachedProgram) error {
	program := filepath.Join(stage, cacheProgramName)
	if err := copyCacheProgram(binaryPath, program); err != nil {
		return err
	}
	storedDigest, storedSize, err := cacheProgramDigest(program, cache.maximumBytes)
	if err != nil {
		return err
	}
	if storedDigest != entry.Digest || storedSize != entry.Size {
		return errors.New("compiled helper changed while being retained")
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encoding compiled cache manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stage, cacheManifestName), encoded, cacheFileMode); err != nil {
		return fmt.Errorf("writing compiled cache manifest: %w", err)
	}
	return nil
}

// checkKey rejects closed ownership and unsafe entry names.
//
// Takes key (string) which must be a lowercase SHA-256 digest.
//
// Returns error when the cache is closed or the key is invalid.
func (cache *ProgramCache) checkKey(key string) error {
	if cache.closed {
		return errors.New("compiled cache lease is closed")
	}
	decoded, err := hex.DecodeString(key)
	if err != nil || len(decoded) != sha256.Size || strings.ToLower(key) != key {
		return errors.New("invalid compiled cache key")
	}
	return nil
}

// readEntry bounds metadata and rejects entries outside the owned directory.
//
// Takes key (string) which identifies one retained program.
//
// Returns cachedProgram which contains validated metadata.
// Returns error when the manifest is missing, oversized or malformed.
func (cache *ProgramCache) readEntry(key string) (cachedProgram, error) {
	manifest := filepath.Join(cache.root, key, cacheManifestName)
	if err := cacheRealPath(manifest); err != nil {
		return cachedProgram{}, err
	}
	file, err := cacheOpenFile(manifest, os.O_RDONLY, 0)
	if err != nil {
		return cachedProgram{}, fmt.Errorf("opening compiled cache manifest: %w", err)
	}
	defer file.Close()
	content, err := readBounded(file, cacheManifestLimit)
	if err != nil {
		return cachedProgram{}, fmt.Errorf("reading compiled cache manifest: %w", err)
	}
	var entry cachedProgram
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entry); err != nil {
		return cachedProgram{}, fmt.Errorf("decoding compiled cache manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return cachedProgram{}, errors.New("compiled cache manifest has trailing data")
	}
	if entry.Key != key || entry.Size < 0 {
		return cachedProgram{}, errors.New("compiled cache manifest identity differs")
	}
	return entry, nil
}

// writeEntry atomically updates the last-use metadata of a verified entry.
//
// Takes entry (cachedProgram) which records one validated program.
//
// Returns error when metadata cannot be written atomically.
func (cache *ProgramCache) writeEntry(entry cachedProgram) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encoding compiled cache manifest: %w", err)
	}
	directory := filepath.Join(cache.root, entry.Key)
	temporary, err := os.CreateTemp(directory, "manifest-")
	if err != nil {
		return fmt.Errorf("creating compiled cache manifest: %w", err)
	}
	defer os.Remove(temporary.Name())
	_, writeErr := temporary.Write(data)
	if err := errors.Join(writeErr, temporary.Close()); err != nil {
		return fmt.Errorf("writing compiled cache manifest: %w", err)
	}
	if err := os.Rename(temporary.Name(), filepath.Join(directory, cacheManifestName)); err != nil {
		return fmt.Errorf("replacing compiled cache manifest: %w", err)
	}
	return nil
}

// makeRoom removes only validated managed entries in last-use order.
//
// Takes incoming (int64) which is the size of the new executable.
//
// Returns error when managed entries cannot be inspected or evicted safely.
func (cache *ProgramCache) makeRoom(incoming int64) error {
	entries, total, err := cache.entries()
	if err != nil {
		return err
	}
	slices.SortFunc(entries, func(left, right cachedProgram) int {
		return cmp.Or(cmp.Compare(left.Used, right.Used), strings.Compare(left.Key, right.Key))
	})
	count := len(entries)
	for _, entry := range entries {
		if count < cache.maximumEntries && total <= cache.maximumBytes-incoming {
			return nil
		}
		if err := os.RemoveAll(filepath.Join(cache.root, entry.Key)); err != nil {
			return fmt.Errorf("evicting compiled cache entry: %w", err)
		}
		total -= entry.Size
		count--
	}
	return nil
}

// entries validates retained files before accounting for their storage.
//
// Returns []cachedProgram which lists validated cache entries.
// Returns int64 which totals retained executable bytes.
// Returns error when an entry or directory is unsafe or malformed.
func (cache *ProgramCache) entries() ([]cachedProgram, int64, error) {
	names, err := os.ReadDir(cache.root)
	if err != nil {
		return nil, 0, fmt.Errorf("listing compiled cache: %w", err)
	}
	entries := make([]cachedProgram, 0, len(names))
	var total int64
	for _, name := range names {
		if !name.IsDir() || name.Name() == cacheLeaseDirectory {
			continue
		}
		if err := cache.checkKey(name.Name()); err != nil {
			return nil, 0, err
		}
		entry, readErr := cache.readEntry(name.Name())
		if readErr != nil {
			return nil, 0, readErr
		}
		_, size, digestErr := cacheProgramDigest(filepath.Join(cache.root, name.Name(), cacheProgramName), cache.maximumBytes)
		if digestErr != nil {
			return nil, 0, digestErr
		}
		if size != entry.Size {
			return nil, 0, errors.New("compiled cache manifest understates or overstates executable storage")
		}
		entries = append(entries, entry)
		total += entry.Size
	}
	return entries, total, nil
}

// removeStaleStages deletes stage directories that an interrupted worker left behind.
//
// The caller holds the exclusive lease, so no other worker can be staging.
//
// Returns error when the cache cannot be listed or a stage cannot be removed.
func (cache *ProgramCache) removeStaleStages() error {
	names, err := os.ReadDir(cache.root)
	if err != nil {
		return fmt.Errorf("listing compiled cache: %w", err)
	}
	for _, name := range names {
		if !name.IsDir() || !strings.HasPrefix(name.Name(), cacheStagePrefix) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(cache.root, name.Name())); err != nil {
			return fmt.Errorf("removing stale compiled cache stage: %w", err)
		}
	}
	return nil
}

// ProgramKey fingerprints ordered build inputs without ambiguous concatenation.
//
// Takes parts (...[]byte) which includes source, dependencies, toolchain and build flags.
//
// Returns string which names an immutable compiled shape.
func ProgramKey(parts ...[]byte) string {
	hash := sha256.New()
	var size [cacheKeySizeBytes]byte
	for _, part := range parts {
		binary.LittleEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write(part)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// OpenProgramCache acquires exclusive ownership of a bounded cache directory.
//
// Takes root (string) which must be a real directory with no symlink ancestors.
// Takes maximumBytes (int64) which bounds retained executable bytes.
// Takes maximumEntries (int) which bounds retained program count.
//
// Returns *ProgramCache which must be closed by its owner.
// Returns error when limits, ownership or filesystem paths are invalid.
func OpenProgramCache(root string, maximumBytes int64, maximumEntries int) (*ProgramCache, error) {
	if maximumBytes <= 0 || maximumEntries <= 0 {
		return nil, errors.New("compiled cache limits must be positive")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving compiled cache path: %w", err)
	}
	if err := cacheRealPath(absolute); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, cacheDirectoryMode); err != nil {
		return nil, fmt.Errorf("creating compiled cache: %w", err)
	}
	if err := ownProgramCache(absolute); err != nil {
		return nil, err
	}
	if err := os.Mkdir(filepath.Join(absolute, cacheLeaseDirectory), cacheDirectoryMode); err != nil {
		return nil, fmt.Errorf("compiled cache ownership: %w", err)
	}
	cache := &ProgramCache{root: absolute, maximumBytes: maximumBytes, maximumEntries: maximumEntries, closed: false}
	if err := recordCacheLease(absolute); err != nil {
		return nil, errors.Join(err, cache.Close())
	}
	if err := cache.removeStaleStages(); err != nil {
		return nil, errors.Join(err, cache.Close())
	}
	return cache, nil
}

// cacheRealPath rejects symlinks before creating or reading owned directories.
//
// Takes path (string) which names the absolute cache directory.
//
// Returns error when an existing ancestor is a symlink or cannot be inspected.
func cacheRealPath(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspecting compiled cache path: %w", err)
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("compiled cache paths must not contain symlinks")
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

// ownProgramCache adopts only empty directories or an exact ownership marker.
//
// Takes root (string) which must be empty or already owned by this cache.
//
// Returns error when ownership cannot be established safely.
func ownProgramCache(root string) error {
	marker := filepath.Join(root, cacheOwnerMarkerName)
	if err := cacheRealPath(marker); err != nil {
		return err
	}
	existing, err := readCacheOwner(marker)
	if err == nil {
		if string(existing) != cacheOwner {
			return errors.New("compiled cache ownership marker differs")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("listing compiled cache: %w", err)
	}
	if len(entries) != 0 {
		return errors.New("refusing to adopt a non-empty compiled cache directory")
	}
	file, err := cacheOpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, cacheFileMode)
	if err != nil {
		return fmt.Errorf("creating compiled cache ownership marker: %w", err)
	}
	_, writeErr := file.WriteString(cacheOwner)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("writing compiled cache ownership marker: %w", err)
	}
	return nil
}

// cacheProgramDigest verifies a regular executable and hashes its exact bytes.
//
// Takes path (string) which names the retained executable.
// Takes limit (int64) which is the largest executable the cache accepts.
//
// Returns string which is the SHA-256 digest.
// Returns int64 which is the executable size in bytes.
// Returns error when the file is unsafe, unreadable, not executable or over the limit.
func cacheProgramDigest(path string, limit int64) (string, int64, error) {
	if err := cacheRealPath(path); err != nil {
		return "", 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, fmt.Errorf("inspecting compiled helper: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&cacheExecutableBits == 0 {
		return "", 0, errors.New("compiled helper is not a regular executable")
	}
	file, err := cacheOpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return "", 0, fmt.Errorf("opening compiled helper: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, limit+1))
	if err != nil {
		return "", 0, fmt.Errorf("hashing compiled helper: %w", err)
	}
	if size > limit {
		return "", 0, fmt.Errorf("compiled helper exceeds the cache byte budget of %d bytes", limit)
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

// copyCacheProgram writes a private executable without following destination links.
//
// Takes source (string) which names the completed executable.
// Takes destination (string) which names the new cache file.
//
// Returns error when the executable cannot be copied safely.
func copyCacheProgram(source, destination string) error {
	input, err := cacheOpenFile(source, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("opening compiled helper: %w", err)
	}
	defer input.Close()
	output, err := cacheOpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, cacheExecutableMode)
	if err != nil {
		return fmt.Errorf("creating cached compiled helper: %w", err)
	}
	_, copyErr := io.Copy(output, input)
	if err := errors.Join(copyErr, output.Close()); err != nil {
		return fmt.Errorf("copying compiled helper: %w", err)
	}
	return nil
}

// cacheOpenFile confines a single filename to its verified parent directory.
//
// Takes path (string) which names a file within its parent directory.
// Takes flags (int) which selects the required open operation.
// Takes mode (os.FileMode) which restricts permissions for new files.
//
// Returns *os.File which must be closed by the caller.
// Returns error when the confined file cannot be opened.
func cacheOpenFile(path string, flags int, mode os.FileMode) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenFile(filepath.Base(path), flags, mode)
}

// readCacheOwner bounds ownership metadata before adopting an existing directory.
//
// Takes path (string) which names the ownership marker.
//
// Returns []byte which contains the bounded marker contents.
// Returns error when the marker cannot be read safely or exceeds its bound.
func readCacheOwner(path string) ([]byte, error) {
	file, err := cacheOpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := readBounded(file, cacheManifestLimit)
	if err != nil {
		return nil, fmt.Errorf("reading compiled cache ownership marker: %w", err)
	}
	return content, nil
}

// readBounded reads a whole reader and reports content beyond the limit.
//
// Takes reader (io.Reader) which supplies untrusted metadata.
// Takes limit (int64) which is the largest accepted size in bytes.
//
// Returns []byte which holds the content.
// Returns error when reading fails or the content exceeds the limit.
func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("content exceeds its %d byte bound", limit)
	}
	return content, nil
}

// recordCacheLease identifies the worker for bounded campaign interruption cleanup.
//
// Takes root (string) which owns the newly created exclusive lease directory.
//
// Returns error when the worker identity cannot be recorded safely.
func recordCacheLease(root string) error {
	owner := struct {
		CommandToken string

		PID int
	}{CommandToken: os.Getenv(cacheCampaignVariable), PID: os.Getpid()}
	content, err := json.Marshal(owner)
	if err != nil {
		return fmt.Errorf("encoding compiled cache lease: %w", err)
	}
	file, err := cacheOpenFile(filepath.Join(root, cacheLeaseDirectory, cacheLeaseOwnerName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, cacheFileMode)
	if err != nil {
		return fmt.Errorf("creating compiled cache lease: %w", err)
	}
	_, writeErr := file.Write(content)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("writing compiled cache lease: %w", err)
	}
	return nil
}
