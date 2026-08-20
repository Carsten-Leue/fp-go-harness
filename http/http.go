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

func AsHttpDeps[R HttpDeps](r R) HttpDeps {
	return r
}

func MakeHttpDeps(c *http.Client) HttpDeps {
	return &httpDeps{c}
}
func MakeDefaultHttpDeps() HttpDeps {
	return MakeHttpDeps(http.DefaultClient)
}
