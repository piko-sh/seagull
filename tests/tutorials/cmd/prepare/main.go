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

package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"piko.sh/safedisk"
)

const (
	// exitOK is the status of a run that prepared the workspace.
	exitOK = 0

	// exitFailure is the status of a run that could not prepare the workspace.
	exitFailure = 1

	// exitUsage is the status of a run with unusable arguments.
	exitUsage = 2

	// directoryPermissions is the mode of the directories prepare creates.
	directoryPermissions = 0o755

	// filePermissions is the mode of the files prepare writes.
	filePermissions = 0o644

	// executablePermissions is the mode of the sgl executable in the workspace.
	executablePermissions = 0o755

	// tutorialsDirectory is the tutorial module's directory within the checkout.
	tutorialsDirectory = "tests/tutorials"

	// sharedDirectory holds the tutorial module's packages every lesson imports.
	sharedDirectory = "internal"

	// firstLesson is the lesson directory the reader starts in.
	firstLesson = "01_first_query"

	// mainFile is the lesson file whose run function is emptied.
	mainFile = "main.go"

	// runSignature opens the run function the reader fills in.
	runSignature = "\nfunc run("

	// bodyOpening ends the run function's signature and opens its body.
	bodyOpening = " {\n"

	// emptyBody is the body the run function is left with, closing brace included.
	emptyBody = "\treturn nil\n}\n"

	// generatorName is the name of the sgl executable in the workspace.
	generatorName = "sgl"

	// generatorPackage is the package sgl is built from, relative to the checkout.
	generatorPackage = "./cmd/sgl"

	// workspaceName is the name of a Go workspace file.
	workspaceName = "go.work"

	// seagullModule is the module path of the Seagull checkout.
	seagullModule = "seagull.sh/seagull"

	// defaultGoCommand is the go command run when the GO environment variable is unset.
	defaultGoCommand = "go"

	// goCommandVariable is the environment variable naming the go command.
	goCommandVariable = "GO"

	// workspaceVariable is the environment variable naming the go.work file to use.
	workspaceVariable = "GOWORK"
)

var (
	// errUsage reports arguments that do not name exactly one destination.
	errUsage = errors.New("usage: prepare [-sgl FILE] DIRECTORY")

	// errNoCheckout reports a working directory outside any Seagull checkout.
	errNoCheckout = errors.New("run prepare from inside a Seagull checkout")

	// errExistingDestination reports a destination that already exists.
	errExistingDestination = errors.New("choose a new practice directory")

	// errInsideCheckout reports a destination inside the Seagull checkout.
	errInsideCheckout = errors.New("practice workspaces must be outside the Seagull checkout")

	// errNoRunFunction reports a lesson main.go without a run function to empty.
	errNoRunFunction = errors.New("no run function to empty")

	// checkoutMarkers are the paths whose presence marks a Seagull checkout's root.
	checkoutMarkers = []string{workspaceName, tutorialsDirectory + "/go.mod"}

	// moduleFiles are the tutorial module files copied into the workspace.
	moduleFiles = []string{"go.mod", "go.sum"}

	// lessons are the lesson directories the workspace supplies.
	lessons = []lesson{
		{name: firstLesson, files: []string{mainFile}},
		{name: "07_postgres", files: []string{mainFile, "main_test.go", "output.txt"}},
		{name: "08_embedding", files: []string{mainFile, "main_test.go"}},
	}

	// databaseDirectories are the SQL directories each lesson starts with, empty.
	databaseDirectories = []string{"db/migrations", "db/queries", "db/seeds"}
)

// options holds the parsed command line.
type options struct {
	// destination is the new directory the workspace is written to.
	destination string

	// generator is an sgl executable to copy instead of building one, or empty.
	generator string
}

// workspace describes the practice workspace to prepare.
type workspace struct {
	// checkout is the resolved root of the Seagull checkout.
	checkout string

	// destination is the resolved directory the workspace is written to.
	destination string

	// generator is an sgl executable to copy instead of building one, or empty.
	generator string

	// goCommand is the go command that edits the module files and builds sgl.
	goCommand string
}

