// Package botpackage checks a bot package: the zip that the SDKs' package
// command builds, with the manifest island-battles-bot.json at its root
// (clients/island-battles-python/island_battles_client/bundle.py and
// clients/island-battles-go/cmd/island-battles-package). It reads only the
// zip's central directory and the manifest, so a package stored in S3 is
// checked with a few range requests instead of a download.
package botpackage

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"unicode"
)

const (
	// ManifestName is the manifest's name at the root of the zip.
	ManifestName = "island-battles-bot.json"
	// MaxZipBytes and MaxUnpackedBytes are the size limits, in binary units,
	// exactly as the SDKs' package commands enforce them: 250 MiB zipped
	// and 1 GiB unpacked.
	MaxZipBytes      = 250 * 1024 * 1024
	MaxUnpackedBytes = 1024 * 1024 * 1024
	// PythonVersion is the Python that the runners run.
	PythonVersion = "3.14"
	// Python and Native are the manifest's languages.
	Python = "python"
	Native = "native"
	// maxManifestBytes bounds the manifest that is read; a real one is under
	// 200 bytes.
	maxManifestBytes = 64 * 1024
)

// Invalid is a reason a package is refused, written for the person who
// uploaded it.
type Invalid struct{ Reason string }

func (invalid *Invalid) Error() string { return invalid.Reason }

func invalid(format string, arguments ...any) error {
	return &Invalid{Reason: fmt.Sprintf(format, arguments...)}
}

// Package is what a valid package holds.
type Package struct {
	// Language is Python or Native.
	Language string
	// EntryOrCommand is a Python package's module:function entry, or a
	// native package's command, its arguments joined with spaces.
	EntryOrCommand string
	// UnpackedBytes is the size of every file, unpacked.
	UnpackedBytes int64
}

// manifest is the manifest's format 1. Fields are pointers so that a
// missing one is told apart from an empty one.
type manifest struct {
	Format   *int     `json:"format"`
	Language *string  `json:"language"`
	Python   *string  `json:"python"`
	Entry    *string  `json:"entry"`
	Command  []string `json:"command"`
}

// Inspect checks the zip of size bytes that reader reads: every path stays
// inside the package (no .., absolute paths or symbolic links), the files
// come to at most MaxUnpackedBytes unpacked, and the manifest is format 1,
// either python, with python "3.14" and a module:function entry, or native,
// with a command whose program is a file in the zip. A package that fails
// a check is an *Invalid error; any other error is the reader's.
func Inspect(reader io.ReaderAt, size int64) (Package, error) {
	if size > MaxZipBytes {
		return Package{}, invalid("the zip is %s; the limit is %s", mebibytes(size), mebibytes(MaxZipBytes))
	}
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		var readError *ReadError
		if errors.As(err, &readError) {
			return Package{}, err
		}
		return Package{}, invalid("the file is not a zip archive (%v); upload the zip that the SDK's package command writes", err)
	}
	files := map[string]*zip.File{}
	var unpacked uint64
	for _, file := range archive.File {
		name := strings.TrimSuffix(file.Name, "/")
		if err := checkPath(file.Name, name); err != nil {
			return Package{}, err
		}
		mode := file.Mode()
		switch {
		case mode&fs.ModeSymlink != 0:
			return Package{}, invalid("%s is a symbolic link; a package may hold only files and folders", file.Name)
		case mode.Type()&^fs.ModeDir != 0:
			return Package{}, invalid("%s is not a regular file; a package may hold only files and folders", file.Name)
		}
		unpacked += file.UncompressedSize64
		if unpacked > MaxUnpackedBytes {
			return Package{}, invalid("the package is more than %s unpacked, the limit", mebibytes(MaxUnpackedBytes))
		}
		if !mode.IsDir() {
			files[name] = file
		}
	}
	manifestFile := files[ManifestName]
	if manifestFile == nil {
		return Package{}, invalid("the zip has no %s at its root; build it with the SDK's package command", ManifestName)
	}
	read, err := readManifest(manifestFile)
	if err != nil {
		return Package{}, err
	}
	built, err := check(read, files)
	if err != nil {
		return Package{}, err
	}
	built.UnpackedBytes = int64(unpacked)
	return built, nil
}

