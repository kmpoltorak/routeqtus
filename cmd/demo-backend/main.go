// The demo backend is not included in release archives.
package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	name := os.Getenv("DEMO_BACKEND_NAME")
	if name == "" {
		name = "backend"
	}
	s := &http.Server{Addr: ":8081", ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: time.Minute, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("routeqtus demo backend: " + name + "\n"))
	})}
	log.Fatal(s.ListenAndServe())
}
