// Run a local demo: go run ./examples/http
// Run a server for curl: go run ./examples/http -serve
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/oarkflow/vela"
)

type Message struct {
	ID   uint64   `vela:"id" json:"id"`
	Text string   `vela:"text" json:"text"`
	Tags []string `vela:"tags,omitempty" json:"tags,omitempty"`
}

func handler(limits vela.Limits) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/echo" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()
		var request Message
		if err := vela.DecodeHTTPRequest(r, &request, limits); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		response := Message{ID: request.ID, Text: "received: " + request.Text, Tags: request.Tags}
		if err := vela.WriteHTTPResponseForRequest(w, r, http.StatusOK, response, limits); err != nil {
			log.Printf("write response: %v", err)
		}
	}
}

func main() {
	serve := flag.Bool("serve", false, "listen on 127.0.0.1:8080 for curl")
	flag.Parse()
	limits := vela.Limits{MaxBytes: 1 << 20} // 1 MiB HTTP body limit
	if *serve {
		log.Print("listening on http://127.0.0.1:8080/echo")
		log.Fatal(http.ListenAndServe("127.0.0.1:8080", handler(limits)))
	}

	server := httptest.NewServer(handler(limits))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request := Message{ID: 42, Text: "hello", Tags: []string{"demo"}}
	req, err := vela.NewHTTPRequest(ctx, http.MethodPost, server.URL+"/echo", request, limits)
	if err != nil {
		log.Fatal(err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("HTTP %d: %s", resp.StatusCode, body)
	}
	var reply Message
	if err := vela.DecodeHTTPResponse(resp, &reply, limits); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("HTTP %d: ID=%d Text=%q Tags=%v\n", resp.StatusCode, reply.ID, reply.Text, reply.Tags)
}
