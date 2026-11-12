package packagestore

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"

	"islandBattles/backend/botpackage"
)

// tailBytes is how much of the end of a zip the first read fetches: the end
// of central directory record, with its longest comment, and the zip64
// locator.
const tailBytes = 64*1024 + 22 + 20

// maxDirectoryBytes bounds the central directory fetched in one request. A
// package of 100,000 files has a directory of about 10 MB.
const maxDirectoryBytes = 32 * 1024 * 1024

// windowBytes is the least a read before the cached directory fetches.
const windowBytes = 64 * 1024

// ReaderAt reads the object at key, of size bytes, with range requests, for
// botpackage.Inspect. The first read fetches the tail of the zip, finds its
// central directory there, and fetches the rest of the directory, so that
// opening the zip takes at most two requests. Other reads, such as the
// manifest's, fetch what they ask for. Every failure is a
// *botpackage.ReadError.
func ReaderAt(ctx context.Context, store Store, key string, size int64) io.ReaderAt {
	return &rangeReader{ctx: ctx, store: store, key: key, size: size}
}

type rangeReader struct {
	ctx   context.Context
	store Store
	key   string
	size  int64

	mutex sync.Mutex
	// cached holds the bytes from cachedFrom to the end of the object, once
	// loaded.
	cached     []byte
	cachedFrom int64
	loaded     bool
	// window holds the bytes from windowFrom that the latest read before
	// the cache fetched.
	window     []byte
	windowFrom int64
}

func (reader *rangeReader) ReadAt(buffer []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, &botpackage.ReadError{Err: errors.New("negative offset")}
	}
	if offset >= reader.size {
		return 0, io.EOF
	}
	want := min(int64(len(buffer)), reader.size-offset)
	reader.mutex.Lock()
	defer reader.mutex.Unlock()
	if !reader.loaded {
		if err := reader.loadDirectory(); err != nil {
			return 0, &botpackage.ReadError{Err: err}
		}
	}
	if offset >= reader.cachedFrom {
		return finish(buffer, reader.cached[offset-reader.cachedFrom:], want)
	}
	if offset < reader.windowFrom || offset+want > reader.windowFrom+int64(len(reader.window)) {
		// A file's local header, its data and its data descriptor are read
		// one after the other, so one request fetches a window of them.
		length := min(max(want, windowBytes), reader.cachedFrom-offset)
		if length < want {
			length = want
		}
		data, err := reader.fetch(offset, length)
		if err != nil {
			return 0, &botpackage.ReadError{Err: err}
		}
		reader.window, reader.windowFrom = data, offset
	}
	return finish(buffer, reader.window[offset-reader.windowFrom:], want)
}

// loadDirectory caches the tail of the zip, and before it the rest of the
// central directory that the tail's end record points to.
func (reader *rangeReader) loadDirectory() error {
	from := max(reader.size-tailBytes, 0)
	tail, err := reader.fetch(from, reader.size-from)
	if err != nil {
		return err
	}
	reader.cached, reader.cachedFrom, reader.loaded = tail, from, true
	start, found := directoryStart(tail, from)
	if !found || start < 0 || start >= from || reader.size-start > maxDirectoryBytes {
		return nil
	}
	head, err := reader.fetch(start, from-start)
	if err != nil {
		return err
	}
	reader.cached, reader.cachedFrom = append(head, tail...), start
	return nil
}

// fetch reads exactly length bytes from offset.
func (reader *rangeReader) fetch(offset, length int64) ([]byte, error) {
	data, err := reader.store.ReadRange(reader.ctx, reader.key, offset, length)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != length {
		return nil, io.ErrUnexpectedEOF
	}
	return data, nil
}

// directoryStart finds where the central directory starts from the end of
// a zip, tail, which starts at offset from: in the end of central directory
// record, or in the zip64 record that it points to when that is in tail too.
func directoryStart(tail []byte, from int64) (int64, bool) {
	const endLength, locatorLength, zip64Length = 22, 20, 56
	for position := len(tail) - endLength; position >= 0; position-- {
		if string(tail[position:position+4]) != "PK\x05\x06" {
			continue
		}
		commentLength := int(binary.LittleEndian.Uint16(tail[position+20:]))
		if position+endLength+commentLength != len(tail) {
			continue
		}
		start := int64(binary.LittleEndian.Uint32(tail[position+16:]))
		if start != 0xffffffff {
			return start, true
		}
		locator := position - locatorLength
		if locator < 0 || string(tail[locator:locator+4]) != "PK\x06\x07" {
			return 0, false
		}
		record := int64(binary.LittleEndian.Uint64(tail[locator+8:])) - from
		if record < 0 || record+zip64Length > int64(len(tail)) || string(tail[record:record+4]) != "PK\x06\x06" {
			return 0, false
		}
		return int64(binary.LittleEndian.Uint64(tail[record+48:])), true
	}
	return 0, false
}

// finish copies want bytes of data, and reports a read that ends at the end
// of the object as io.ReaderAt does.
func finish(buffer, data []byte, want int64) (int, error) {
	copied := copy(buffer, data[:want])
	if copied < len(buffer) {
		return copied, io.EOF
	}
	return copied, nil
}
