package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"islandBattles/backend/botpackage"
	"islandBattles/backend/models"
)

// packageIDPattern keeps a package's ID a plain folder name.
var packageIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// load downloads the package into the runner's directory, checks it as the
// upload's check does (backend/botpackage), unpacks it within the size
// limit, starts its worker and waits for it to be ready, all within ctx.
func (runner *Runner) load(ctx context.Context, botPackage models.BotRunnerPackage) (*worker, error) {
	if !packageIDPattern.MatchString(botPackage.ID) {
		return nil, fmt.Errorf("the package ID %q is not a plain ID", botPackage.ID)
	}
	if botPackage.SizeBytes > botpackage.MaxZipBytes {
		return nil, fmt.Errorf("the package is %d bytes; the limit is %d", botPackage.SizeBytes, botpackage.MaxZipBytes)
	}
	// Nothing is loaded now, but an earlier runtime of this instance, such as
	// one that timed out, may have left a package behind in /tmp.
	if err := os.RemoveAll(runner.Directory); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(runner.Directory, 0o700); err != nil {
		return nil, err
	}
	archivePath := filepath.Join(runner.Directory, botPackage.ID+".zip")
	if err := runner.download(ctx, botPackage, archivePath); err != nil {
		return nil, err
	}
	directory := filepath.Join(runner.Directory, botPackage.ID)
	command, err := unpack(archivePath, directory)
	_ = os.Remove(archivePath)
	if err != nil {
		return nil, err
	}
	program := filepath.Join(directory, filepath.FromSlash(strings.TrimPrefix(command[0], "./")))
	started, err := startWorker(botPackage.ID, directory, append([]string{program}, command[1:]...), workerEnvironment())
	if err != nil {
		return nil, fmt.Errorf("its command could not start: %w", err)
	}
	if err := started.waitReady(ctx); err != nil {
		stderr := started.stderr.String()
		started.kill()
		return nil, &loadError{err: err, stderr: stderr}
	}
	return started, nil
}

// loadError is a worker that started but never got ready, with what it
// wrote to stderr.
type loadError struct {
	err    error
	stderr string
}

func (failure *loadError) Error() string { return failure.err.Error() }
func (failure *loadError) Unwrap() error { return failure.err }

// download writes the package's zip to path: at most MaxZipBytes, and
// exactly the size it was uploaded with.
func (runner *Runner) download(ctx context.Context, botPackage models.BotRunnerPackage, path string) error {
	parsed, err := url.Parse(botPackage.URL)
	plainHTTP := runner.AllowHTTPPackages && parsed != nil && parsed.Scheme == "http"
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !plainHTTP) {
		return errors.New("the package's URL is not an https URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, botPackage.URL, nil)
	if err != nil {
		return err
	}
	response, err := runner.client().Do(request)
	if err != nil {
		// The URL is presigned: keep its signature out of the game log.
		var urlError *url.Error
		if errors.As(err, &urlError) {
			err = urlError.Err
		}
		return fmt.Errorf("its download failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("its download answered %d", response.StatusCode)
	}
	if response.ContentLength > botpackage.MaxZipBytes {
		return fmt.Errorf("the package is %d bytes; the limit is %d", response.ContentLength, botpackage.MaxZipBytes)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	written, err := io.Copy(file, io.LimitReader(response.Body, botpackage.MaxZipBytes+1))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	switch {
	case err != nil:
		return fmt.Errorf("its download failed: %w", err)
	case written > botpackage.MaxZipBytes:
		return fmt.Errorf("the package is more than %d bytes, the limit", botpackage.MaxZipBytes)
	case botPackage.SizeBytes > 0 && written != botPackage.SizeBytes:
		return fmt.Errorf("the package's zip is %d bytes, not the %d it was uploaded with", written, botPackage.SizeBytes)
	}
	return nil
}

// unpack checks the zip at archivePath like the upload's check, then
// unpacks it into directory, counting the bytes it writes: the sizes in the
// zip come from the uploader, so the limit holds even when they lie. It
// returns the manifest's command.
func unpack(archivePath, directory string) ([]string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	checked, err := botpackage.Inspect(file, info.Size())
	if err != nil {
		return nil, fmt.Errorf("the package is refused: %w", err)
	}
	if checked.Language != botpackage.Native {
		return nil, fmt.Errorf("it is a %s package, which the Python runner plays, not this one", checked.Language)
	}
	archive, err := zip.NewReader(file, info.Size())
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	var unpacked int64
	for _, entry := range archive.File {
		written, err := unpackEntry(entry, directory, botpackage.MaxUnpackedBytes-unpacked)
		unpacked += written
		if err != nil {
			return nil, err
		}
	}
	data, err := os.ReadFile(filepath.Join(directory, botpackage.ManifestName))
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Command []string `json:"command"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || len(manifest.Command) == 0 {
		return nil, fmt.Errorf("%s has no command", botpackage.ManifestName)
	}
	return manifest.Command, nil
}

// unpackEntry writes one entry of at most room bytes, which Inspect has
// checked: a folder, or a regular file with a path inside the package.
func unpackEntry(entry *zip.File, directory string, room int64) (int64, error) {
	name := strings.TrimSuffix(entry.Name, "/")
	target := filepath.Join(directory, filepath.FromSlash(name))
	if !strings.HasPrefix(target, directory+string(filepath.Separator)) {
		return 0, fmt.Errorf("%s is outside the package", entry.Name)
	}
	if entry.Mode().IsDir() {
		return 0, os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	mode := os.FileMode(0o644)
	if entry.Mode()&0o111 != 0 {
		mode = 0o755
	}
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return 0, fmt.Errorf("%s can't be unpacked: %w", entry.Name, err)
	}
	input, err := entry.Open()
	if err != nil {
		_ = output.Close()
		return 0, fmt.Errorf("%s can't be read: %w", entry.Name, err)
	}
	written, err := io.Copy(output, io.LimitReader(input, room+1))
	_ = input.Close()
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	switch {
	case written > room:
		return written, fmt.Errorf("the package is more than %d bytes unpacked, the limit", botpackage.MaxUnpackedBytes)
	case err != nil:
		return written, fmt.Errorf("%s can't be unpacked: %w", entry.Name, err)
	}
	return written, nil
}
