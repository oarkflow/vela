// Run with: go run ./examples/http/manual
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/oarkflow/vela"
)

type Message struct {
	ID   uint64 `vela:"id"`
	Text string `vela:"text"`
}

func main() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Header.Get("Content-Type") != vela.HTTPContentType {
			http.Error(w, "expected VELA body", http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
		if err != nil || len(body) > 1<<20 {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		var request Message
		if err := vela.UnmarshalInto(body, &request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reply, err := vela.Marshal(Message{ID: request.ID, Text: "received: " + request.Text})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", vela.HTTPContentType)
		_, _ = w.Write(reply)
	}))
	defer server.Close()

	payload, err := vela.Marshal(Message{ID: 7, Text: "hello"})
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(payload))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", vela.HTTPContentType)
	resp, err := server.Client().Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("HTTP %d", resp.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		log.Fatal(err)
	}
	var reply Message
	if err := vela.UnmarshalInto(responseBody, &reply); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("HTTP %d: ID=%d Text=%q\n", resp.StatusCode, reply.ID, reply.Text)
}
