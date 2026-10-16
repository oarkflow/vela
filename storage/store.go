// Package storage implements a simple checksummed append-only VELA record log
// with an in-memory key index, recovery, and atomic compaction. For production
// databases add MVCC/transactions, locking across processes, and WAL rotation.
package storage

import (
	"bufio"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"

	"github.com/oarkflow/vela"
	"github.com/oarkflow/vela/query"
)

var ErrCorrupt = errors.New("vela/storage: corrupt store")
var ErrClosed = errors.New("vela/storage: closed")
var ErrLocked = errors.New("vela/storage: already opened by another process")

const maxEntry = 64 << 20

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Store must only be opened by one writer process at a time.
type Store struct {
	mu        sync.RWMutex
	file      *os.File
	path      string
	items     map[string][]byte
	lock      *os.File
	limits    Limits
	liveBytes int64
}

type Limits struct {
	MaxFileBytes int64
	MaxLiveBytes int64
	MaxRecords   int
}

var DefaultLimits = Limits{MaxFileBytes: 8 << 30, MaxLiveBytes: 512 << 20, MaxRecords: 1 << 20}

func (l Limits) normalized() Limits {
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = DefaultLimits.MaxFileBytes
	}
	if l.MaxLiveBytes <= 0 {
		l.MaxLiveBytes = DefaultLimits.MaxLiveBytes
	}
	if l.MaxRecords <= 0 {
		l.MaxRecords = DefaultLimits.MaxRecords
	}
	return l
}

func Open(path string) (*Store, error) { return OpenWithLimits(path, Limits{}) }
func OpenWithLimits(path string, limits Limits) (*Store, error) {
	limits = limits.normalized()
	// Separate lock inode survives atomic segment replacement during compaction.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, ErrLocked
	}
	release := func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }
	f, e := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if e != nil {
		release()
		return nil, e
	}
	s := &Store{file: f, path: path, lock: lock, limits: limits, items: make(map[string][]byte)}
	info, e := f.Stat()
	if e != nil {
		f.Close()
		release()
		return nil, e
	}
	if info.Size() == 0 {
		if _, e = f.Write([]byte("VLS1")); e != nil {
			f.Close()
			release()
			return nil, e
		}
		if e = f.Sync(); e != nil {
			f.Close()
			release()
			return nil, e
		}
	} else {
		if e = s.recover(); e != nil {
			f.Close()
			release()
			return nil, e
		}
	}
	_, e = f.Seek(0, io.SeekEnd)
	if e != nil {
		f.Close()
		release()
		return nil, e
	}
	return s, nil
}
func (s *Store) recover() error {
	info, err := s.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > s.limits.MaxFileBytes {
		return vela.ErrLimit
	}
	if _, err = s.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	br := bufio.NewReaderSize(s.file, 64<<10)
	var header [4]byte
	if _, err = io.ReadFull(br, header[:]); err != nil {
		return ErrCorrupt
	}
	if string(header[:]) != "VLS1" {
		return ErrCorrupt
	}
	good := int64(4)
	truncateTail := false
	for {
		var prefix [10]byte
		prefixN := 0
		var size uint64
		var shift uint
		for {
			v, e := br.ReadByte()
			if e == io.EOF {
				if prefixN > 0 {
					truncateTail = true
				}
				break
			}
			if e != nil {
				return e
			}
			if prefixN == 10 {
				return ErrCorrupt
			}
			prefix[prefixN] = v
			prefixN++
			if v < 0x80 {
				if prefixN == 10 && v > 1 {
					return ErrCorrupt
				}
				size |= uint64(v) << shift
				if prefixN > 1 && v == 0 {
					return ErrCorrupt
				}
				break
			}
			if prefixN == 10 {
				return ErrCorrupt
			}
			size |= uint64(v&0x7f) << shift
			shift += 7
		}
		if prefixN == 0 || truncateTail {
			break
		}
		if size > maxEntry {
			return ErrCorrupt
		}
		if size > uint64(s.limits.MaxFileBytes-good-int64(prefixN)) {
			return vela.ErrLimit
		}
		body := make([]byte, int(size))
		if _, err = io.ReadFull(br, body); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				truncateTail = true
				break
			}
			return err
		}
		packed := make([]byte, 0, prefixN+len(body))
		packed = append(packed, prefix[:prefixN]...)
		packed = append(packed, body...)
		op, k, v, next, ok, partial := parseEntry(packed, 0)
		if !ok || partial || next != len(packed) {
			return ErrCorrupt
		}
		key := string(k)
		if op == 1 {
			if prev, ok := s.items[key]; ok {
				s.liveBytes -= int64(len(prev))
			} else if len(s.items) >= s.limits.MaxRecords {
				return vela.ErrLimit
			}
			s.liveBytes += int64(len(v))
			if s.liveBytes > s.limits.MaxLiveBytes {
				return vela.ErrLimit
			}
			s.items[key] = append([]byte(nil), v...)
		} else {
			if prev, ok := s.items[key]; ok {
				s.liveBytes -= int64(len(prev))
			}
			delete(s.items, key)
		}
		good += int64(prefixN + len(body))
	}
	if truncateTail {
		if err = s.file.Truncate(good); err != nil {
			return err
		}
		if err = s.file.Sync(); err != nil {
			return err
		}
	}
	return nil
}

