package vela

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

// File v1: "VLF1" | flags(1) | [dictLen uvarint, dictBytes] |
// payloadLen uvarint | payload | crc32c(flags..payload).
// The portable file envelope is distinct from naked VELA values and network frames.
// Unknown flags and extra trailing bytes are rejected by ReadFile.
const fileMagic = "VLF1"
const fileDictionaryFlag = 1

var fileCRC = crc32.MakeTable(crc32.Castagnoli)

type FileOptions struct {
	Limits Limits
	// Dictionary is used for writing and embedded in the file. For reading it
	// is not needed: files carrying dictionary IDs are self-contained.
	Dictionary *Dictionary
	// Permissions on *new* files; defaults to owner read/write (0600).
	Perm os.FileMode
}

// MarshalFile atomically writes a value to path, synchronizing file contents
// before rename and synchronizing the containing directory when supported.
// Existing file permissions are not preserved; temporary files are private.
func MarshalFile(path string, value any) error { return WriteFile(path, value, FileOptions{}) }
func UnmarshalFile(path string) (any, error)   { return ReadFile(path, FileOptions{}) }

// UnmarshalFileInto reads a .vlf file into a typed destination.
func UnmarshalFileInto(path string, dst any) error {
	return ReadFileInto(path, FileOptions{}, dst)
}

func fileBytes(value any, options FileOptions) ([]byte, error) {
	limits := options.Limits.normalized()
	var payload []byte
	var err error
	payload, err = MarshalLimited(value, options.Dictionary, limits)
	if err != nil {
		return nil, err
	}
	if len(payload) > limits.MaxBytes {
		return nil, ErrLimit
	}
	b := make([]byte, 0, len(payload)+64)
	b = append(b, fileMagic...)
	flags := byte(0)
	if options.Dictionary != nil {
		flags = fileDictionaryFlag
	}
	b = append(b, flags)
	if flags != 0 {
		d, err := options.Dictionary.MarshalBinary()
		if err != nil {
			return nil, err
		}
		if len(d) > limits.MaxBytes || len(d)+len(payload) > limits.MaxBytes {
			return nil, ErrLimit
		}
		b = binary.AppendUvarint(b, uint64(len(d)))
		b = append(b, d...)
	}
	b = binary.AppendUvarint(b, uint64(len(payload)))
	b = append(b, payload...)
	if len(b)+4 > limits.MaxBytes {
		return nil, ErrLimit
	}
	crc := crc32.Checksum(b[len(fileMagic):], fileCRC)
	return binary.LittleEndian.AppendUint32(b, crc), nil
}

// WriteTo writes one checksummed, self-contained file-format object to an
// arbitrary writer. A partial writer error is propagated.
func WriteTo(w io.Writer, value any, options FileOptions) (int64, error) {
	data, err := fileBytes(value, options)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(w, bytes.NewReader(data))
	return n, err
}

// ReadFrom consumes one VELA file envelope from r, returning exactly one value.
// For byte streams containing multiple envelopes, subsequent bytes are left
// unread. Use ReadFile to reject trailing data in a standalone file.
func ReadFrom(r io.Reader, options FileOptions) (any, error) {
	payload, dict, err := readFileEnvelope(r, options)
	if err != nil {
		return nil, err
	}
	return UnmarshalWithLimits(payload, dict, options.Limits)
}

// ReadFromInto consumes one VLF1 envelope and decodes it into dst.
func ReadFromInto(r io.Reader, options FileOptions, dst any) error {
	payload, dict, err := readFileEnvelope(r, options)
	if err != nil {
		return err
	}
	return UnmarshalIntoWithLimits(payload, dict, options.Limits, dst)
}

