package botpackage_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"hash/crc32"
	"io/fs"
	"strings"
	"testing"

	"islandBattles/backend/botpackage"
)

// file is one entry of a test zip.
type file struct {
	name string
	data string
	mode fs.FileMode
	// declared replaces the unpacked size that the central directory
	// claims, for a zip bomb that never has to be built.
	declared uint64
}

const pythonManifest = `{"format": 1, "language": "python", "python": "3.14", "entry": "bot:play"}`
const nativeManifest = `{"format": 1, "language": "native", "command": ["./bot", "worker"]}`

func build(t *testing.T, files ...file) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, entry := range files {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header.SetMode(mode)
		if entry.declared != 0 {
			header.CompressedSize64 = uint64(len(entry.data))
			header.UncompressedSize64 = entry.declared
			header.CRC32 = crc32.ChecksumIEEE([]byte(entry.data))
			writer, err := archive.CreateRaw(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write([]byte(entry.data)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func inspect(data []byte) (botpackage.Package, error) {
	return botpackage.Inspect(bytes.NewReader(data), int64(len(data)))
}

func TestAPythonPackageIsAccepted(t *testing.T) {
	data := build(t,
		file{name: botpackage.ManifestName, data: pythonManifest},
		file{name: "site/", mode: fs.ModeDir | 0o755},
		file{name: "site/bot.py", data: "def play(turn): ...\n"},
		file{name: "site/island_battles_client/worker.py", data: "# worker\n"},
	)
	built, err := inspect(data)
	if err != nil {
		t.Fatal(err)
	}
	want := botpackage.Package{Language: "python", EntryOrCommand: "bot:play", UnpackedBytes: int64(len(pythonManifest) + len("def play(turn): ...\n") + len("# worker\n"))}
	if built != want {
		t.Fatalf("package = %+v, want %+v", built, want)
	}
}

func TestANativePackageIsAccepted(t *testing.T) {
	data := build(t,
		file{name: botpackage.ManifestName, data: nativeManifest},
		file{name: "bot", data: "\x7fELF", mode: 0o755},
	)
	built, err := inspect(data)
	if err != nil {
		t.Fatal(err)
	}
	if built.Language != "native" || built.EntryOrCommand != "./bot worker" || built.UnpackedBytes != int64(len(nativeManifest)+4) {
		t.Fatalf("package = %+v", built)
	}
}

func TestBadPackagesAreRefusedWithAReason(t *testing.T) {
	bot := file{name: "bot", data: "\x7fELF", mode: 0o755}
	cases := map[string]struct {
		files []file
		want  string
	}{
		"no manifest":             {[]file{{name: "bot.py", data: "x"}}, "has no island-battles-bot.json at its root"},
		"manifest in a folder":    {[]file{{name: "dist/island-battles-bot.json", data: pythonManifest}}, "has no island-battles-bot.json at its root"},
		"manifest not JSON":       {[]file{{name: botpackage.ManifestName, data: "{"}}, "is not a valid manifest"},
		"unknown manifest key":    {[]file{{name: botpackage.ManifestName, data: `{"format": 1, "language": "python", "python": "3.14", "entry": "bot:play", "extra": 1}`}}, "is not a valid manifest"},
		"format 2":                {[]file{{name: botpackage.ManifestName, data: `{"format": 2, "language": "python", "python": "3.14", "entry": "bot:play"}`}}, `"format": 1`},
		"no language":             {[]file{{name: botpackage.ManifestName, data: `{"format": 1}`}}, `"language": "python" or "native"`},
		"other language":          {[]file{{name: botpackage.ManifestName, data: `{"format": 1, "language": "ruby"}`}}, `not "ruby"`},
		"other Python":            {[]file{{name: botpackage.ManifestName, data: `{"format": 1, "language": "python", "python": "3.12", "entry": "bot:play"}`}}, `"python": "3.14"`},
		"entry without function":  {[]file{{name: botpackage.ManifestName, data: `{"format": 1, "language": "python", "python": "3.14", "entry": "bot"}`}}, `the entry "bot" must be module:function`},
		"entry with a path":       {[]file{{name: botpackage.ManifestName, data: `{"format": 1, "language": "python", "python": "3.14", "entry": "bots/my:play"}`}}, "must be module:function"},
		"entry starting a digit":  {[]file{{name: botpackage.ManifestName, data: `{"format": 1, "language": "python", "python": "3.14", "entry": "bot:1play"}`}}, "must be module:function"},
		"no entry":                {[]file{{name: botpackage.ManifestName, data: `{"format": 1, "language": "python", "python": "3.14"}`}}, "the entry must be module:function"},
		"native without command":  {[]file{{name: botpackage.ManifestName, data: `{"format": 1, "language": "native"}`}, bot}, `must have a "command"`},
		"native command missing":  {[]file{{name: botpackage.ManifestName, data: `{"format": 1, "language": "native", "command": ["./server"]}`}, bot}, "runs ./server, which is not a file in the zip"},
		"native command a folder": {[]file{{name: botpackage.ManifestName, data: `{"format": 1, "language": "native", "command": ["./bin"]}`}, {name: "bin/", mode: fs.ModeDir | 0o755}}, "which is not a file in the zip"},
		"native command escapes":  {[]file{{name: botpackage.ManifestName, data: `{"format": 1, "language": "native", "command": ["../bot"]}`}, bot}, "which is not a file in the zip"},
		"path traversal":          {[]file{{name: botpackage.ManifestName, data: nativeManifest}, bot, {name: "site/../../etc/passwd", data: "x"}}, "goes outside the package with .."},
		"absolute path":           {[]file{{name: botpackage.ManifestName, data: nativeManifest}, bot, {name: "/etc/passwd", data: "x"}}, "is an absolute path"},
		"drive letter":            {[]file{{name: botpackage.ManifestName, data: nativeManifest}, bot, {name: "C:/bot.exe", data: "x"}}, "is an absolute path"},
		"backslash":               {[]file{{name: botpackage.ManifestName, data: nativeManifest}, bot, {name: `..\bot`, data: "x"}}, "has a backslash"},
		"dot part":                {[]file{{name: botpackage.ManifestName, data: nativeManifest}, bot, {name: "site/./bot.py", data: "x"}}, "is not a plain relative path"},
		"symbolic link":           {[]file{{name: botpackage.ManifestName, data: nativeManifest}, bot, {name: "site/link", data: "/etc", mode: fs.ModeSymlink | 0o777}}, "site/link is a symbolic link"},
		"named pipe":              {[]file{{name: botpackage.ManifestName, data: nativeManifest}, bot, {name: "pipe", mode: fs.ModeNamedPipe | 0o644}}, "pipe is not a regular file"},
		"over 1 GiB unpacked": {[]file{{name: botpackage.ManifestName, data: nativeManifest}, bot,
			{name: "site/a.bin", data: "a", declared: 600 << 20}, {name: "site/b.bin", data: "b", declared: 600 << 20}}, "more than 1024.0 MiB unpacked"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := inspect(build(t, test.files...))
			var refused *botpackage.Invalid
			if !errors.As(err, &refused) {
				t.Fatalf("err = %v, want an Invalid", err)
			}
			if !strings.Contains(refused.Reason, test.want) {
				t.Fatalf("reason = %q, want it to say %q", refused.Reason, test.want)
			}
		})
	}
}

func TestExactly1GiBUnpackedIsAccepted(t *testing.T) {
	manifest := file{name: botpackage.ManifestName, data: nativeManifest}
	bot := file{name: "bot", data: "\x7fELF", mode: 0o755}
	rest := uint64(botpackage.MaxUnpackedBytes - len(nativeManifest) - 4)
	built, err := inspect(build(t, manifest, bot, file{name: "data.bin", data: "x", declared: rest}))
	if err != nil {
		t.Fatal(err)
	}
	if built.UnpackedBytes != botpackage.MaxUnpackedBytes {
		t.Fatalf("unpacked = %d", built.UnpackedBytes)
	}
}

func TestAFileThatIsNotAZipIsRefused(t *testing.T) {
	_, err := inspect([]byte("this is not a zip at all"))
	var refused *botpackage.Invalid
	if !errors.As(err, &refused) || !strings.Contains(refused.Reason, "is not a zip archive") {
		t.Fatalf("err = %v", err)
	}
}

func TestAZipOver250MiBIsRefusedBeforeReading(t *testing.T) {
	_, err := botpackage.Inspect(failingReader{}, botpackage.MaxZipBytes+1)
	var refused *botpackage.Invalid
	if !errors.As(err, &refused) || !strings.Contains(refused.Reason, "the limit is 250.0 MiB") {
		t.Fatalf("err = %v", err)
	}
}

func TestAReadFailureIsNotCalledAnInvalidPackage(t *testing.T) {
	_, err := botpackage.Inspect(failingReader{}, 1000)
	var readError *botpackage.ReadError
	if !errors.As(err, &readError) {
		t.Fatalf("err = %v, want a ReadError", err)
	}
}

// failingReader fails every read as a storage outage would.
type failingReader struct{}

func (failingReader) ReadAt([]byte, int64) (int, error) {
	return 0, &botpackage.ReadError{Err: errors.New("storage unavailable")}
}

func TestTheLimitsAreTheSDKs(t *testing.T) {
	if botpackage.MaxZipBytes != 262_144_000 || botpackage.MaxUnpackedBytes != 1_073_741_824 {
		t.Fatalf("limits = %d, %d", botpackage.MaxZipBytes, botpackage.MaxUnpackedBytes)
	}
}