// A single entry: varint(body length), op, key len, data len, key, data, crc32c.
// Length is bounded and checksum protects all bytes after the length prefix.
func entry(op byte, key string, data []byte) []byte {
	body := []byte{op}
	body = binary.AppendUvarint(body, uint64(len(key)))
	body = binary.AppendUvarint(body, uint64(len(data)))
	body = append(body, key...)
	body = append(body, data...)
	sum := crc32.Checksum(body, castagnoli)
	body = binary.LittleEndian.AppendUint32(body, sum)
	out := binary.AppendUvarint(nil, uint64(len(body)))
	return append(out, body...)
}
func parseEntry(in []byte, pos int) (byte, []byte, []byte, int, bool, bool) {
	size, n := binary.Uvarint(in[pos:])
	if n == 0 {
		return 0, nil, nil, pos, false, true
	}
	if n < 0 || size > maxEntry {
		return 0, nil, nil, pos, false, false
	}
	start := pos + n
	if size > uint64(len(in)-start) {
		return 0, nil, nil, pos, false, true
	}
	body := in[start : start+int(size)]
	if len(body) < 7 {
		return 0, nil, nil, pos, false, false
	}
	got := binary.LittleEndian.Uint32(body[len(body)-4:])
	if crc32.Checksum(body[:len(body)-4], castagnoli) != got {
		return 0, nil, nil, pos, false, false
	}
	op := body[0]
	if op != 1 && op != 2 {
		return 0, nil, nil, pos, false, false
	}
	p := 1
	klen, m := binary.Uvarint(body[p:])
	if m <= 0 {
		return 0, nil, nil, pos, false, false
	}
	p += m
	vlen, m := binary.Uvarint(body[p:])
	if m <= 0 {
		return 0, nil, nil, pos, false, false
	}
	p += m
	if klen > uint64(len(body)-p-4) {
		return 0, nil, nil, pos, false, false
	}
	key := body[p : p+int(klen)]
	p += int(klen)
	if vlen != uint64(len(body)-p-4) || (op == 2 && vlen != 0) || klen == 0 {
		return 0, nil, nil, pos, false, false
	}
	data := body[p : p+int(vlen)]
	return op, key, data, start + int(size), true, false
}

