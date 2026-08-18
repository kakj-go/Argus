package winrsline

import (
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenCreatesShellOverVerifiedTLS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:5986")
	if err != nil {
		t.Skipf("WinRS test port is unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "argus" || password != "secret" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
		if request.Method != http.MethodPost || request.URL.Path != "/wsman" {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Content-Type") == "" {
			http.Error(writer, "missing content type", http.StatusBadRequest)
			return
		}
		fmt.Fprint(writer, winRSCreateResponse)
	}))
	server.Listener = listener
	server.StartTLS()
	defer server.Close()

	host, _, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := Open(Options{Host: host, Port: 5986, Username: "argus", Password: "secret", CACert: ca})
	if err != nil {
		t.Fatal(err)
	}
	if client == nil || client.shell == nil {
		t.Fatal("WinRS shell was not created")
	}
}

var winRSCreateResponse = strings.Join([]string{
	`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:x="http://schemas.xmlsoap.org/ws/2004/09/transfer" xmlns:w="http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd" xmlns:rsp="http://schemas.microsoft.com/wbem/wsman/1/windows/shell">`,
	`<s:Header><a:Action>http://schemas.xmlsoap.org/ws/2004/09/transfer/CreateResponse</a:Action></s:Header>`,
	`<s:Body><x:ResourceCreated><a:ReferenceParameters><w:SelectorSet><w:Selector Name="ShellId">67A74734-DD32-4F10-89DE-49A060483810</w:Selector></w:SelectorSet></a:ReferenceParameters></x:ResourceCreated>`,
	`<rsp:Shell><rsp:ShellId>67A74734-DD32-4F10-89DE-49A060483810</rsp:ShellId></rsp:Shell></s:Body></s:Envelope>`,
}, "")