// newWorkspace locates the checkout around the working directory and resolves the
// destination the options name.
//
// Takes chosen (options) which holds the destination and the optional generator.
//
// Returns workspace which is ready to prepare.
// Returns error when the working directory is outside a Seagull checkout or a path cannot
// be resolved.
func newWorkspace(chosen options) (workspace, error) {
	working, err := os.Getwd()
	if err != nil {
		return workspace{}, fmt.Errorf("reading the working directory: %w", err)
	}
	checkout, err := findCheckout(working)
	if err != nil {
		return workspace{}, err
	}
	destination, err := resolvePath(chosen.destination)
	if err != nil {
		return workspace{}, err
	}
	return workspace{
		checkout:    checkout,
		destination: destination,
		generator:   chosen.generator,
		goCommand:   cmp.Or(os.Getenv(goCommandVariable), defaultGoCommand),
	}, nil
}

// prepare creates the workspace: the module files, the lessons, the module and workspace
// wiring, and the sgl executable.
//
// Returns error when the destination exists or lies inside the checkout, or when a file
// cannot be copied or a go command fails.
func (practice workspace) prepare(ctx context.Context) (err error) {
	_, statErr := os.Lstat(practice.destination)
	if statErr == nil {
		return fmt.Errorf("%w: %s", errExistingDestination, practice.destination)
	}
	if !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("checking the practice directory: %w", statErr)
	}
	if isWithin(practice.checkout, practice.destination) {
		return errInsideCheckout
	}
	source, err := safedisk.NewSandbox(filepath.Join(practice.checkout, tutorialsDirectory), safedisk.ModeReadOnly)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	target, err := safedisk.NewSandbox(practice.destination, safedisk.ModeReadWrite)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, target.Close()) }()
	if err := target.Chmod(".", directoryPermissions); err != nil {
		return err
	}
	if err := copyModule(source, target); err != nil {
		return err
	}
	for _, supplied := range lessons {
		if err := supplied.prepare(source, target); err != nil {
			return err
		}
	}
	if err := practice.linkModules(ctx); err != nil {
		return err
	}
	return practice.installGenerator(ctx, target)
}

// linkModules points the copied go.mod at the checkout, and creates a go.work using both
// the checkout and the workspace.
//
// Returns error when a go command fails.
func (practice workspace) linkModules(ctx context.Context) error {
	if err := practice.runGo(ctx, practice.destination, "", "mod", "edit", replacement(seagullModule, practice.checkout)); err != nil {
		return err
	}
	workspaceFile := filepath.Join(practice.destination, workspaceName)
	return practice.runGo(ctx, practice.destination, workspaceFile, "work", "init", practice.checkout, practice.destination)
}

// installGenerator builds sgl from the checkout into the workspace, or copies the
// executable the options named.
//
// Takes target (safedisk.Sandbox) which is the workspace.
//
// Returns error when the build fails or the executable cannot be copied.
func (practice workspace) installGenerator(ctx context.Context, target safedisk.Sandbox) error {
	if practice.generator == "" {
		workspaceFile := filepath.Join(practice.checkout, workspaceName)
		output := filepath.Join(practice.destination, generatorName)
		return practice.runGo(ctx, practice.checkout, workspaceFile, "build", "-o", output, generatorPackage)
	}
	return copyExecutable(practice.generator, target)
}

// runGo runs a go command, reporting its output when it fails.
//
// Takes directory (string) which is where the command runs.
// Takes workspaceFile (string) which is the go.work file the command uses, or empty to
// keep the inherited setting.
// Takes arguments (...string) which are the go command's arguments.
//
// Returns error when the command fails, carrying its output.
func (practice workspace) runGo(ctx context.Context, directory, workspaceFile string, arguments ...string) error {
	command := goCommandContext(ctx, practice.goCommand, arguments)
	command.Dir = directory
	command.Env = os.Environ()
	if workspaceFile != "" {
		command.Env = append(command.Env, workspaceVariable+"="+workspaceFile)
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s failed: %w\n%s", strings.Join(command.Args, " "), err, output.String())
	}
	return nil
}

