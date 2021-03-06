// Package logrusmiddleware is a simple net/http middleware for logging
// using logrus
package logrusmiddleware

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/sirupsen/logrus"
)

type (
	// Middleware is a middleware handler for HTTP logging
	Middleware struct {
		// Logger is the log.Logger instance used to log messages with the Logger middleware
		Logger *logrus.Logger
		// Name is the name of the application as recorded in latency metrics
		Name string
	}

	responseData struct {
		status int
		size   int
	}

	// Handler is the actual middleware that handles logging
	Handler struct {
		http.ResponseWriter
		m            *Middleware
		handler      http.Handler
		component    string
		responseData *responseData
	}
)

func (h *Handler) newResponseData() *responseData {
	return &responseData{
		status: 0,
		size:   0,
	}
}

// Handler create a new handler. component, if set, is emitted in the log messages.
func (m *Middleware) Handler(h http.Handler, component string) *Handler {
	return &Handler{
		m:         m,
		handler:   h,
		component: component,
	}
}

// Hijack implements http.Hijacker. It simply wraps the underlying
// ResponseWriter's Hijack method if there is one, or returns an error.
func (h *Handler) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := h.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("Parent ResponseWriter is no Hijacker")
}

// Write is a wrapper for the "real" ResponseWriter.Write
func (h *Handler) Write(b []byte) (int, error) {
	if h.responseData.status == 0 {
		// The status will be StatusOK if WriteHeader has not been called yet
		h.responseData.status = http.StatusOK
	}
	size, err := h.ResponseWriter.Write(b)
	h.responseData.size += size
	return size, err
}

// WriteHeader is a wrapper around ResponseWriter.WriteHeader
func (h *Handler) WriteHeader(s int) {
	h.ResponseWriter.WriteHeader(s)
	h.responseData.status = s
}

// Header is a wrapper around ResponseWriter.Header
func (h *Handler) Header() http.Header {
	return h.ResponseWriter.Header()
}

// ServeHTTP calls the "real" handler and logs using the logger
func (h *Handler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	start := time.Now()

	h = h.m.Handler(h.handler, h.component)
	h.ResponseWriter = rw
	h.responseData = h.newResponseData()

	safeURI := ""
	uri, err := url.ParseRequestURI(r.RequestURI)
	if err != nil {
		safeURI = ""
	} else {
		query := uri.Query()
		changes := false
		if query.Get("password") != "" {
			query.Set("password", "****")
			changes = true
		}
		if query.Get("pw") != "" {
			query.Set("pw", "****")
			changes = true
		}
		if changes == true {
			uri.RawQuery = query.Encode()
			safeURI = uri.String()
		} else {
			safeURI = r.RequestURI
		}
	}

	fields := logrus.Fields{
		"method":     r.Method,
		"request":    safeURI,
		"remote":     r.RemoteAddr,
		"referer":    r.Referer(),
		"user-agent": r.UserAgent(),
	}

	if h.m.Name != "" {
		fields["name"] = h.m.Name
	}

	if h.component != "" {
		fields["component"] = h.component
	}

	info := func(msg string) {
		if l := h.m.Logger; l != nil {
			l.WithFields(fields).Info(msg)
		} else {
			logrus.WithFields(fields).Info(msg)
		}
	}
	info("starting request")

	h.handler.ServeHTTP(h, r)

	latency := time.Since(start)
	fields["duration"] = float64(latency.Nanoseconds()) / float64(1000)

	status := h.responseData.status
	if status == 0 {
		status = 200
	}
	fields["status"] = status
	fields["size"] = h.responseData.size

	info("completed handling request")
}
