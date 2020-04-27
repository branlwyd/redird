package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"time"

	"github.com/BranLwyd/redird/handler"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

func serve(hostname, email, certDir string, h http.Handler) {
	m := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(hostname),
		Cache:      autocert.DirCache(certDir),
		Email:      email,
	}
	server := &http.Server{
		TLSConfig: &tls.Config{
			PreferServerCipherSuites: true,
			CurvePreferences: []tls.CurveID{
				tls.X25519,
				tls.CurveP256,
			},
			MinVersion:             tls.VersionTLS12,
			SessionTicketsDisabled: true,
			GetCertificate:         m.GetCertificate,
			NextProtos:             []string{"h2", acme.ALPNProto},
		},
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
		Handler:      handler.NewLoggingHandler("https", h),
	}

	log.Printf("Serving")
	log.Fatalf("ListenAndServeTLS: %v", server.ListenAndServeTLS("", ""))
}
