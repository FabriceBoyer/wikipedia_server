package wikipedia

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"

	pbzip2 "github.com/d4l3k/go-pbzip2"
)

// The index is a single flat little-endian byte buffer, so it can be
// persisted to disk once and mmapped on subsequent startups: resident
// memory then stays proportional to the pages actually touched instead of
// holding tens of millions of map entries on the heap.
//
// Layout (format "WIX1"):
//
//	header (40 bytes):
//	  0  magic      u32
//	  4  version    u32
//	  8  count      u32   number of pages (n)
//	  12 streams    u32   number of distinct bzip2 streams (m)
//	  16 srcSize    u64   size of the source .txt.bz2 index, for invalidation
//	  24 srcModTime i64   mtime (unix seconds) of the source index
//	  32 blobLen    u32   length of the title blob
//	  36 reserved   u32
//	titleOffsets  (n+1) * u32   offsets into blob, sorted by title bytes
//	seeks         n * u64       stream byte offset of each page's stream
//	ids           n * u32       page ids
//	streams       m * u64       sorted distinct stream offsets
//	blob          blobLen bytes concatenated titles in sorted order
const (
	indexMagic   = 0x31584957 // "WIX1"
	indexVersion = 1
	headerSize   = 40
)

var le = binary.LittleEndian

type index struct {
	data    []byte
	mmapped bool

	n, m                                  int
	toOff, seekOff, idOff, stOff, blobOff int
}

func newIndex(data []byte, mmapped bool) (*index, error) {
	if len(data) < headerSize {
		return nil, fmt.Errorf("index too short (%d bytes)", len(data))
	}
	if le.Uint32(data[0:]) != indexMagic {
		return nil, fmt.Errorf("bad index magic")
	}
	if v := le.Uint32(data[4:]); v != indexVersion {
		return nil, fmt.Errorf("unsupported index version %d", v)
	}
	ix := &index{
		data:    data,
		mmapped: mmapped,
		n:       int(le.Uint32(data[8:])),
		m:       int(le.Uint32(data[12:])),
	}
	blobLen := int(le.Uint32(data[32:]))
	ix.toOff = headerSize
	ix.seekOff = ix.toOff + 4*(ix.n+1)
	ix.idOff = ix.seekOff + 8*ix.n
	ix.stOff = ix.idOff + 4*ix.n
	ix.blobOff = ix.stOff + 8*ix.m
	if want := ix.blobOff + blobLen; len(data) < want {
		return nil, fmt.Errorf("truncated index: have %d bytes, want %d", len(data), want)
	}
	return ix, nil
}

func (ix *index) close() error {
	if ix.mmapped {
		return munmap(ix.data)
	}
	return nil
}

func (ix *index) title(i int) []byte {
	start := le.Uint32(ix.data[ix.toOff+4*i:])
	end := le.Uint32(ix.data[ix.toOff+4*i+4:])
	return ix.data[ix.blobOff+int(start) : ix.blobOff+int(end)]
}

func (ix *index) seek(i int) int64   { return int64(le.Uint64(ix.data[ix.seekOff+8*i:])) }
func (ix *index) id(i int) int       { return int(le.Uint32(ix.data[ix.idOff+4*i:])) }
func (ix *index) stream(j int) int64 { return int64(le.Uint64(ix.data[ix.stOff+8*j:])) }

func (ix *index) srcSize() int64    { return int64(le.Uint64(ix.data[16:])) }
func (ix *index) srcModTime() int64 { return int64(le.Uint64(ix.data[24:])) }

// find returns the position of an exact title match.
func (ix *index) find(title []byte) (int, bool) {
	i := ix.lowerBound(title)
	if i < ix.n && bytes.Equal(ix.title(i), title) {
		return i, true
	}
	return 0, false
}

// lowerBound returns the first position whose title is >= key.
func (ix *index) lowerBound(key []byte) int {
	return sort.Search(ix.n, func(i int) bool {
		return bytes.Compare(ix.title(i), key) >= 0
	})
}

// streamEnd returns the byte offset where the stream starting at seek ends,
// i.e. the next stream's offset, or fileSize for the last indexed stream.
func (ix *index) streamEnd(seek, fileSize int64) int64 {
	j := sort.Search(ix.m, func(j int) bool { return ix.stream(j) > seek })
	if j < ix.m {
		return ix.stream(j)
	}
	return fileSize
}

