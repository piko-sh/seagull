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
	"errors"
	"flag"
	"fmt"
	"io"
)

// commandFlags is a command's flag set together with the synopsis its usage starts with.
// Asking for help prints the usage on standard output and succeeds; a malformed command
// line prints it on standard error and fails.
type commandFlags struct {
	*flag.FlagSet

	// stdout receives the usage when it is asked for.
	stdout io.Writer

	// stderr receives parsing errors and the usage that follows them.
	stderr io.Writer

	// synopsis is the command line printed after "Usage:", starting with the program name.
	synopsis string
}

// parse parses a command's flags, which may come before, between or after its positional
// arguments, and maps the outcome to an exit status.
//
// Takes arguments ([]string) which holds the command's arguments.
//
// Returns []string which holds the positional arguments in order.
// Returns int which is the exit status when parsing ended the command.
// Returns bool which is true when the command should go on.
func (c *commandFlags) parse(arguments []string) ([]string, int, bool) {
	var positional []string
	remaining := arguments
	for {
		err := c.Parse(remaining)
		if errors.Is(err, flag.ErrHelp) {
			c.printUsage(c.stdout)
			return nil, exitOK, false
		}
		if err != nil {
			c.printUsage(c.stderr)
			return nil, exitUsage, false
		}
		rest := c.Args()
		if len(rest) == 0 {
			return positional, exitOK, true
		}
		positional = append(positional, rest[0])
		remaining = rest[1:]
	}
}

// misuse prints the usage on standard error, for a command line that lacks what the
// command needs.
//
// Returns int which is the usage exit status.
func (c *commandFlags) misuse() int {
	c.printUsage(c.stderr)
	return exitUsage
}

// printUsage writes the synopsis and the flags.
//
// Takes writer (io.Writer) which receives the usage.
func (c *commandFlags) printUsage(writer io.Writer) {
	fmt.Fprintf(writer, "Usage: %s\n", c.synopsis)
	c.SetOutput(writer)
	c.PrintDefaults()
	c.SetOutput(c.stderr)
}

// newFlags creates a command's flag set.
//
// Takes name (string) which is the command name, such as "schema dump".
// Takes synopsis (string) which is the command line printed above the flags, without the
// program name in front of it, such as "schema dump [--database name]".
//
// Returns *commandFlags which is ready for flag definitions.
func (inv *invocation) newFlags(name, synopsis string) *commandFlags {
	flags := flag.NewFlagSet(inv.program+" "+name, flag.ContinueOnError)
	flags.SetOutput(inv.streams.Stderr)
	flags.Usage = func() {}
	return &commandFlags{FlagSet: flags, stdout: inv.streams.Stdout, stderr: inv.streams.Stderr, synopsis: inv.program + " " + synopsis}
}

// parseNoArguments parses the command line of a command that takes no arguments and no
// flags of its own, so it still answers -h like every other command.
//
// Takes name (string) which is the command name, such as "config schema".
// Takes arguments ([]string) which holds the command's arguments.
//
// Returns int which is the exit status when the arguments ended the command.
// Returns bool which is true when the command should go on.
func (inv *invocation) parseNoArguments(name string, arguments []string) (int, bool) {
	flags := inv.newFlags(name, name)
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status, false
	}
	if len(positional) > 0 {
		return inv.fail(usageError("%s takes no arguments", name)), false
	}
	return exitOK, true
}

// runGroup handles the arguments of a command whose first argument names a subcommand,
// such as schema or config, before the subcommand runs. With no arguments it prints the
// group's usage on standard error and fails; asked for help, it prints it on standard
// output and succeeds.
//
// Takes arguments ([]string) which holds the subcommand and its arguments.
// Takes usage (string) which is the group's usage, ending in a newline, with %[1]s
// wherever the program name goes.
//
// Returns int which is the exit status when the arguments ended the command.
// Returns bool which is true when the subcommand should run.
func (inv *invocation) runGroup(arguments []string, usage string) (int, bool) {
	switch {
	case len(arguments) == 0:
		fmt.Fprintf(inv.streams.Stderr, usage, inv.program)
		return exitUsage, false
	case isHelpFlag(arguments[0]):
		fmt.Fprintf(inv.streams.Stdout, usage, inv.program)
		return exitOK, false
	default:
		return exitOK, true
	}
}

// isHelpFlag reports whether an argument asks for help, in any spelling the flag package
// accepts.
//
// Takes argument (string) which is one command-line argument.
//
// Returns bool which is true for -h, --h, -help and --help.
func isHelpFlag(argument string) bool {
	switch argument {
	case "-h", "--h", "-help", "--help":
		return true
	default:
		return false
	}
}
