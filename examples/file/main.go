package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/oarkflow/vela"
	"github.com/oarkflow/vela/batch"
)

func main() {
	dir, err := os.MkdirTemp("", "vela-file-demo-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "person.vlf")
	value := map[string]any{"id": uint64(300), "age": uint64(30), "active": true, "name": "Asha"}
	dict := vela.NewDictionary("id", "age", "active", "name")
	if err := vela.WriteFile(path, value, vela.FileOptions{Dictionary: dict}); err != nil {
		log.Fatal(err)
	}
	view, err := vela.OpenFileView(path, vela.FileOptions{})
	if err != nil {
		log.Fatal(err)
	}
	name, exists, err := view.GetString("name")
	if err != nil || !exists {
		log.Fatal(err)
	}
	fmt.Println("borrowed file field:", name)
	full, err := vela.UnmarshalFile(path)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("full record:", full)
	rows := []map[string]any{{"status": "new", "id": uint64(1)}, {"status": "paid", "id": uint64(2)}}
	batchPath := filepath.Join(dir, "rows.vlf")
	if err := batch.WriteFile(batchPath, rows); err != nil {
		log.Fatal(err)
	}
	page, err := batch.ReadFile(batchPath)
	if err != nil {
		log.Fatal(err)
	}
	hits, err := page.FilterEq("status", "paid")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("paid row indexes:", hits)
}