// buildIndex parses a Wikipedia multistream index file
// (lines of the form "seek:id:title") into the flat binary form.
// limit > 0 caps the number of entries (used by tests and benchmarks).
func buildIndex(indexPath string, limit int, srcSize, srcModTime int64) (*index, error) {
	f, err := os.Open(indexPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r, err := pbzip2.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	type entry struct {
		start, end uint32
		id         uint32
		seek       int64
	}
	var (
		blob    []byte
		entries []entry
	)

	slog.Info("building index", "file", indexPath)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		seekStr, rest, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("malformed index line %d: %q", len(entries)+1, line)
		}
		idStr, title, ok := strings.Cut(rest, ":")
		if !ok {
			return nil, fmt.Errorf("malformed index line %d: %q", len(entries)+1, line)
		}
		seek, err := strconv.ParseInt(seekStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("index line %d: %w", len(entries)+1, err)
		}
		id, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("index line %d: %w", len(entries)+1, err)
		}
		start := uint32(len(blob))
		blob = append(blob, title...)
		entries = append(entries, entry{
			start: start,
			end:   uint32(len(blob)),
			id:    uint32(id),
			seek:  seek,
		})
		if len(entries)%5_000_000 == 0 {
			slog.Info("index progress", "entries", len(entries))
		}
		if limit > 0 && len(entries) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(blob[entries[i].start:entries[i].end], blob[entries[j].start:entries[j].end]) < 0
	})

	streamSet := map[int64]struct{}{}
	for _, e := range entries {
		streamSet[e.seek] = struct{}{}
	}
	streams := make([]int64, 0, len(streamSet))
	for s := range streamSet {
		streams = append(streams, s)
	}
	sort.Slice(streams, func(i, j int) bool { return streams[i] < streams[j] })

	n, m := len(entries), len(streams)
	total := headerSize + 4*(n+1) + 8*n + 4*n + 8*m + len(blob)
	data := make([]byte, total)
	le.PutUint32(data[0:], indexMagic)
	le.PutUint32(data[4:], indexVersion)
	le.PutUint32(data[8:], uint32(n))
	le.PutUint32(data[12:], uint32(m))
	le.PutUint64(data[16:], uint64(srcSize))
	le.PutUint64(data[24:], uint64(srcModTime))
	le.PutUint32(data[32:], uint32(len(blob)))

	toOff := headerSize
	seekOff := toOff + 4*(n+1)
	idOff := seekOff + 8*n
	stOff := idOff + 4*n
	blobOff := stOff + 8*m

	pos := uint32(0)
	for i, e := range entries {
		le.PutUint32(data[toOff+4*i:], pos)
		copy(data[blobOff+int(pos):], blob[e.start:e.end])
		pos += e.end - e.start
		le.PutUint64(data[seekOff+8*i:], uint64(e.seek))
		le.PutUint32(data[idOff+4*i:], e.id)
	}
	le.PutUint32(data[toOff+4*n:], pos)
	for j, s := range streams {
		le.PutUint64(data[stOff+8*j:], uint64(s))
	}

	slog.Info("index built", "entries", n, "streams", m, "bytes", total)
	return newIndex(data, false)
}

// loadIndexFile mmaps (or, failing that, reads) a previously saved index and
// validates it against the source index file's current size and mtime.
func loadIndexFile(path string, srcSize, srcModTime int64) (*index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}

	data, mmapped, err := mmapFile(f, int(st.Size()))
	if err != nil {
		return nil, err
	}
	ix, err := newIndex(data, mmapped)
	if err != nil {
		if mmapped {
			_ = munmap(data)
		}
		return nil, err
	}
	if ix.srcSize() != srcSize || ix.srcModTime() != srcModTime {
		_ = ix.close()
		return nil, fmt.Errorf("index cache is stale")
	}
	return ix, nil
}

// saveIndexFile atomically persists the index next to the dumps so later
// startups can mmap it instead of re-parsing the bz2 index.
func saveIndexFile(ix *index, path string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, ix.data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
