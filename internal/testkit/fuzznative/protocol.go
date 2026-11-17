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

package fuzznative

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

const (
	// ProtocolVersion identifies the bounded native helper protocol.
	ProtocolVersion = 2

	// protocolBound caps one complete JSON frame.
	protocolBound = 1 << 20

	// helperShutdownBound limits graceful helper shutdown before termination.
	helperShutdownBound = 5 * time.Second
)

var (
	// errHelperClosed reports an exchange after the helper was closed.
	errHelperClosed = errors.New("native helper is closed")

	// errHelperFailed reports an exchange after an earlier one failed or was stopped.
	errHelperFailed = errors.New("native helper failed an earlier exchange")

	// errFrameTooLarge reports a frame beyond the protocol bound.
	errFrameTooLarge = errors.New("native frame exceeds the protocol bound")

	// errTrailingData reports a frame with more than one JSON value.
	errTrailingData = errors.New("native protocol contains trailing data")

	// errWrongEnvelope reports a response to another request or protocol version.
	errWrongEnvelope = errors.New("native helper response has the wrong request ID or protocol version")
)

// Request supplies one independent input and its isolated connection.
type Request struct {
	// Connection is transmitted only through the helper's private stdin pipe.
	Connection string

	// Data holds the versioned independent runtime model.
	Data []byte

	// ID identifies the expected response.
	ID uint64

	// Version rejects incompatible protocol frames.
	Version int
}

// Row preserves an actual generated row and its SQL NULL indicator.
type Row struct {
	// Value retains the exact generated field value.
	Value fuzzcase.TypedValue

	// ID identifies a unique model row and establishes total ordering.
	ID int64

	// Null records the independently selected IS NULL expression.
	Null bool
}

// Response records each required generated operation independently.
type Response struct {
	// Completed records the required stages reached by this input.
	Completed []string

	// Stage identifies an operation that failed before completion.
	Stage string

	// Error describes a failed generated call without connection credentials.
	Error string

	// Panic records a recovered panic as an unconditional test failure.
	Panic string

	// Plain holds the ordinary generated read.
	Plain []Row

	// Stream holds the complete generated iterator read.
	Stream []Row

	// Early holds the prefix returned before stopping an iterator.
	Early []Row

	// Page holds filtered and paginated rows.
	Page []Row

	// Reused holds rows from the same builder after Count.
	Reused []Row

	// Fresh verifies a new builder starts without another builder's filters.
	Fresh []Row

	// BeforeRollback records rows visible before discarding a transaction.
	BeforeRollback []Row

	// AfterRollback records rows after rollback.
	AfterRollback []Row

	// BeforeCommit records rows visible before committing a transaction.
	BeforeCommit []Row

	// AfterCommit records persisted rows after commit.
	AfterCommit []Row

	// AfterCancel records recovery after a cancelled generated call.
	AfterCancel []Row

	// ClosedTransactions counts rejected calls through completed transaction receivers.
	ClosedTransactions int

	// Count is the unpaginated row count after applying the caller's filter.
	Count int64

	// EmptyCount observes an empty runtime IN list.
	EmptyCount int64

	// ID matches the request being answered.
	ID uint64

	// Version identifies the response schema.
	Version int

	// Prepared confirms the factory owns generated prepared statements.
	Prepared bool

	// Cancelled confirms the generated call preserved context cancellation.
	Cancelled bool

	// Native confirms execution through the dedicated driver emitter.
	Native bool
}

// Process owns one native generated-code helper and its bounded protocol pipes.
//
// Each worker uses its process sequentially and closes it before releasing its cache.
type Process struct {
	// command owns the running generated program.
	command *exec.Cmd

	// diagnostics retains only the bounded tail of helper failures.
	diagnostics *diagnosticTail

	// input writes private request frames.
	input io.WriteCloser

	// output reads bounded response frames.
	output io.ReadCloser

	// frames retains buffered response bytes between exchanges.
	frames *bufio.Scanner

	// sequence increases for each attempted exchange.
	sequence uint64

	// closed prevents use after process ownership has ended.
	closed bool

	// failed prevents use after an exchange left the pipes in an unknown state.
	failed bool
}

