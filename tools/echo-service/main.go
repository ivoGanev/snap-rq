package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// EchoResponse is what the echo server sends back.
type EchoResponse struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func echoHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	headers := make(map[string]string)
	for name, values := range r.Header {
		headers[name] = strings.Join(values, ", ")
	}

	resp := EchoResponse{
		Method:  r.Method,
		URL:     r.URL.String(),
		Headers: headers,
		Body:    string(body),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func main() {
	port := "18080"
	http.HandleFunc("/", echoHandler)
	fmt.Printf("Echo server listening on http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}