// lesson names a lesson directory and the files supplied with it.
type lesson struct {
	// name is the lesson's directory within the tutorial module.
	name string

	// files are the lesson files copied when the tutorial module has them.
	files []string
}

// prepare creates the lesson directory with its supplied files, an emptied run function
// and empty SQL directories.
//
// Takes source (safedisk.Sandbox) which is the tutorial module.
// Takes target (safedisk.Sandbox) which is the workspace.
//
// Returns error when a file cannot be copied or main.go has no run function.
func (supplied lesson) prepare(source, target safedisk.Sandbox) error {
	if err := target.Mkdir(supplied.name, directoryPermissions); err != nil {
		return err
	}
	for _, name := range supplied.files {
		path := filepath.Join(supplied.name, name)
		_, err := source.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := copyFile(source, target, path, filePermissions); err != nil {
			return err
		}
	}
	program := filepath.Join(supplied.name, mainFile)
	text, err := target.ReadFile(program)
	if err != nil {
		return err
	}
	emptied, err := emptyRunFunction(string(text))
	if err != nil {
		return fmt.Errorf("%s: %w", program, err)
	}
	if err := target.WriteFile(program, []byte(emptied), filePermissions); err != nil {
		return err
	}
	for _, directory := range databaseDirectories {
		if err := target.MkdirAll(filepath.Join(supplied.name, directory), directoryPermissions); err != nil {
			return err
		}
	}
	return nil
}

// main prepares the workspace the arguments name and exits with the run's status.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	status := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(status)
}

// run parses the arguments and prepares the workspace they name.
//
// Takes arguments ([]string) which are the command-line arguments.
// Takes stdout (io.Writer) which receives where the workspace is and where to start.
// Takes stderr (io.Writer) which receives usage text and errors.
//
// Returns int which is the exit status.
func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	chosen, err := parseOptions(arguments, stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stderr, "prepare:", err)
		}
		return exitUsage
	}
	target, err := newWorkspace(chosen)
	if err == nil {
		err = target.prepare(ctx)
	}
	if err != nil {
		fmt.Fprintln(stderr, "prepare:", err)
		return exitFailure
	}
	fmt.Fprintf(stdout, "Practice workspace: %s\nStart in %s\n", target.destination, filepath.Join(target.destination, firstLesson))
	return exitOK
}

// parseOptions parses the flags and the one destination argument.
//
// Takes arguments ([]string) which are the command-line arguments.
// Takes stderr (io.Writer) which receives the usage text.
//
// Returns options which holds the destination and the optional generator.
// Returns error when the flags cannot be parsed or the arguments do not name exactly one
// destination.
func parseOptions(arguments []string, stderr io.Writer) (options, error) {
	chosen := options{destination: "", generator: ""}
	flags := flag.NewFlagSet("prepare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&chosen.generator, "sgl", "", "copy the sgl executable at `FILE` instead of building one")
	if err := flags.Parse(arguments); err != nil {
		return chosen, err
	}
	if flags.NArg() != 1 {
		return chosen, errUsage
	}
	chosen.destination = flags.Arg(0)
	return chosen, nil
}

// findCheckout finds the Seagull checkout holding a directory.
//
// Takes start (string) which is the directory the search starts from.
//
// Returns string which is the checkout's resolved root.
// Returns error when no directory from start upwards is a checkout's root.
func findCheckout(start string) (string, error) {
	directory, err := resolvePath(start)
	if err != nil {
		return "", err
	}
	for !isCheckout(directory) {
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errNoCheckout
		}
		directory = parent
	}
	return directory, nil
}

// isCheckout reports whether a directory is a Seagull checkout's root.
//
// Takes directory (string) which is the directory to check.
//
// Returns bool which is true when every checkout marker exists in the directory.
func isCheckout(directory string) bool {
	for _, marker := range checkoutMarkers {
		if _, err := os.Stat(filepath.Join(directory, filepath.FromSlash(marker))); err != nil {
			return false
		}
	}
	return true
}

