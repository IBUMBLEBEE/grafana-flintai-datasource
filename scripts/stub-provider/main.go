// Stub OpenAI-compatible provider for local/E2E use. No billable upstream calls.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
)

func main() {
	addr := envOr("STUB_PROVIDER_ADDR", ":18080")
	var chatHits atomic.Int64
	var modelHits atomic.Int64

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":        true,
			"modelHits": modelHits.Load(),
			"chatHits":  chatHits.Load(),
		})
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		modelHits.Add(1)
		writeModels(w)
	})
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		modelHits.Add(1)
		writeModels(w)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		chatHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"role": "assistant", "content": "stub"},
			}},
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			chatHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]string{"role": "assistant", "content": "stub"},
				}},
			})
			return
		}
		http.NotFound(w, r)
	})

	log.Printf("stub provider listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func writeModels(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data": []map[string]string{
			{"id": "stub-chat", "object": "model", "owned_by": "stub"},
			{"id": "stub-reasoner", "object": "model", "owned_by": "stub"},
		},
	})
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
