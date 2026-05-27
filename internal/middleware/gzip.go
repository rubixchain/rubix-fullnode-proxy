package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"

	"rubix-fullnode-proxy/internal/constants"
)

type gzipResponseWriter struct {
	http.ResponseWriter
	gzWriter   *gzip.Writer
	statusCode int
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	g.statusCode = code
	g.ResponseWriter.Header().Del(constants.HeaderContentLength)
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if g.statusCode == 0 {
		g.statusCode = http.StatusOK
	}
	return g.gzWriter.Write(b)
}

var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

func Gzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get(constants.HeaderAcceptEncoding), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w)
		defer func() {
			gz.Close()
			gzipWriterPool.Put(gz)
		}()

		w.Header().Set(constants.HeaderContentEncoding, "gzip")
		w.Header().Set(constants.HeaderVary, constants.HeaderAcceptEncoding)
		w.Header().Del(constants.HeaderContentLength)

		gzw := &gzipResponseWriter{
			ResponseWriter: w,
			gzWriter:       gz,
		}

		next.ServeHTTP(gzw, r)
	})
}
