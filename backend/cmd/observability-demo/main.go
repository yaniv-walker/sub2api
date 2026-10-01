// observability-demo exercises the request instrumentation against a local
// mock upstream. It has no database, credential or paid API dependency.
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func main() {
	port := flag.Int("port", 18080, "loopback TCP port")
	observe := flag.Bool("observe", false, "enable observability for this isolated demo")
	flag.Parse()
	if err := logger.Init(logger.InitOptions{
		Level: "info", Format: "json", ServiceName: "observability-demo", Environment: "local",
		Output: logger.OutputOptions{ToStdout: true},
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/unavailable" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "mock upstream success")
	}))
	defer upstream.Close()
	upstreamClient := repository.NewHTTPUpstream(&config.Config{})
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(middleware.Recovery(), middleware.RequestLogger(), middleware.ClientRequestID())
	if *observe {
		router.Use(middleware.RequestObservability())
	}
	router.Use(middleware.Logger())
	router.GET("/demo/plain", func(c *gin.Context) { c.String(http.StatusOK, "plain response\n") })
	router.GET("/demo/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		for i := range 4 {
			select {
			case <-c.Request.Context().Done():
				return
			case <-time.After(300 * time.Millisecond):
			}
			if _, err := fmt.Fprintf(c.Writer, "data: chunk-%d\n\n", i+1); err != nil {
				return
			}
			c.Writer.Flush()
		}
	})
	router.GET("/demo/retry", func(c *gin.Context) {
		// This is a fixed demo sequence, not the production failover policy.
		for _, path := range []string{"/unavailable", "/success"} {
			req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstream.URL+path, nil)
			if err != nil {
				c.Status(http.StatusInternalServerError)
				return
			}
			resp, err := upstreamClient.Do(req, "", 0, 1)
			if err != nil {
				c.Status(http.StatusBadGateway)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		c.String(http.StatusOK, "recovered after one mock 503\n")
	})
	address := net.JoinHostPort("127.0.0.1", fmt.Sprint(*port))
	fmt.Fprintf(os.Stderr, "Local demo: http://%s/demo/stream (observe=%t)\n", address, *observe)
	if err := (&http.Server{Addr: address, Handler: router, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
