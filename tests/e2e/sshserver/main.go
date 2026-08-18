package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

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
		if channel.ChannelType() != "session" {
			_ = channel.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		accepted, channelRequests, err := channel.Accept()
		if err != nil {
			continue
		}
		go serveSession(accepted, channelRequests)
	}
}

func serveSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	var shellOnce sync.Once
	for request := range requests {
		switch request.Type {
		case "pty-req", "window-change":
			_ = request.Reply(true, nil)
		case "shell":
			_ = request.Reply(true, nil)
			shellOnce.Do(func() { runShell(channel) })
			return
		default:
			_ = request.Reply(false, nil)
		}
	}
}

func runShell(channel ssh.Channel) {
	_, _ = channel.Write([]byte("Argus M6 SSH PTY ready\r\n$ "))
	reader := bufio.NewReader(channel)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
		switch {
		case command == "exit":
			_, _ = channel.Write([]byte("logout\r\n"))
			return
		case command == "whoami":
			_, _ = channel.Write([]byte("argus\r\n"))
		case strings.HasPrefix(command, "echo "):
			_, _ = channel.Write([]byte(strings.TrimPrefix(command, "echo ") + "\r\n"))
		case command == "stream":
			for index := 0; index < 45; index++ {
				_, _ = channel.Write([]byte("stream-output\r\n"))
				time.Sleep(time.Second)
			}
		case command == "":
		default:
			_, _ = channel.Write([]byte("executed: " + command + "\r\n"))
		}
		_, _ = channel.Write([]byte("$ "))
	}
}
