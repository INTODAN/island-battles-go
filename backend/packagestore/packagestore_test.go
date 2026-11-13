package packagestore_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"islandBattles/backend/botpackage"
	"islandBattles/backend/packagestore"
	"islandBattles/backend/packagestore/locals3"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

// fakeBucket serves a fake S3 with one bucket.
func fakeBucket(t *testing.T) *packagestore.S3 {
	t.Helper()
	backend := s3mem.New()
	if err := backend.CreateBucket("packages"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gofakes3.New(backend, gofakes3.WithLogger(gofakes3.DiscardLog())).Server())
	t.Cleanup(server.Close)
	return packagestore.Endpoint(server.URL, "packages", "test", "test")
}

// upload posts data with a presigned POST, as a browser form does.
func upload(t *testing.T, presigned packagestore.Upload, data []byte) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range presigned.Fields {
		if err := form.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("file", "island-battles-bot.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(presigned.URL, form.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode/100 != 2 {
		message, _ := io.ReadAll(response.Body)
		t.Fatalf("upload = %d %s", response.StatusCode, message)
	}
}

func nativePackage(t *testing.T, extraFiles int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	add := func(name, data string, mode uint32) {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o644)
		if mode != 0 {
			header.SetMode(0o755)
		}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	add(botpackage.ManifestName, `{"format": 1, "language": "native", "command": ["./bot", "worker"]}`, 0)
	add("bot", strings.Repeat("\x7fELF binary ", 20000), 1)
	for index := range extraFiles {
		add(fmt.Sprintf("data/%06d.txt", index), "", 0)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestAPresignedUploadIsReadBackWithRanges(t *testing.T) {
	ctx := context.Background()
	store := fakeBucket(t)
	key := "lobbies/lobby/package.zip"
	presigned, err := store.PresignUpload(ctx, key, botpackage.MaxZipBytes, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(presigned.URL, "/packages") || presigned.Fields["key"] != key {
		t.Fatalf("upload = %+v", presigned)
	}
	if _, err := store.Size(ctx, key); !errors.Is(err, packagestore.ErrNotFound) {
		t.Fatalf("size before the upload: %v", err)
	}
	data := nativePackage(t, 0)
	upload(t, presigned, data)
	size, err := store.Size(ctx, key)
	if err != nil || size != int64(len(data)) {
		t.Fatalf("size = %d, %v; want %d", size, err, len(data))
	}
	part, err := store.ReadRange(ctx, key, 2, 3)
	if err != nil || !bytes.Equal(part, data[2:5]) {
		t.Fatalf("range = %q, %v", part, err)
	}
	built, err := botpackage.Inspect(packagestore.ReaderAt(ctx, store, key, size), size)
	if err != nil || built.EntryOrCommand != "./bot worker" {
		t.Fatalf("inspect = %+v, %v", built, err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Size(ctx, key); !errors.Is(err, packagestore.ErrNotFound) {
		t.Fatalf("size after delete: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("deleting a deleted object: %v", err)
	}
}

func TestThePresignedPolicyLimitsTheSizeAndTheKey(t *testing.T) {
	store := fakeBucket(t)
	before := time.Now()
	presigned, err := store.PresignUpload(context.Background(), "lobbies/l/p.zip", botpackage.MaxZipBytes, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := base64.StdEncoding.DecodeString(presigned.Fields["policy"])
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Expiration time.Time `json:"expiration"`
		Conditions []any     `json:"conditions"`
	}
	if err := json.Unmarshal(encoded, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Expiration.Before(before.Add(14*time.Minute)) || policy.Expiration.After(before.Add(16*time.Minute)) {
		t.Fatalf("expiration = %s", policy.Expiration)
	}
	conditions, _ := json.Marshal(policy.Conditions)
	for _, want := range []string{`["content-length-range",1,262144000]`, `{"key":"lobbies/l/p.zip"}`, `{"bucket":"packages"}`} {
		if !strings.Contains(string(conditions), want) {
			t.Fatalf("conditions %s lack %s", conditions, want)
		}
	}
}

// countingStore counts the range requests.
type countingStore struct {
	packagestore.Store
	reads atomic.Int32
}

func (store *countingStore) ReadRange(ctx context.Context, key string, offset, length int64) ([]byte, error) {
	store.reads.Add(1)
	return store.Store.ReadRange(ctx, key, offset, length)
}

func TestAPackageIsCheckedWithAFewRangeRequests(t *testing.T) {
	ctx := context.Background()
	// 70,000 files make the directory larger than the tail that the first
	// request reads, and need zip64's record of the file count.
	for _, files := range []int{0, 2000, 70000} {
		t.Run(fmt.Sprint(files), func(t *testing.T) {
			bucket := fakeBucket(t)
			data := nativePackage(t, files)
			presigned, err := bucket.PresignUpload(ctx, "p.zip", botpackage.MaxZipBytes, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			upload(t, presigned, data)
			store := &countingStore{Store: bucket}
			built, err := botpackage.Inspect(packagestore.ReaderAt(ctx, store, "p.zip", int64(len(data))), int64(len(data)))
			if err != nil || built.Language != "native" {
				t.Fatalf("inspect = %+v, %v", built, err)
			}
			// The tail, the rest of the directory, and the manifest.
			if reads := store.reads.Load(); reads > 3 {
				t.Fatalf("%d range requests, want at most 3", reads)
			}
		})
	}
}

func TestAMissingObjectIsAReadError(t *testing.T) {
	_, err := botpackage.Inspect(packagestore.ReaderAt(context.Background(), fakeBucket(t), "missing.zip", 100), 100)
	var readError *botpackage.ReadError
	if !errors.As(err, &readError) || !errors.Is(err, packagestore.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestAForbiddenHeadIsAMissingObject(t *testing.T) {
	// S3 answers 403 for a key that isn't there to a role that may not list
	// the bucket.
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	store := packagestore.Endpoint(server.URL, "packages", "test", "test")
	if _, err := store.Size(context.Background(), "p.zip"); !errors.Is(err, packagestore.ErrNotFound) {
		t.Fatalf("size = %v, want ErrNotFound", err)
	}
	if _, err := store.ReadRange(context.Background(), "p.zip", 0, 10); err == nil || errors.Is(err, packagestore.ErrNotFound) {
		t.Fatalf("a forbidden read = %v, want an error of its own", err)
	}
}

func TestTheLocalBucketKeepsPackagesOnDisk(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	store, handler, err := locals3.New(directory, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle(locals3.Prefix+"/", handler)
	presigned, err := store.PresignUpload(ctx, "lobbies/l/p.zip", botpackage.MaxZipBytes, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if presigned.URL != server.URL+locals3.Prefix+"/"+locals3.Bucket {
		t.Fatalf("url = %s", presigned.URL)
	}
	data := nativePackage(t, 0)
	upload(t, presigned, data)
	size, err := store.Size(ctx, "lobbies/l/p.zip")
	if err != nil || size != int64(len(data)) {
		t.Fatalf("size = %d, %v", size, err)
	}
	if _, err := botpackage.Inspect(packagestore.ReaderAt(ctx, store, "lobbies/l/p.zip", size), size); err != nil {
		t.Fatal(err)
	}
	// A bot runner downloads it with a presigned GET (#225).
	link, err := store.PresignDownload(ctx, "lobbies/l/p.zip", time.Hour)
	if err != nil || !strings.HasPrefix(link, server.URL+locals3.Prefix+"/"+locals3.Bucket+"/lobbies/l/p.zip?") || !strings.Contains(link, "X-Amz-Expires=3600") {
		t.Fatalf("presigned GET = %s, %v", link, err)
	}
	response, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !bytes.Equal(downloaded, data) {
		t.Fatalf("the presigned GET answered %d with %d bytes", response.StatusCode, len(downloaded))
	}
	// A second store on the same folder, as after a restart, finds it.
	again, _, err := locals3.New(directory, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if size, err := again.Size(ctx, "lobbies/l/p.zip"); err != nil || size != int64(len(data)) {
		t.Fatalf("size after a restart = %d, %v", size, err)
	}
}
