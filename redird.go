package main

import (
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/golang/protobuf/proto"

	pb "github.com/BranLwyd/redird/redird_go_proto"
)

var (
	configFile = flag.String("config", "", "The redird configuration file to use.")
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
	w.Header().Add("Content-Security-Policy", "default-src 'self'")

	shh.h.ServeHTTP(w, r)
}

func NewSecureHeaderHandler(h http.Handler) http.Handler { return secureHeaderHandler{h} }

type urlRedirector struct {
	redirects map[string]string
}

func (u urlRedirector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	targURL, ok := u.redirects[strings.TrimPrefix(r.URL.Path, "/")]
	if !ok {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	http.Redirect(w, r, targURL, http.StatusFound)
}

func main() {
	// Parse & validate configuration.
	flag.Parse()
	if *configFile == "" {
		log.Fatalf("--config is required")
	}
	cfgBytes, err := ioutil.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("Couldn't read config file %q: %v", *configFile, err)
	}
	cfg := &pb.Config{}
	if err := proto.UnmarshalText(string(cfgBytes), cfg); err != nil {
		log.Fatalf("Couldn't parse config file: %v", err)
	}

	redir := urlRedirector{cfg.Redirect}
	serve(cfg.HostName, cfg.Email, cfg.CertDir, NewSecureHeaderHandler(redir))
}
