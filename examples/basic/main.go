package main

import (
	"fmt"
	"log"

	"github.com/oarkflow/vela"
	"github.com/oarkflow/vela/batch"
)

func main() {
	record := map[string]any{"id": uint64(300), "name": "Asha", "active": true}
	data, err := vela.Marshal(record)
	if err != nil {
		log.Fatal(err)
	}
	v, err := vela.Open(data)
	if err != nil {
		log.Fatal(err)
	}
	id, _, err := v.GetUint("id")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("VELA payload=%d bytes, id=%d\n", len(data), id)
	d := vela.NewDictionary("id", "name", "active")
	compact, err := vela.MarshalWithDictionary(record, d)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Dictionary payload=%d bytes\n", len(compact))
	rows := []map[string]any{{"id": uint64(1), "status": "paid"}, {"id": uint64(2), "status": "new"}}
	encoded, err := batch.Marshal(rows)
	if err != nil {
		log.Fatal(err)
	}
	page, err := batch.Open(encoded)
	if err != nil {
		log.Fatal(err)
	}
	found, err := page.FilterEq("status", "paid")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Batch bytes=%d, paid rows=%v\n", len(encoded), found)
}