// writeEntry is called under the store lock. A partial append is truncated so
// subsequent writes never follow a partially written record.
func (s *Store) writeEntry(op byte, key string, payload []byte) error {
	packed := entry(op, key, payload)
	info, err := s.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > s.limits.MaxFileBytes-int64(len(packed)) {
		return vela.ErrLimit
	}
	offset, err := s.file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	written, err := s.file.Write(packed)
	if err != nil || written != len(packed) {
		_ = s.file.Truncate(offset)
		_, _ = s.file.Seek(0, io.SeekEnd)
		if err != nil {
			return err
		}
		return io.ErrShortWrite
	}
	if err = s.file.Sync(); err != nil {
		return err
	}
	return nil
}
func (s *Store) Put(key string, data []byte) error {
	if _, e := vela.Open(data); e != nil {
		return e
	}
	if len(key) == 0 || len(data)+len(key) > maxEntry {
		return vela.ErrLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return ErrClosed
	}
	if prev, ok := s.items[key]; ok {
		if s.liveBytes-int64(len(prev))+int64(len(data)) > s.limits.MaxLiveBytes {
			return vela.ErrLimit
		}
	} else if len(s.items) >= s.limits.MaxRecords || s.liveBytes+int64(len(data)) > s.limits.MaxLiveBytes {
		return vela.ErrLimit
	}
	if e := s.writeEntry(1, key, data); e != nil {
		return e
	}
	if prev, ok := s.items[key]; ok {
		s.liveBytes -= int64(len(prev))
	}
	s.liveBytes += int64(len(data))
	s.items[key] = append([]byte(nil), data...)
	return nil
}
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return ErrClosed
	}
	if len(key) == 0 || len(key) > maxEntry-32 {
		return vela.ErrLimit
	}
	if e := s.writeEntry(2, key, nil); e != nil {
		return e
	}
	if prev, ok := s.items[key]; ok {
		s.liveBytes -= int64(len(prev))
	}
	delete(s.items, key)
	return nil
}
func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.items[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), b...), true
}
func (s *Store) Count() int { s.mu.RLock(); defer s.mu.RUnlock(); return len(s.items) }

type Match struct {
	Key  string
	Data []byte
}

func (s *Store) FilterEq(field string, want any) ([]Match, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Match, 0)
	for k, b := range s.items {
		v, e := vela.Open(b)
		if e != nil {
			return nil, e
		}
		matched, e := query.Eq(v, want, field)
		if e != nil {
			return nil, e
		}
		if matched {
			out = append(out, Match{Key: k, Data: append([]byte(nil), b...)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Compact rebuilds the append log, keeping only the most recent record per key.
// Rename atomicity is platform/filesystem-dependent; directory sync is used on Unix.
func (s *Store) Compact() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return ErrClosed
	}
	temp := s.path + ".compact.tmp"
	f, e := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	clean := func(err error) error { f.Close(); os.Remove(temp); return err }
	bw := bufio.NewWriter(f)
	if _, e = bw.WriteString("VLS1"); e != nil {
		return clean(e)
	}
	keys := make([]string, 0, len(s.items))
	for k := range s.items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, e = bw.Write(entry(1, key, s.items[key])); e != nil {
			return clean(e)
		}
	}
	if e = bw.Flush(); e != nil {
		return clean(e)
	}
	if e = f.Sync(); e != nil {
		return clean(e)
	}
	if e = f.Close(); e != nil {
		os.Remove(temp)
		return e
	}
	newFile, e := os.OpenFile(temp, os.O_RDWR|os.O_APPEND, 0600)
	if e != nil {
		os.Remove(temp)
		return e
	}
	if e = os.Rename(temp, s.path); e != nil {
		newFile.Close()
		os.Remove(temp)
		return e
	}
	old := s.file
	s.file = newFile
	closeErr := old.Close()
	if dir, de := os.Open(filepath.Dir(s.path)); de == nil {
		syncErr := dir.Sync()
		_ = dir.Close()
		if syncErr != nil {
			return syncErr
		}
	}
	return closeErr
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	e := s.file.Close()
	s.file = nil
	if s.lock != nil {
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		if le := s.lock.Close(); e == nil {
			e = le
		}
		s.lock = nil
	}
	return e
}

// Snapshot writes the key-indexed store's contents to a writer in log format.
func (s *Store) Snapshot(w io.Writer) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.file == nil {
		return ErrClosed
	}
	bw := bufio.NewWriter(w)
	if _, err := bw.WriteString("VLS1"); err != nil {
		return err
	}
	keys := make([]string, 0, len(s.items))
	for k := range s.items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := bw.Write(entry(1, k, s.items[k])); err != nil {
			return err
		}
	}
	return bw.Flush()
}
