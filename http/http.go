package http

import "net/http"

type HttpDeps interface {
	GetHttpClient() *http.Client
}

type httpDeps struct {
	client *http.Client
}

func (h *httpDeps) GetHttpClient() *http.Client {
	return h.client
}

// AsHttpDeps is wiring: it narrows any wider dependencies to [HttpDeps], for
// use with [reader.Local] and [effect.Local].
func AsHttpDeps[R HttpDeps](r R) HttpDeps {
	return r
}

// MakeHttpDeps is wiring: it combines an HTTP client into [HttpDeps].
func MakeHttpDeps(c *http.Client) HttpDeps {
	return &httpDeps{c}
}

// MakeDefaultHttpDeps is wiring: it builds [HttpDeps] over [http.DefaultClient].
func MakeDefaultHttpDeps() HttpDeps {
	return MakeHttpDeps(http.DefaultClient)
}
