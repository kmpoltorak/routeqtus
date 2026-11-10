package middleware

import (
	"errors"
	"io"
	"net/http"
	"time"
)

// LimitBody adds a streaming bound without removing any existing, tighter bound.
func LimitBody(w http.ResponseWriter, r *http.Request, limit int64) bool {
	if r.ContentLength > limit {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return false
	}
	r.Body = &limitedBody{ReadCloser: http.MaxBytesReader(w, r.Body, limit), state: Current(r), controller: http.NewResponseController(w)}
	return true
}

type limitedBody struct {
	io.ReadCloser
	state      *State
	controller *http.ResponseController
}

func (b *limitedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		b.state.BodyTooLarge.Store(true)
	}
	if err != nil {
		_ = b.controller.SetReadDeadline(time.Time{})
	}
	return n, err
}
