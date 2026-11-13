// Command island-battles-package zips a Go bot for the server's runners: one
// zip with the bot built for Linux arm64 and a manifest, that the runner
// Lambdas run without installing anything.
//
// Run it in the bot's module:
//
//	go run github.com/INTODAN/island-battles-go/clients/island-battles-go/cmd/island-battles-package [--output dist/island-battles-bot.zip] [package path]
//
// The package is the bot program's main package, "." by default. It must call
// islandbattles.Main, which gives the program the worker subcommand the
// runner starts. The zip holds island-battles-bot.json, the manifest
//
//	{"format": 1, "language": "native", "command": ["./bot", "worker"]}
//
// and the executable bot, built with GOOS=linux GOARCH=arm64 CGO_ENABLED=0.
// The zip may be at most 250 MiB and 1 GiB unpacked; over either, the command
// names the largest files.
//
// Its upload subcommand uploads the zip to a lobby, with the upload token that
// the bot picker's "Upload from the command line" shows:
//
//	go run github.com/INTODAN/island-battles-go/clients/island-battles-go/cmd/island-battles-package upload --game-url <url> --token=<token> [zip]
package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// sdkPath is the import path of the SDK, whose Main the bot calls.
	sdkPath = "github.com/INTODAN/island-battles-go/clients/island-battles-go"
	// manifestName is the manifest's name at the root of the zip.
	manifestName = "island-battles-bot.json"
	// binaryName is the executable's name in the zip.
	binaryName = "bot"
	// defaultOutput is where the zip is written by default.
	defaultOutput = "dist/island-battles-bot.zip"
	// maxZipBytes and maxUnpackedBytes are the size limits.
	maxZipBytes      = 250 * 1024 * 1024
	maxUnpackedBytes = 1024 * 1024 * 1024
	largestFiles     = 5
)

// manifest is the native manifest of a bot.
var manifest = []byte("{\n  \"format\": 1,\n  \"language\": \"native\",\n  \"command\": [\"./bot\", \"worker\"]\n}\n")

// zipTime is the time of every file in the zip, so that the same bot makes
// the same zip.
var zipTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// entry is a file of the zip.
type entry struct {
	name string
	mode os.FileMode
	data io.Reader
	size int64
}