// resolvePath makes a path absolute and resolves the symbolic links of the part of it
// that exists, keeping the missing rest as written.
//
// Takes path (string) which is the path to resolve.
//
// Returns string which is the resolved, cleaned absolute path.
// Returns error when the path cannot be made absolute or a link cannot be read.
func resolvePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", path, err)
	}
	existing, missing := absolute, ""
	for {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			return filepath.Join(resolved, missing), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolving %s: %w", path, err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return absolute, nil
		}
		missing = filepath.Join(filepath.Base(existing), missing)
		existing = parent
	}
}

// isWithin reports whether a path is a directory or lies inside it.
//
// Takes parent (string) which is the resolved directory.
// Takes path (string) which is the resolved path to check.
//
// Returns bool which is true when path is parent or lies beneath it.
func isWithin(parent, path string) bool {
	relative, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// emptyRunFunction replaces the body of a lesson's run function with a bare return.
//
// Takes text (string) which is the lesson's main.go.
//
// Returns string which is main.go with everything after run's opening brace replaced.
// Returns error when main.go has no run function with a body.
func emptyRunFunction(text string) (string, error) {
	start := strings.Index(text, runSignature)
	if start < 0 {
		return "", errNoRunFunction
	}
	opening := strings.Index(text[start:], bodyOpening)
	if opening < 0 {
		return "", errNoRunFunction
	}
	body := start + opening + len(bodyOpening)
	return text[:body] + emptyBody, nil
}

// goCommandContext builds a go command bound to a context.
//
// Takes goCommand (string) which names the go command, found on PATH unless it is a path.
// Takes arguments ([]string) which are the go command's arguments.
//
// Returns *exec.Cmd which is ready to configure and run.
func goCommandContext(ctx context.Context, goCommand string, arguments []string) *exec.Cmd {
	command := exec.CommandContext(ctx, goCommand)
	command.Args = append(command.Args, arguments...)
	return command
}

// replacement builds a go mod edit or go work edit flag replacing a module with a
// directory.
//
// Takes module (string) which is the module path, with an optional @version.
// Takes directory (string) which is the module's directory.
//
// Returns string which is the -replace flag.
func replacement(module, directory string) string {
	return "-replace=" + module + "=" + directory
}

// copyModule copies the tutorial module's go.mod, go.sum and shared packages.
//
// Takes source (safedisk.Sandbox) which is the tutorial module.
// Takes target (safedisk.Sandbox) which is the workspace.
//
// Returns error when a file cannot be copied.
func copyModule(source, target safedisk.Sandbox) error {
	for _, name := range moduleFiles {
		if err := copyFile(source, target, name, filePermissions); err != nil {
			return err
		}
	}
	return source.WalkDir(sharedDirectory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return target.MkdirAll(path, info.Mode().Perm())
		}
		return copyFile(source, target, path, info.Mode().Perm())
	})
}

// copyFile copies one file between sandboxes at the same relative path.
//
// Takes source (safedisk.Sandbox) which holds the file.
// Takes target (safedisk.Sandbox) which receives it.
// Takes path (string) which is the file's path in both.
// Takes permissions (fs.FileMode) which is the copy's mode.
//
// Returns error when the file cannot be read or written.
func copyFile(source, target safedisk.Sandbox, path string, permissions fs.FileMode) error {
	data, err := source.ReadFile(path)
	if err != nil {
		return err
	}
	return target.WriteFile(path, data, permissions)
}

// copyExecutable copies an sgl executable into the workspace.
//
// Takes generator (string) which is the executable's path.
// Takes target (safedisk.Sandbox) which is the workspace.
//
// Returns error when the executable cannot be read, written or made executable.
func copyExecutable(generator string, target safedisk.Sandbox) (err error) {
	source, err := safedisk.NewSandbox(filepath.Dir(generator), safedisk.ModeReadOnly)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	data, err := source.ReadFile(filepath.Base(generator))
	if err != nil {
		return err
	}
	if err := target.WriteFile(generatorName, data, executablePermissions); err != nil {
		return err
	}
	return target.Chmod(generatorName, executablePermissions)
}
