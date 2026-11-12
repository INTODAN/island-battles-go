package main

import (
	"archive/zip"
	"bytes"
	"debug/elf"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const examples = sdkPath + "/examples/"

func TestPackageBuildsALinuxArm64Zip(t *testing.T) {
	output := filepath.Join(t.TempDir(), "dist", "bot.zip")
	var stdout, stderr bytes.Buffer
	if status := run([]string{"--output", output, examples + "firstbot"}, &stdout, &stderr); status != 0 {
		t.Fatalf("got status %d, stderr %q", status, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "wrote "+output) {
		t.Fatalf("got %q", stdout.String())
	}
	archive, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.Close() }()
	if len(archive.File) != 2 || archive.File[0].Name != "island-battles-bot.json" || archive.File[1].Name != "bot" {
		t.Fatalf("got %d files, the first %q", len(archive.File), archive.File[0].Name)
	}
	if archive.File[1].Mode()&0o111 == 0 {
		t.Fatalf("the bot is not executable: %v", archive.File[1].Mode())
	}
	read := func(file *zip.File) []byte {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = reader.Close() }()
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if got := string(read(archive.File[0])); got != "{\n  \"format\": 1,\n  \"language\": \"native\",\n  \"command\": [\"./bot\", \"worker\"]\n}\n" {
		t.Fatalf("got the manifest %q", got)
	}
	binary, err := elf.NewFile(bytes.NewReader(read(archive.File[1])))
	if err != nil {
		t.Fatalf("the bot is not an ELF file: %v", err)
	}
	if binary.Machine != elf.EM_AARCH64 || binary.Class != elf.ELFCLASS64 {
		t.Fatalf("got machine %v, class %v", binary.Machine, binary.Class)
	}
	if libraries, _ := binary.ImportedLibraries(); len(libraries) > 0 {
		t.Fatalf("the bot is not static: %v", libraries)
	}
}

func TestPackageRefusesAProgramThatDoesNotCallMain(t *testing.T) {
	output := filepath.Join(t.TempDir(), "bot.zip")
	var stderr bytes.Buffer
	if status := run([]string{"--output", output, "./testdata/nomain"}, io.Discard, &stderr); status != 1 || !strings.Contains(stderr.String(), "never calls islandbattles.Main") {
		t.Fatalf("got status %d, stderr %q", status, stderr.String())
	}
	if _, err := os.Stat(output); err == nil {
		t.Fatal("a zip was written")
	}
}

func TestPackageRefusesAPackageThatIsNotMain(t *testing.T) {
	var stderr bytes.Buffer
	if status := run([]string{"--output", filepath.Join(t.TempDir(), "bot.zip"), examples + "defaultbots"}, io.Discard, &stderr); status != 1 || !strings.Contains(stderr.String(), "must be a main package") {
		t.Fatalf("got status %d, stderr %q", status, stderr.String())
	}
}

func TestPackageNamesAMissingPackage(t *testing.T) {
	var stderr bytes.Buffer
	if status := run([]string{"--output", filepath.Join(t.TempDir(), "bot.zip"), "./nothing/here"}, io.Discard, &stderr); status != 1 || !strings.Contains(stderr.String(), "could not find the package ./nothing/here") {
		t.Fatalf("got status %d, stderr %q", status, stderr.String())
	}
}

func TestAnOversizedPackageNamesItsLargestFiles(t *testing.T) {
	output := filepath.Join(t.TempDir(), "bot.zip")
	// The size is declared and never read, so no large file is needed.
	entries := []entry{
		{name: "island-battles-bot.json", size: 10, data: strings.NewReader("")},
		{name: "bot", size: maxUnpackedBytes + 1, data: strings.NewReader("")},
	}
	_, _, err := writeZip(output, entries)
	if err == nil || !strings.Contains(err.Error(), "unpacked") || !strings.Contains(err.Error(), "bot") || !strings.Contains(err.Error(), "limit is 1024.0 MiB") {
		t.Fatalf("got %v", err)
	}
	if _, statErr := os.Stat(output); statErr == nil {
		t.Fatal("a zip was written")
	}
}

func TestAnOversizedZipNamesItsLargestFiles(t *testing.T) {
	err := sizeError("the zip is %s; the limit is %s", maxZipBytes+1, maxZipBytes, []entry{{name: "bot", size: 300 << 20}, {name: "small", size: 1}})
	if !strings.Contains(err.Error(), "limit is 250.0 MiB") || !strings.Contains(err.Error(), "300.0 MiB  bot") {
		t.Fatalf("got %v", err)
	}
}
