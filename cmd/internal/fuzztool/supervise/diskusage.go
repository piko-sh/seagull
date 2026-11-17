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

//go:build linux || darwin

package supervise

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

const (
	// blockSize is the unit of a stat block count.
	blockSize = 512

	// maxDepth bounds directory recursion in a managed tree.
	maxDepth = 1024

	// readingFormat describes a failed read.
	readingFormat = "reading %s: %w"
)

// errTooDeep refuses a managed tree nested past maxDepth.
var errTooDeep = errors.New("managed storage is nested too deeply")

// fileIdentity names one file across hard links.
type fileIdentity struct {
	// device is the file's device number.
	device uint64

	// inode is the file's inode number.
	inode uint64
}

// diskWalker counts allocated blocks without following symbolic links.
type diskWalker struct {
	// seen holds every directory and file already counted.
	seen map[fileIdentity]bool

	// openRoot opens a child directory; tests replace it to remove a child mid-walk.
	openRoot func(parent *os.Root, name string) (*os.Root, error)
}

// AllocatedBytes counts the disk blocks a directory tree holds.
//
// Symbolic links are never followed and hard links count once. An entry removed
// while the tree is read is skipped, since cleanup runs alongside.
//
// Takes root (string) which is the directory to measure.
//
// Returns uint64 which is the allocated size in bytes.
// Returns error when root is a symbolic link or cannot be read.
func AllocatedBytes(root string) (uint64, error) {
	walker := diskWalker{
		seen: make(map[fileIdentity]bool),
		openRoot: func(parent *os.Root, name string) (*os.Root, error) {
			return parent.OpenRoot(name)
		},
	}
	return walker.measure(root)
}

// measure opens the top directory, refusing a symbolic link, and walks it.
//
// Takes root (string) which is the directory to measure.
//
// Returns uint64 which is the allocated size in bytes.
// Returns error when the tree cannot be read.
func (walker *diskWalker) measure(root string) (uint64, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return 0, fmt.Errorf(readingFormat, root, err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf(readingFormat, root, syscall.ENOTDIR)
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return 0, fmt.Errorf("opening %s: %w", root, err)
	}
	defer func() { _ = directory.Close() }()
	return walker.directory(directory, identityOf(info), 0)
}

// directory counts one directory and everything below it.
//
// Takes directory (*os.Root) which is the open directory.
// Takes expected (fileIdentity) which is the identity the directory had when listed.
// Takes depth (int) which is the nesting below the top directory.
//
// Returns uint64 which is the allocated size in bytes.
// Returns error when the directory cannot be read.
func (walker *diskWalker) directory(directory *os.Root, expected fileIdentity, depth int) (uint64, error) {
	if depth > maxDepth {
		return 0, errTooDeep
	}
	handle, err := directory.Open(".")
	if err != nil {
		return 0, fmt.Errorf("opening %s: %w", directory.Name(), err)
	}
	defer func() { _ = handle.Close() }()
	info, err := handle.Stat()
	if err != nil {
		return 0, fmt.Errorf(readingFormat, directory.Name(), err)
	}
	identity := identityOf(info)

	if identity != expected || walker.seen[identity] {
		return 0, nil
	}
	walker.seen[identity] = true
	total := allocated(info)
	entries, err := handle.ReadDir(-1)
	if err != nil {
		return 0, fmt.Errorf("listing %s: %w", directory.Name(), err)
	}
	for _, entry := range entries {
		size, err := walker.entry(directory, entry.Name(), depth)
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}

// entry counts one directory entry, skipping one that vanished or changed kind.
//
// Takes directory (*os.Root) which holds the entry.
// Takes name (string) which is the entry's name.
// Takes depth (int) which is the directory's nesting.
//
// Returns uint64 which is the allocated size in bytes.
// Returns error when the entry cannot be read for another reason.
func (walker *diskWalker) entry(directory *os.Root, name string, depth int) (uint64, error) {
	info, err := directory.Lstat(name)
	if vanished(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf(readingFormat, name, err)
	}
	identity := identityOf(info)
	if !info.IsDir() {
		if walker.seen[identity] {
			return 0, nil
		}
		walker.seen[identity] = true
		return allocated(info), nil
	}
	child, err := walker.openRoot(directory, name)
	if vanished(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("opening %s: %w", name, err)
	}
	defer func() { _ = child.Close() }()
	return walker.directory(child, identity, depth+1)
}

// vanished reports whether an error means an entry was removed or replaced mid-walk.
//
// Takes err (error) which is the error to classify.
//
// Returns bool which is true for a missing entry or one no longer a directory.
func vanished(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// identityOf returns the device and inode of a file.
//
// Takes info (fs.FileInfo) which is the file's stat result.
//
// Returns fileIdentity which names the file.
func identityOf(info fs.FileInfo) fileIdentity {
	status, isStat := info.Sys().(*syscall.Stat_t)
	if !isStat {
		return fileIdentity{device: 0, inode: 0}
	}
	return fileIdentity{device: uint64(status.Dev), inode: status.Ino}
}

// allocated returns the bytes a file's blocks occupy.
//
// Takes info (fs.FileInfo) which is the file's stat result.
//
// Returns uint64 which is the allocated size in bytes.
func allocated(info fs.FileInfo) uint64 {
	status, isStat := info.Sys().(*syscall.Stat_t)
	if !isStat || status.Blocks < 0 {
		return 0
	}
	return uint64(status.Blocks) * blockSize
}