// helperResponse is a response frame whose envelope the parent checks and redacts.
type helperResponse[R any] interface {
	*R

	// envelope returns the request ID and protocol version the helper answered.
	//
	// Returns uint64 which is the answered request ID.
	// Returns int which is the protocol version.
	envelope() (uint64, int)

	// redact removes the connection string from the helper's failure text.
	//
	// Takes connection (string) which is the private connection string.
	redact(connection string)
}

// envelope returns the answered request ID and protocol version.
//
// Returns uint64 which is the request ID.
// Returns int which is the protocol version.
func (response *Response) envelope() (uint64, int) { return response.ID, response.Version }

// redact removes the connection string from the failure text.
//
// Takes connection (string) which is the private connection string.
func (response *Response) redact(connection string) {
	response.Error = redactConnection(response.Error, connection)
	response.Panic = redactConnection(response.Panic, connection)
}

// Exchange runs one model and validates the complete response envelope.
// A worker must call Exchange and Close sequentially on a process.
//
// Takes connection (string) which names the isolated database.
// Takes data ([]byte) which contains the versioned independent model.
//
// Returns Response which belongs to this exact request.
// Returns error when the helper fails, ignores cancellation or violates the protocol.
func (process *Process) Exchange(ctx context.Context, connection string, data []byte) (Response, error) {
	id := process.next()
	request := Request{Connection: connection, Data: data, ID: id, Version: ProtocolVersion}
	return exchange[Response](ctx, process, request, connection, id, ProtocolVersion)
}

// Close closes the protocol, terminates an unresponsive helper and reaps its process.
//
// Returns error when the helper exits unsuccessfully or cannot be reaped.
//
// Concurrency: The worker calls Close after its last exchange.
func (process *Process) Close() error {
	if process.closed {
		return nil
	}
	process.closed = true
	_ = process.input.Close()
	done := make(chan error, 1)
	go func() { done <- process.command.Wait() }()
	timer := time.NewTimer(helperShutdownBound)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("native helper exit: %w", err)
		}
		return nil
	case <-timer.C:
		_ = process.command.Process.Kill()
		return fmt.Errorf("native helper did not stop within %s: %w", helperShutdownBound, <-done)
	}
}

// next numbers the next exchange.
//
// Returns uint64 which identifies the only acceptable response.
func (process *Process) next() uint64 {
	process.sequence++
	return process.sequence
}

// usable refuses an exchange through a closed or failed helper.
//
// Returns error when the helper can no longer answer requests reliably.
func (process *Process) usable() error {
	switch {
	case process.closed:
		return errHelperClosed
	case process.failed:
		return errHelperFailed
	default:
		return nil
	}
}

// abort kills the helper and closes both pipes, which unblocks a pending exchange.
func (process *Process) abort() {
	_ = process.command.Process.Kill()
	_ = process.input.Close()
	_ = process.output.Close()
}

// roundTrip writes one request frame and reads the next response line.
//
// Takes frame ([]byte) which contains one bounded JSON request and its newline.
//
// Returns []byte which holds the response line until the next read.
// Returns error when the helper exits or the pipe fails.
func (process *Process) roundTrip(frame []byte) ([]byte, error) {
	if _, err := process.input.Write(frame); err != nil {
		return nil, fmt.Errorf("writing native request: %w", err)
	}
	if !process.frames.Scan() {
		return nil, cmp.Or(process.frames.Err(), io.ErrUnexpectedEOF)
	}
	return process.frames.Bytes(), nil
}

// Start launches a compiled native helper without placing connection details in argv.
//
// Takes binary (string) which names a validated cached executable.
//
// Returns *Process which must be closed by its worker.
// Returns error when the helper cannot be started.
func Start(binary string) (*Process, error) {
	command := exec.Command(binary)
	command.Env = append(os.Environ(), "GOMAXPROCS=1")
	return startCommand(command)
}

