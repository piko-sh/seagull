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
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// maxResponseBytes bounds each protocol response.
const maxResponseBytes = 1 << 20

// shutdownTimeout bounds graceful helper exit.
const shutdownTimeout = 5 * time.Second

// programSource is the bounded protocol runner, as embedded.
//
//go:embed program_worker.go
var programSource string

// Program is the bounded protocol runner compiled with generated packages.
var Program = workerSource(programSource)

// Argument records the value and optional name passed to a driver.
type Argument struct {
	// Name is empty for positional arguments.
	Name string

	// Values holds scalar or list elements in order.
	Values []string

	// List distinguishes a list from a scalar value.
	List bool
}

// Statement records one generated driver call.
type Statement struct {
	// SQL is the executed statement.
	SQL string

	// Arguments holds driver arguments in binding order.
	Arguments []Argument
}

// Call records one terminal operation and all its driver calls.
type Call struct {
	// Error is an unexpected generated-method error.
	Error string

	// Statements holds the driver's observations.
	Statements []Statement

	// Values holds scanned scalar values in column order.
	Values []string

	// OpenRows counts result sets left open after the terminal call.
	OpenRows int

	// Step identifies the input operation.
	Step int
}

// PreparedEvent records one prepared statement lifecycle operation.
type PreparedEvent struct {
	// Operation identifies preparation, execution, closure or an injected failure.
	Operation string

	// SQL identifies the statement without exposing connection details.
	SQL string
}

// Response is one complete generated-program response.
type Response struct {
	// Panic describes a panic caught in generated code.
	Panic string

	// PreparationError records a failed attempt to prepare the static query catalogue.
	PreparationError string

	// Prepared holds statement lifecycle events before connection cleanup.
	Prepared []PreparedEvent

	// Calls holds terminal operations in execution order.
	Calls []Call

	// ID matches the request identifier.
	ID uint64
}

// exchangeOutcome carries one round trip's result back to Exchange.
type exchangeOutcome struct {
	// err records a write, read or decode failure.
	err error

	// response holds the decoded reply.
	response Response
}

// Process owns one recording program for a fuzz worker.
type Process struct {
	// command owns the subprocess.
	command *exec.Cmd

	// input supplies bounded JSON requests.
	input io.WriteCloser

	// output reads bounded JSON responses.
	output io.ReadCloser

	// sequence identifies each request.
	sequence uint64
}

// Exchange runs a bounded sequence and rejects missing or mismatched responses.
//
// Takes data ([]byte) which holds at most 32 operations.
//
// Returns Response which belongs to this request.
// Returns error when the helper fails or ignores cancellation.
//
// Not safe for concurrent use. Each worker exchanges requests sequentially.
func (process *Process) Exchange(ctx context.Context, data []byte) (Response, error) {
	if err := context.Cause(ctx); err != nil {
		return Response{}, err
	}
	process.sequence++
	finished := make(chan exchangeOutcome, 1)
	go func() {
		response, err := process.roundTrip(data)
		finished <- exchangeOutcome{response: response, err: err}
	}()
	select {
	case result := <-finished:
		if result.err != nil {
			return Response{}, result.err
		}
		if result.response.ID != process.sequence {
			return Response{}, fmt.Errorf("response ID %d does not match %d", result.response.ID, process.sequence)
		}
		return result.response, nil
	case <-ctx.Done():
		stopErr := process.stop()
		<-finished
		return Response{}, errors.Join(context.Cause(ctx), stopErr)
	}
}

// Close ends the protocol and reaps the helper within five seconds.
//
// Returns error when the helper fails to shut down cleanly.
//
// Not safe for concurrent use. All exchanges must finish before closing.
func (process *Process) Close() error {
	inputErr := ignoreClosed(process.input.Close())
	finished := make(chan error, 1)
	go func() { finished <- process.command.Wait() }()
	select {
	case err := <-finished:
		outputErr := ignoreClosed(process.output.Close())
		if err != nil {
			err = fmt.Errorf("waiting for recording helper: %w", err)
		}
		return errors.Join(inputErr, err, outputErr)
	case <-time.After(shutdownTimeout):
		killErr := process.command.Process.Kill()
		return errors.Join(inputErr, errors.New("recording helper did not stop"), killErr, <-finished)
	}
}

// stop kills the helper and closes its pipes so a blocked round trip returns.
//
// Returns error when the helper cannot be killed or a pipe cannot be closed.
func (process *Process) stop() error {
	killErr := process.command.Process.Kill()
	if errors.Is(killErr, os.ErrProcessDone) {
		killErr = nil
	}
	return errors.Join(killErr, ignoreClosed(process.input.Close()), ignoreClosed(process.output.Close()))
}

// roundTrip writes one request and reads its bounded response.
//
// Takes data ([]byte) which holds the request operations.
//
// Returns Response which is the decoded reply.
// Returns error when the request cannot be written or the reply is malformed or
// oversized.
func (process *Process) roundTrip(data []byte) (Response, error) {
	request := struct {
		Data []byte
		ID   uint64
	}{ID: process.sequence, Data: data}
	if err := json.NewEncoder(process.input).Encode(request); err != nil {
		return Response{}, fmt.Errorf("writing recording helper request: %w", err)
	}
	limited := &io.LimitedReader{R: process.output, N: maxResponseBytes}
	var response Response
	if err := json.NewDecoder(limited).Decode(&response); err != nil {
		if limited.N <= 0 {
			return Response{}, fmt.Errorf("recording helper response exceeds %d bytes: %w", maxResponseBytes, err)
		}
		return Response{}, fmt.Errorf("reading recording helper response: %w", err)
	}
	return response, nil
}

// Start starts a previously compiled recording program.
//
// Takes binary (string) which is the executable path.
// Takes stderr (io.Writer) which receives helper failures.
//
// Returns *Process which must be closed by the worker.
// Returns error when a pipe or process cannot be started.
func Start(binary string, stderr io.Writer) (*Process, error) {
	command := exec.Command(binary)
	command.Stderr = stderr
	input, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening recording helper input: %w", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("opening recording helper output: %w", err), input.Close())
	}
	if err := command.Start(); err != nil {
		return nil, errors.Join(fmt.Errorf("starting recording helper: %w", err), input.Close(), output.Close())
	}
	return &Process{command: command, input: input, output: output, sequence: 0}, nil
}

// ignoreClosed drops the error of closing a pipe that is already closed.
//
// Takes err (error) which is the close result.
//
// Returns error which is nil for an already closed pipe.
func ignoreClosed(err error) error {
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}