// checkPath refuses a name that leaves the package's folder when unpacked.
func checkPath(original, name string) error {
	switch {
	case name == "":
		return invalid("the zip has a file without a name")
	case strings.Contains(name, `\`):
		return invalid("%s has a backslash; a package's paths use /", original)
	case strings.HasPrefix(name, "/") || (len(name) > 1 && name[1] == ':'):
		return invalid("%s is an absolute path; every path must be inside the package", original)
	}
	for _, element := range strings.Split(name, "/") {
		if element == ".." {
			return invalid("%s goes outside the package with ..; every path must be inside it", original)
		}
	}
	if !fs.ValidPath(name) {
		return invalid("%s is not a plain relative path (it has an empty or . part)", original)
	}
	return nil
}

func readManifest(file *zip.File) (manifest, error) {
	if file.UncompressedSize64 > maxManifestBytes {
		return manifest{}, invalid("%s is %d bytes; a manifest is a few lines of JSON", ManifestName, file.UncompressedSize64)
	}
	opened, err := file.Open()
	if err != nil {
		return manifest{}, readFailure(err)
	}
	defer func() { _ = opened.Close() }()
	data, err := io.ReadAll(io.LimitReader(opened, maxManifestBytes+1))
	if err != nil {
		return manifest{}, readFailure(err)
	}
	var read manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&read); err != nil {
		return manifest{}, invalid("%s is not a valid manifest: %v", ManifestName, err)
	}
	return read, nil
}

// readFailure keeps the reader's own errors, and calls anything else, such
// as a checksum or a broken compressed stream, a damaged zip.
func readFailure(err error) error {
	var readError *ReadError
	if errors.As(err, &readError) {
		return err
	}
	return invalid("%s can't be read: %v; the zip is damaged", ManifestName, err)
}

func check(read manifest, files map[string]*zip.File) (Package, error) {
	if read.Format == nil || *read.Format != 1 {
		return Package{}, invalid(`%s must have "format": 1`, ManifestName)
	}
	if read.Language == nil {
		return Package{}, invalid(`%s must have "language": "python" or "native"`, ManifestName)
	}
	switch *read.Language {
	case Python:
		if read.Command != nil {
			return Package{}, invalid(`a python manifest has an "entry", not a "command"`)
		}
		if read.Python == nil || *read.Python != PythonVersion {
			return Package{}, invalid(`a python manifest must have "python": %q, the runners' Python`, PythonVersion)
		}
		if read.Entry == nil || !validEntry(*read.Entry) {
			entry := ""
			if read.Entry != nil {
				entry = " " + strconv.Quote(*read.Entry)
			}
			return Package{}, invalid(`the entry%s must be module:function, such as "bot:play"`, entry)
		}
		return Package{Language: Python, EntryOrCommand: *read.Entry}, nil
	case Native:
		if read.Python != nil || read.Entry != nil {
			return Package{}, invalid(`a native manifest has a "command", not a "python" or "entry"`)
		}
		if len(read.Command) == 0 || read.Command[0] == "" {
			return Package{}, invalid(`a native manifest must have a "command", such as ["./bot", "worker"]`)
		}
		for _, argument := range read.Command {
			if argument == "" || strings.ContainsAny(argument, "\x00\n\r") {
				return Package{}, invalid("the command's arguments must be non-empty lines of text")
			}
		}
		program := strings.TrimPrefix(read.Command[0], "./")
		if program == "" || !fs.ValidPath(program) || files[program] == nil {
			return Package{}, invalid("the command runs %s, which is not a file in the zip", read.Command[0])
		}
		return Package{Language: Native, EntryOrCommand: strings.Join(read.Command, " ")}, nil
	default:
		return Package{}, invalid(`the language must be "python" or "native", not %q`, *read.Language)
	}
}

// validEntry accepts module:function, where the module is dotted Python
// identifiers, as the Python SDK checks it.
func validEntry(entry string) bool {
	module, function, found := strings.Cut(entry, ":")
	if !found {
		return false
	}
	for _, name := range append(strings.Split(module, "."), function) {
		if !identifier(name) {
			return false
		}
	}
	return true
}

func identifier(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if character != '_' && !unicode.IsLetter(character) && (index == 0 || !unicode.IsDigit(character)) {
			return false
		}
	}
	return true
}

// ReadError wraps a failure to read the stored zip, such as a storage
// outage, so it is told apart from a damaged zip.
type ReadError struct{ Err error }

func (readError *ReadError) Error() string { return "read the package: " + readError.Err.Error() }
func (readError *ReadError) Unwrap() error { return readError.Err }

func mebibytes(size int64) string {
	return strconv.FormatFloat(float64(size)/(1024*1024), 'f', 1, 64) + " MiB"
}