func readFileEnvelope(in io.Reader, options FileOptions) ([]byte, *Dictionary, error) {
	limits := options.Limits.normalized()
	var magic [4]byte
	if _, err := io.ReadFull(in, magic[:]); err != nil {
		return nil, nil, err
	}
	if string(magic[:]) != fileMagic {
		return nil, nil, ErrInvalid
	}
	var flag [1]byte
	if _, err := io.ReadFull(in, flag[:]); err != nil {
		return nil, nil, err
	}
	if flag[0]&^fileDictionaryFlag != 0 {
		return nil, nil, ErrInvalid
	}
	h := crc32.New(fileCRC)
	_, _ = h.Write(flag[:])
	// Reads canonical varints using only the needed bytes from the underlying stream.
	readVar := func() (uint64, error) {
		var raw [10]byte
		for i := 0; i < len(raw); i++ {
			if _, err := io.ReadFull(in, raw[i:i+1]); err != nil {
				return 0, err
			}
			_, _ = h.Write(raw[i : i+1])
			if raw[i] < 0x80 {
				value, n := binary.Uvarint(raw[:i+1])
				if n != i+1 || (i > 0 && raw[i] == 0) {
					return 0, ErrInvalid
				}
				return value, nil
			}
		}
		return 0, ErrInvalid
	}
	consumed := 5
	var dict *Dictionary
	if flag[0]&fileDictionaryFlag != 0 {
		n, err := readVar()
		if err != nil {
			return nil, nil, err
		}
		consumed += binary.PutUvarint(make([]byte, 10), n)
		if consumed > limits.MaxBytes-4 || n > uint64(limits.MaxBytes-consumed-4) {
			return nil, nil, ErrLimit
		}
		d := make([]byte, int(n))
		if _, err = io.ReadFull(in, d); err != nil {
			return nil, nil, err
		}
		_, _ = h.Write(d)
		consumed += len(d)
		dict, err = ParseDictionaryWithLimits(d, limits)
		if err != nil {
			return nil, nil, err
		}
	}
	n, err := readVar()
	if err != nil {
		return nil, nil, err
	}
	consumed += binary.PutUvarint(make([]byte, 10), n)
	if consumed > limits.MaxBytes-4 || n > uint64(limits.MaxBytes-consumed-4) {
		return nil, nil, ErrLimit
	}
	payload := make([]byte, int(n))
	if _, err = io.ReadFull(in, payload); err != nil {
		return nil, nil, err
	}
	_, _ = h.Write(payload)
	var sum [4]byte
	if _, err = io.ReadFull(in, sum[:]); err != nil {
		return nil, nil, err
	}
	if binary.LittleEndian.Uint32(sum[:]) != h.Sum32() {
		return nil, nil, ErrInvalid
	}
	return payload, dict, nil
}

func ReadFile(path string, options FileOptions) (any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	limits := options.Limits.normalized()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > int64(limits.MaxBytes) {
		return nil, ErrLimit
	}
	v, err := ReadFrom(f, options)
	if err != nil {
		return nil, err
	}
	if err := rejectTrailing(f); err != nil {
		return nil, err
	}
	return v, nil
}

// ReadFileInto reads a complete .vlf file into dst and rejects trailing data.
func ReadFileInto(path string, options FileOptions, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	limits := options.Limits.normalized()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > int64(limits.MaxBytes) {
		return ErrLimit
	}
	payload, dict, err := readFileEnvelope(f, options)
	if err != nil {
		return err
	}
	if err := rejectTrailing(f); err != nil {
		return err
	}
	return UnmarshalIntoWithLimits(payload, dict, limits, dst)
}

func WriteFile(path string, value any, options FileOptions) (err error) {
	data, err := fileBytes(value, options)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".vela-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer func() {
		_ = f.Close()
		if err != nil {
			_ = os.Remove(temp)
		}
	}()
	perm := options.Perm
	if perm == 0 {
		perm = 0600
	}
	if err = f.Chmod(perm.Perm()); err != nil {
		return err
	}
	if _, err = io.Copy(f, bytes.NewReader(data)); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp, path); err != nil {
		return err
	}
	if d, e := os.Open(dir); e == nil {
		e = d.Sync()
		_ = d.Close()
		if e != nil {
			return fmt.Errorf("vela: directory sync: %w", e)
		}
	}
	return nil
}

// OpenFileView opens a checksummed file with a borrowed-like View backed by an
// owned byte slice; the view retains the buffer for its own lifetime. Unlike
// UnmarshalFile, it does not materialize a map or array of Go values.
func OpenFileView(path string, options FileOptions) (View, error) {
	f, err := os.Open(path)
	if err != nil {
		return View{}, err
	}
	defer f.Close()
	limits := options.Limits.normalized()
	info, err := f.Stat()
	if err != nil {
		return View{}, err
	}
	if info.Size() > int64(limits.MaxBytes) {
		return View{}, ErrLimit
	}
	payload, dict, err := readFileEnvelope(f, options)
	if err != nil {
		return View{}, err
	}
	if err = rejectTrailing(f); err != nil {
		return View{}, err
	}
	return OpenWithLimits(payload, dict, limits)
}
func rejectTrailing(r io.Reader) error {
	var extra [1]byte
	n, err := r.Read(extra[:])
	if n > 0 {
		return ErrInvalid
	}
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}
