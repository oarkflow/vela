package vela

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// JSON writes use the same temporary file, fsync, rename and directory fsync
// sequence as WriteFile. Each read benchmark includes opening and reading the
// file. The payload and decoded fields are identical.
func benchmarkJSONAtomicWrite(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".json-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func BenchmarkFileWrite(b *testing.B) {
	dir := b.TempDir()
	b.Run("VLF", func(b *testing.B) {
		path := filepath.Join(dir, "record.vlf")
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := WriteFile(path, benchStruct, FileOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("JSON", func(b *testing.B) {
		path := filepath.Join(dir, "record.json")
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := benchmarkJSONAtomicWrite(path, benchStruct); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkFileReadStruct(b *testing.B) {
	dir := b.TempDir()
	vlfPath := filepath.Join(dir, "record.vlf")
	jsonPath := filepath.Join(dir, "record.json")
	if err := WriteFile(vlfPath, benchStruct, FileOptions{}); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, benchStructJSON, 0600); err != nil {
		b.Fatal(err)
	}
	b.Run("VLF", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var v benchPerson
			if err := UnmarshalFileInto(vlfPath, &v); err != nil {
				b.Fatal(err)
			}
			benchPersonSink = v
		}
	})
	b.Run("JSON", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			data, err := os.ReadFile(jsonPath)
			if err != nil {
				b.Fatal(err)
			}
			var v benchPerson
			if err := json.Unmarshal(data, &v); err != nil {
				b.Fatal(err)
			}
			benchPersonSink = v
		}
	})
}

func BenchmarkFileReadField(b *testing.B) {
	dir := b.TempDir()
	vlfPath := filepath.Join(dir, "record.vlf")
	jsonPath := filepath.Join(dir, "record.json")
	if err := WriteFile(vlfPath, benchStruct, FileOptions{}); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, benchStructJSON, 0600); err != nil {
		b.Fatal(err)
	}
	b.Run("VLF", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			v, err := OpenFileView(vlfPath, FileOptions{})
			if err != nil {
				b.Fatal(err)
			}
			id, ok, err := v.GetUint("id")
			if err != nil || !ok {
				b.Fatal(err)
			}
			benchNum = id
		}
	})
	b.Run("JSON", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			data, err := os.ReadFile(jsonPath)
			if err != nil {
				b.Fatal(err)
			}
			var v struct {
				ID uint64 `json:"id"`
			}
			if err := json.Unmarshal(data, &v); err != nil {
				b.Fatal(err)
			}
			benchNum = v.ID
		}
	})
}
