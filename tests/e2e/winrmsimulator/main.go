package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

const (
	shellID   = "67A74734-DD32-4F10-89DE-49A060483810"
	commandID = "1A6DEE6B-EC68-4DD6-87E9-030C0048ECC4"
)

type simulator struct {
	username string
	password string

	mu      sync.Mutex
	command string
	last    string
}

func main() {
	address := env("ARGUS_E2E_WINRS_ADDRESS", ":5986")
	certificate := env("ARGUS_E2E_WINRS_CERT", "/tls/tls.crt")
	key := env("ARGUS_E2E_WINRS_KEY", "/tls/tls.key")
	handler := &simulator{
		username: env("ARGUS_E2E_WINRS_USERNAME", "argus"),
		password: env("ARGUS_E2E_WINRS_PASSWORD", "M6-e2e-winrs-password"),
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 10 * 1e9}
	log.Printf("WinRS TLS simulator listening on %s", address)
	log.Fatal(server.ListenAndServeTLS(certificate, key))
}

func (server *simulator) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	username, password, ok := request.BasicAuth()
	if !ok || username != server.username || password != server.password {
		writer.Header().Set("WWW-Authenticate", `Basic realm="Argus M6 E2E"`)
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	if request.URL.Path != "/wsman" || request.Method != http.MethodPost {
		http.NotFound(writer, request)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil {
		http.Error(writer, "invalid request", http.StatusBadRequest)
		return
	}
	value := string(body)
	writer.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
	switch {
	case strings.Contains(value, "transfer/Create"):
		fmt.Fprint(writer, createShellResponse())
	case strings.Contains(value, "shell/Command"):
		server.mu.Lock()
		server.command = value
		server.last = value
		server.mu.Unlock()
		fmt.Fprint(writer, executeCommandResponse())
	case strings.Contains(value, "shell/Receive"):
		server.mu.Lock()
		command := server.command
		server.command = ""
		server.mu.Unlock()
		stdout, stderr, exitCode := commandResult(command)
		fmt.Fprint(writer, outputResponse(stdout, stderr, exitCode))
	default:
		fmt.Fprint(writer, emptyResponse())
	}
}

func commandResult(command string) (string, string, int) {
	switch {
	case strings.Contains(command, "Write-Error"), strings.Contains(command, "m6-fail"):
		return "", "m6-winrs-command-failed\r\n", 1
	case strings.Contains(command, "whoami"):
		return "argus\\m6-e2e\r\n", "", 0
	default:
		return "argus-winrs-e2e-ok\r\n", "", 0
	}
}

func createShellResponse() string {
	return envelope("http://schemas.xmlsoap.org/ws/2004/09/transfer/CreateResponse", `<x:ResourceCreated><a:Address>https://127.0.0.1:5986/wsman</a:Address><a:ReferenceParameters><w:ResourceURI>http://schemas.microsoft.com/wbem/wsman/1/windows/shell/cmd</w:ResourceURI><w:SelectorSet><w:Selector Name="ShellId">`+shellID+`</w:Selector></w:SelectorSet></a:ReferenceParameters></x:ResourceCreated><rsp:Shell><rsp:ShellId>`+shellID+`</rsp:ShellId></rsp:Shell>`)
}

func executeCommandResponse() string {
	return envelope("http://schemas.microsoft.com/wbem/wsman/1/windows/shell/CommandResponse", `<rsp:CommandResponse><rsp:CommandId>`+commandID+`</rsp:CommandId></rsp:CommandResponse>`)
}

func outputResponse(stdout, stderr string, exitCode int) string {
	streams := ""
	if stdout != "" {
		streams += `<rsp:Stream Name="stdout" CommandId="` + commandID + `">` + base64.StdEncoding.EncodeToString([]byte(stdout)) + `</rsp:Stream>`
	}
	if stderr != "" {
		streams += `<rsp:Stream Name="stderr" CommandId="` + commandID + `">` + base64.StdEncoding.EncodeToString([]byte(stderr)) + `</rsp:Stream>`
	}
	return envelope("http://schemas.microsoft.com/wbem/wsman/1/windows/shell/ReceiveResponse", `<rsp:ReceiveResponse>`+streams+`<rsp:CommandState CommandId="`+commandID+`" State="http://schemas.microsoft.com/wbem/wsman/1/windows/shell/CommandState/Done"><rsp:ExitCode>`+fmt.Sprint(exitCode)+`</rsp:ExitCode></rsp:CommandState></rsp:ReceiveResponse>`)
}

func emptyResponse() string {
	return envelope("http://schemas.xmlsoap.org/ws/2004/09/transfer/DeleteResponse", "")
}

func envelope(action, body string) string {
	return `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:x="http://schemas.xmlsoap.org/ws/2004/09/transfer" xmlns:w="http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd" xmlns:rsp="http://schemas.microsoft.com/wbem/wsman/1/windows/shell"><s:Header><a:Action>` + action + `</a:Action><a:MessageID>uuid:11111111-1111-1111-1111-111111111111</a:MessageID><a:To>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</a:To><a:RelatesTo>uuid:22222222-2222-2222-2222-222222222222</a:RelatesTo></s:Header><s:Body>` + body + `</s:Body></s:Envelope>`
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