func run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) > 0 && arguments[0] == "upload" {
		return runUpload(arguments[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("island-battles-package", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", defaultOutput, "the zip to write")
	flags.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: island-battles-package [--output file] [package path]\n\nBuilds the bot program for Linux arm64 and zips it with its manifest for the\nserver's runners. The package is the bot's main package, \".\" by default.\n\n       island-battles-package upload --game-url <url> --token=<token> [zip]\n\nUploads the zip to a lobby; see island-battles-package upload --help.\n\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 1 {
		_, _ = fmt.Fprintf(stderr, "unexpected arguments: %v\n", flags.Args()[1:])
		flags.Usage()
		return 2
	}
	pattern := "."
	if flags.NArg() == 1 {
		pattern = flags.Arg(0)
	}
	zipped, unpacked, err := build(pattern, *output, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s: %s zipped, %s unpacked, Linux arm64\n", *output, mebibytes(zipped), mebibytes(unpacked))
	return 0
}

func mebibytes(size int64) string {
	return strconv.FormatFloat(float64(size)/(1024*1024), 'f', 1, 64) + " MiB"
}

// build builds the package for Linux arm64 and writes the zip.
func build(pattern, output string, stderr io.Writer) (zipped, unpacked int64, err error) {
	if err := checkProgram(pattern); err != nil {
		return 0, 0, err
	}
	directory, err := os.MkdirTemp("", "island-battles-package-")
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	binary := filepath.Join(directory, binaryName)
	command := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", binary, pattern)
	command.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	command.Stdout, command.Stderr = stderr, stderr
	if err := command.Run(); err != nil {
		return 0, 0, fmt.Errorf("go build for linux/arm64 failed: %w", err)
	}
	info, err := os.Stat(binary)
	if err != nil {
		return 0, 0, err
	}
	file, err := os.Open(binary)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = file.Close() }()
	return writeZip(output, []entry{
		{name: manifestName, mode: 0o644, data: bytes.NewReader(manifest), size: int64(len(manifest))},
		{name: binaryName, mode: 0o755, data: file, size: info.Size()},
	})
}

// checkProgram reports a package that is not a main package or doesn't call
// islandbattles.Main.
func checkProgram(pattern string) error {
	command := exec.Command("go", "list", "-f", `{{.Name}}{{"\n"}}{{.Dir}}{{"\n"}}{{join .GoFiles "\n"}}`, pattern)
	listed, err := command.Output()
	if err != nil {
		var failure *exec.ExitError
		if errors.As(err, &failure) {
			return fmt.Errorf("could not find the package %s:\n%s", pattern, strings.TrimSpace(string(failure.Stderr)))
		}
		return fmt.Errorf("could not run go: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(listed)), "\n")
	if len(lines) < 3 {
		return fmt.Errorf("the package %s has no Go files", pattern)
	}
	if lines[0] != "main" {
		return fmt.Errorf("the package %s is package %s; the bot program must be a main package", pattern, lines[0])
	}
	for _, name := range lines[2:] {
		found, err := callsMain(filepath.Join(lines[1], name))
		if err != nil {
			return err
		}
		if found {
			return nil
		}
	}
	return fmt.Errorf("the package %s never calls islandbattles.Main (%s). The runner starts the bot with `bot worker`, so main must be:\n\n\tfunc main() { islandbattles.Main(play) }", pattern, sdkPath)
}

// callsMain reports whether the file calls Main of the SDK.
func callsMain(path string) (bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return false, fmt.Errorf("could not read %s: %w", path, err)
	}
	names := map[string]bool{}
	for _, spec := range file.Imports {
		importPath, _ := strconv.Unquote(spec.Path.Value)
		if importPath != sdkPath {
			continue
		}
		name := "islandbattles"
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[name] = true
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Main" {
			if pkg, ok := selector.X.(*ast.Ident); ok && names[pkg.Name] {
				found = true
			}
		}
		return !found
	})
	return found, nil
}

// writeZip writes the entries to output, after checking the unpacked size.
func writeZip(output string, entries []entry) (zipped, unpacked int64, err error) {
	var total int64
	for _, item := range entries {
		total += item.size
	}
	if total > maxUnpackedBytes {
		return 0, 0, sizeError("the package is %s unpacked; the limit is %s", total, maxUnpackedBytes, entries)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o750); err != nil {
		return 0, 0, err
	}
	temporary := output + ".partial"
	defer func() { _ = os.Remove(temporary) }()
	file, err := os.Create(temporary) //nolint:gosec // the player's chosen output file
	if err != nil {
		return 0, 0, err
	}
	archive := zip.NewWriter(file)
	for _, item := range entries {
		header := &zip.FileHeader{Name: item.name, Method: zip.Deflate, Modified: zipTime}
		header.SetMode(item.mode)
		writer, err := archive.CreateHeader(header)
		if err == nil {
			_, err = io.Copy(writer, item.data)
		}
		if err != nil {
			_ = file.Close()
			return 0, 0, err
		}
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		return 0, 0, err
	}
	if err := file.Close(); err != nil {
		return 0, 0, err
	}
	info, err := os.Stat(temporary)
	if err != nil {
		return 0, 0, err
	}
	if info.Size() > maxZipBytes {
		return 0, 0, sizeError("the zip is %s; the limit is %s", info.Size(), maxZipBytes, entries)
	}
	if err := os.Rename(temporary, output); err != nil {
		return 0, 0, err
	}
	return info.Size(), total, nil
}

// sizeError says a limit was passed and names the largest files.
func sizeError(format string, size, limit int64, entries []entry) error {
	sorted := append([]entry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].size != sorted[j].size {
			return sorted[i].size > sorted[j].size
		}
		return sorted[i].name < sorted[j].name
	})
	var message strings.Builder
	_, _ = fmt.Fprintf(&message, format+". The largest files:", mebibytes(size), mebibytes(limit))
	for index, item := range sorted {
		if index == largestFiles {
			break
		}
		_, _ = fmt.Fprintf(&message, "\n  %10s  %s", mebibytes(item.size), item.name)
	}
	return errors.New(message.String())
}
