package groupcache

import (
	"context"
	"net/http"
)

type HttpTransportConfig struct {
	HTTPTransport func(context.Context) http.RoundTripper
	HTTPContext   func(*http.Request) context.Context
	BasePath      string
}

type Transport interface {
}

type HttpTransport struct {
}

func (t HttpTransport) ServeHTTP(writer http.ResponseWriter, r *http.Request) {
	//TODO implement me
	panic("implement me")
}

func NewHttpTransport(config HttpTransportConfig) *HttpTransport {
	return &HttpTransport{}
}