// exchange writes one request and reads its response while ctx can stop the helper.
//
// A failed or stopped exchange marks the helper unusable, because a later response could
// belong to this request.
//
// Takes process (*Process) which owns the helper.
// Takes request (any) which is the request frame value.
// Takes connection (string) which is redacted from every reported text.
// Takes id (uint64) which identifies the only acceptable response.
// Takes version (int) which is the only acceptable protocol version.
//
// Returns R which belongs to this request.
// Returns error when the helper fails, is stopped or violates the protocol.
func exchange[R any, P helperResponse[R]](ctx context.Context, process *Process, request any, connection string, id uint64, version int) (R, error) {
	var response R
	if err := process.usable(); err != nil {
		return response, err
	}
	if ctx.Err() != nil {
		return response, fmt.Errorf("native exchange not started: %w", context.Cause(ctx))
	}
	frame, err := encodeFrame(request)
	if err != nil {
		return response, err
	}
	stop := context.AfterFunc(ctx, process.abort)
	line, err := process.roundTrip(frame)
	if err == nil {
		err = decodeFrame(line, &response)
	}
	if gotID, gotVersion := P(&response).envelope(); err == nil && (gotID != id || gotVersion != version) {
		err = errWrongEnvelope
	}
	if !stop() {
		process.failed = true
		var zero R
		return zero, fmt.Errorf("native exchange stopped: %w", context.Cause(ctx))
	}
	P(&response).redact(connection)
	if err != nil {
		process.failed = true
		return response, fmt.Errorf("native exchange: %w; helper stderr: %s", err, redactConnection(process.diagnostics.String(), connection))
	}
	return response, nil
}

// startCommand opens protocol pipes and starts one owned executable.
//
// Takes command (*exec.Cmd) which has no connection credentials in its arguments.
//
// Returns *Process which exclusively owns the command and pipes.
// Returns error when a pipe or executable cannot be opened.
func startCommand(command *exec.Cmd) (*Process, error) {
	diagnostics := new(diagnosticTail)
	command.Stderr = diagnostics
	input, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening native helper input: %w", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("opening native helper output: %w", err)
	}
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, fmt.Errorf("starting native helper: %w", err)
	}
	return &Process{command: command, diagnostics: diagnostics, input: input, output: output, frames: newFrameScanner(output), sequence: 0, closed: false, failed: false}, nil
}

// newFrameScanner reads newline-delimited frames up to the protocol bound.
//
// Takes input (io.Reader) which supplies the frames.
//
// Returns *bufio.Scanner which fails on a frame beyond the bound.
func newFrameScanner(input io.Reader) *bufio.Scanner {
	frames := bufio.NewScanner(input)
	frames.Buffer(make([]byte, initialFrameBytes), protocolBound+1)
	return frames
}

// serveFrames answers each request frame with one response frame until input ends.
//
// Takes input (io.Reader) which supplies request frames.
// Takes output (io.Writer) which receives response frames.
// Takes respond (func(*Q) (any, error)) which answers one decoded request.
//
// Returns error when the protocol is invalid or a response cannot be written.
func serveFrames[Q any](input io.Reader, output io.Writer, respond func(*Q) (any, error)) error {
	frames := newFrameScanner(input)
	for frames.Scan() {
		var request Q
		if err := decodeFrame(frames.Bytes(), &request); err != nil {
			return err
		}
		response, err := respond(&request)
		if err != nil {
			return err
		}
		encoded, err := encodeFrame(response)
		if err != nil {
			return err
		}
		if _, err := output.Write(encoded); err != nil {
			return fmt.Errorf("writing native response: %w", err)
		}
	}
	if err := frames.Err(); err != nil {
		return fmt.Errorf("reading native request: %w", err)
	}
	return nil
}

// encodeFrame writes one value as a bounded protocol line.
//
// Takes value (any) which is a request or response.
//
// Returns []byte which holds the JSON frame and its newline.
// Returns error when the value cannot be encoded or exceeds the protocol bound.
func encodeFrame(value any) ([]byte, error) {
	frame, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding native frame: %w", err)
	}
	if len(frame) > protocolBound {
		return nil, errFrameTooLarge
	}
	return append(frame, '\n'), nil
}

// decodeFrame rejects unknown fields, oversized frames and trailing JSON values.
//
// Takes frame ([]byte) which holds one complete protocol line.
// Takes destination (any) which points to a request or response value.
//
// Returns error when the frame does not have the exact expected schema.
func decodeFrame(frame []byte, destination any) error {
	if len(frame) > protocolBound {
		return errFrameTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("native protocol: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errTrailingData
	}
	return nil
}

// redactConnection removes connection material from diagnostic fields.
//
// Takes text (string) which may contain a driver diagnostic.
// Takes connection (string) which is the private connection string.
//
// Returns string which omits the original connection string.
func redactConnection(text, connection string) string {
	if connection == "" {
		return text
	}
	return strings.ReplaceAll(text, connection, "[connection]")
}
