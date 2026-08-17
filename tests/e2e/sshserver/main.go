package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh"
)

func main() {
	password := os.Getenv("ARGUS_E2E_SSH_PASSWORD")
	if password == "" {
		log.Fatal("ARGUS_E2E_SSH_PASSWORD is required")
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		log.Fatal(err)
	}
	configuration := &ssh.ServerConfig{
		PasswordCallback: func(metadata ssh.ConnMetadata, value []byte) (*ssh.Permissions, error) {
			if metadata.User() != "argus" || string(value) != password {
				return nil, errors.New("authentication rejected")
			}
			return nil, nil
		},
	}
	configuration.AddHostKey(signer)
	listener, err := net.Listen("tcp", ":2222")
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		_ = listener.Close()
	}()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("accept: %v", err)
			continue
		}
		go serve(connection, configuration)
	}
}

func serve(connection net.Conn, configuration *ssh.ServerConfig) {
	server, channels, requests, err := ssh.NewServerConn(connection, configuration)
	if err != nil {
		_ = connection.Close()
		return
	}
	defer server.Close()
	go ssh.DiscardRequests(requests)
	for channel := range channels {
		_ = channel.Reject(ssh.UnknownChannelType, "test server does not provide sessions")
	}
}
