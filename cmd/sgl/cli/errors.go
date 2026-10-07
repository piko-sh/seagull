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

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
)

const (
	// signalExitBase is added to a signal's number to give the exit status of a process that
	// signal ended, as a shell reports it.
	signalExitBase = 128
)

var (
	// errInvalidOption marks an Option given a value it cannot use, such as a nil factory or
	// an empty name.
	errInvalidOption = errors.New("invalid option")

	// errNotRegularFile marks a path sgl was asked to read that is not a regular file, such
	// as a directory, a device or a named pipe.
	errNotRegularFile = errors.New("not a regular file")

	// errFileTooLarge marks a file larger than sgl reads.
	errFileTooLarge = errors.New("file is too large")

	// errTooManyEntries marks a directory holding more entries than sgl lists.
	errTooManyEntries = errors.New("directory holds too many entries")
)

// interruptError is the cause a run is cancelled with when the process receives a signal.
// It matches context.Canceled, so every check for a cancelled run holds.
type interruptError struct {
	// signal is the signal received.
	signal os.Signal
}

// Error names the signal.
//
// Returns string which reads like "interrupt received" or "SIGTERM received".
func (e *interruptError) Error() string {
	switch e.signal {
	case os.Interrupt:
		return "interrupt received"
	case syscall.SIGTERM:
		return "SIGTERM received"
	default:
		return fmt.Sprintf("signal %v received", e.signal)
	}
}

// Unwrap returns context.Canceled, the error a cancelled context reports.
//
// Returns error which is context.Canceled.
func (*interruptError) Unwrap() error {
	return context.Canceled
}

// exitStatus is the status a process the signal ended exits with: 128 plus the signal's
// number, such as 130 for SIGINT and 143 for SIGTERM.
//
// Returns int which is the status, or exitInterrupted for a signal with no number.
func (e *interruptError) exitStatus() int {
	number, numbered := e.signal.(syscall.Signal)
	if !numbered {
		return exitInterrupted
	}
	return signalExitBase + int(number)
}
