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

package catalogue

import (
	"errors"
	"fmt"
)

const (
	// tableNotFoundFormat describes a table a mutation names that the schemas searched do
	// not hold, given the table's name and the schemas.
	tableNotFoundFormat = "table %s not found in schema %s"
)

var (
	// errDuplicateObject marks a mutation that creates, renames or moves an object, a column
	// or an enum value onto a name one already holds, which the engine refuses.
	errDuplicateObject = errors.New("the object already exists")

	// errUnknownObject marks a mutation that changes an object, a column or an enum value
	// that does not exist, or an object of another kind than the statement names, which the
	// engine refuses.
	errUnknownObject = errors.New("the object does not exist")

	// errMalformedMutation marks a mutation that lacks what its kind needs, such as a
	// partition mutation that names no parent table.
	errMalformedMutation = errors.New("the mutation is incomplete")

	// errPartitionCycle marks a partition attached where it would become a partition of
	// itself, directly or through the tables it partitions.
	errPartitionCycle = errors.New("the partition would partition itself")

	// errAlreadyPartition marks a table attached as a partition while it partitions another
	// table already.
	errAlreadyPartition = errors.New("the table is a partition already")
)

// mutationError is a mutation the catalogue refuses: the sentinel that says why, and the
// message that names the objects involved.
type mutationError struct {
	// reason is the sentinel the error matches.
	reason error

	// message names the objects and says what is wrong.
	message string
}

// Error returns the message, which names the objects involved.
//
// Returns string which is the message.
func (err *mutationError) Error() string {
	return err.message
}

// Unwrap returns the sentinel that says why the mutation was refused.
//
// Returns error which is the sentinel.
func (err *mutationError) Unwrap() error {
	return err.reason
}

// refused returns an error for a mutation the catalogue refuses, matching a sentinel and
// carrying a message that names the objects involved.
//
// Takes reason (error) which is the sentinel the error matches.
// Takes format (string) which is the message format.
// Takes arguments (...any) which fill the format.
//
// Returns error which is the *mutationError.
func refused(reason error, format string, arguments ...any) error {
	return &mutationError{reason: reason, message: fmt.Sprintf(format, arguments...)}
}

// duplicateObject returns an error for a mutation onto a name an object, a column or an
// enum value already holds.
//
// Takes format (string) which is the message format.
// Takes arguments (...any) which fill the format.
//
// Returns error which matches errDuplicateObject.
func duplicateObject(format string, arguments ...any) error {
	return refused(errDuplicateObject, format, arguments...)
}

// unknownObject returns an error for a mutation of an object, a column or an enum value
// that does not exist.
//
// Takes format (string) which is the message format.
// Takes arguments (...any) which fill the format.
//
// Returns error which matches errUnknownObject.
func unknownObject(format string, arguments ...any) error {
	return refused(errUnknownObject, format, arguments...)
}
