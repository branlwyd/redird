package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/BranLwyd/redird/assets"
)

type loggingHandler struct {
	h       http.Handler
	logName string
}

func (lh loggingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip port from remote address, as the client port is not useful information.
	ra := r.RemoteAddr
	idx := strings.LastIndex(ra, ":")
	if idx != -1 {
		ra = ra[:idx]
	}
	log.Printf("[%s] %s requested %s", lh.logName, ra, r.RequestURI)
	lh.h.ServeHTTP(w, r)
}

func NewLoggingHandler(logName string, h http.Handler) http.Handler {
	return loggingHandler{
		h:       h,
		logName: logName,
	}
}

type secureHeaderHandler struct {
	h http.Handler
}

func (shh secureHeaderHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
	w.Header().Add("X-Frame-Options", "DENY")
	w.Header().Add("X-XSS-Protection", "1; mode=block")
	w.Header().Add("X-Content-Type-Options", "nosniff")
	w.Header().Add("Content-Security-Policy", "default-src 'self'; style-src-elem 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com")

	shh.h.ServeHTTP(w, r)
}

func NewSecureHeaderHandler(h http.Handler) http.Handler { return secureHeaderHandler{h} }

// staticHandler serves static content from memory.
type staticHandler struct {
	content     []byte
	contentType string
	tag         string
}

func NewStatic(content []byte, contentType string) http.Handler {
	h := sha256.Sum256(content)
	return staticHandler{
		content:     content,
		contentType: contentType,
		tag:         fmt.Sprintf(`"%s"`, base64.RawURLEncoding.EncodeToString(h[:])),
	}
}

func NewAsset(name, contentType string) (http.Handler, error) {
	asset, ok := assets.Asset[name]
	if !ok {
		return nil, fmt.Errorf("no such asset %q", name)
	}
	return NewStatic(asset, contentType), nil
}

func (sh staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", sh.contentType)
	w.Header().Set("ETag", sh.tag)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(sh.content))
}

func Must(h http.Handler, err error) http.Handler {
	if err != nil {
		panic(fmt.Sprintf("Could not create HTTP handler: %v", err))
	}
	return h
}
