// Command tls-reject is a minimal TCP listener that accepts connections
// and immediately closes them before any TLS handshake bytes can be
// exchanged. Clients attempting a TLS handshake see EOF during the
// first read, which Go's tls.Conn surfaces as a handshake error.
//
// Used by the testenv HandshakeFailure scenario: nginx-based mTLS
// turned out to complete the TLS handshake successfully and only
// reject at HTTP layer (400 "No required SSL certificate was sent"),
// so ProbeTLS classified it as Match. Closing the connection pre-
// handshake is the most direct way to exercise stageHandshakeFailed.
//
// Usage:
//
//	tls-reject                 # binds 127.0.0.1:8445
//	tls-reject 8446            # override port
//
// Lifecycle: runs until killed (SIGINT/SIGTERM).
package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	port := "8445"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}

	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("tls-reject: listening 127.0.0.1:%s (closes every TCP connection pre-handshake)\n", port)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ln.Close()
	}()

	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = c.Close()
	}
}
