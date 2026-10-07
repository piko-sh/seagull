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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	// maxCommandOutput is the most RunCommand keeps of what a program writes to each of
	// standard output and standard error.
	maxCommandOutput = 16 << 20

	// commandWaitDelay is how long RunCommand waits for a stopped program's output to close,
	// so a child the program started cannot hold the check open.
	commandWaitDelay = 5 * time.Second
)

// CommandRunner runs a program in a directory and returns what it wrote to standard
// output and standard error. arguments[0] names the program and the rest are its
// arguments. An error with an ExitCode() int method, as *exec.ExitError has, means the
// program ran and exited with that status; any other error means it did not run to the
// end.
type CommandRunner func(ctx context.Context, directory string, arguments []string) (stdout, stderr []byte, err error)

// exitCoder is an error that carries the status a program exited with.
type exitCoder interface {
	error

	// ExitCode returns the status the program exited with.
	ExitCode() int
}

// cappedBuffer keeps the first limit bytes written to it and records whether more came.
type cappedBuffer struct {
	// data holds what was kept.
	data bytes.Buffer

	// limit is the most data holds.
	limit int

	// truncated is true when more was written than data keeps.
	truncated bool
}

// Write keeps what fits under the limit and accepts the rest without keeping it, so the
// program writing is neither blocked nor failed.
//
// Takes chunk ([]byte) which is written.
//
// Returns int which is always len(chunk).
// Returns error which is always nil.
func (b *cappedBuffer) Write(chunk []byte) (int, error) {
	kept := chunk
	if room := b.limit - b.data.Len(); len(kept) > room {
		kept = kept[:max(room, 0)]
		b.truncated = true
	}
	_, _ = b.data.Write(kept)
	return len(chunk), nil
}

// RunCommand is the CommandRunner that starts arguments[0] as a process in directory. A
// program named by a relative path, rather than a bare name looked up on PATH, is found
// from the current directory, not from directory. Cancelling the context stops the
// process.
//
// Takes directory (string) which is the directory the program runs in.
// Takes arguments ([]string) which is the program and its arguments.
//
// Returns stdout ([]byte) which is what the program wrote to standard output.
// Returns stderr ([]byte) which is what the program wrote to standard error.
// Returns err (error) which is the *exec.ExitError of a program that exited with a
// non-zero status, the context's cause when it ended the run, ErrOutputTooLarge when the
// program wrote more than RunCommand keeps, ErrNoCommand when arguments is empty, or why
// the program could not start.
func RunCommand(ctx context.Context, directory string, arguments []string) (stdout, stderr []byte, err error) {
	if len(arguments) == 0 {
		return nil, nil, ErrNoCommand
	}
	program := arguments[0]
	if filepath.Base(program) != program && !filepath.IsAbs(program) {
		if program, err = filepath.Abs(program); err != nil {
			return nil, nil, err
		}
	}
	command := exec.CommandContext(ctx, program, arguments[1:]...)
	command.Dir = directory
	command.WaitDelay = commandWaitDelay
	output := &cappedBuffer{limit: maxCommandOutput}
	errorOutput := &cappedBuffer{limit: maxCommandOutput}
	command.Stdout, command.Stderr = output, errorOutput
	runErr := command.Run()
	switch {
	case ctx.Err() != nil:
		runErr = context.Cause(ctx)
	case output.truncated || errorOutput.truncated:
		runErr = fmt.Errorf("%w: more than %d bytes", ErrOutputTooLarge, maxCommandOutput)
	}
	return output.data.Bytes(), errorOutput.data.Bytes(), runErr
}

// exitStatus reports the status a program exited with, when err says it ran to the end.
//
// Takes err (error) which a CommandRunner returned.
//
// Returns status (int) which is the exit status.
// Returns exited (bool) which is false when err does not carry an exit status.
func exitStatus(err error) (status int, exited bool) {
	coder, isCoder := errors.AsType[exitCoder](err)
	if !isCoder {
		return 0, false
	}
	return coder.ExitCode(), true
}
