package tlsmaterial

import (
	"context"
	"crypto/tls"
	"errors"
	"net"

	"google.golang.org/grpc/credentials"
)

type clientCredentials struct {
	material   *Material
	serverName string
}

func ClientCredentials(material *Material, serverName string) (credentials.TransportCredentials, error) {
	if material == nil || serverName == "" {
		return nil, errors.New("dynamic client TLS material and server name are required")
	}
	if _, err := material.ClientConfig(serverName); err != nil {
		return nil, err
	}
	return &clientCredentials{material: material, serverName: serverName}, nil
}

func (credentialsValue *clientCredentials) ClientHandshake(ctx context.Context, _ string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	configuration, err := credentialsValue.material.ClientConfig(credentialsValue.serverName)
	if err != nil {
		return nil, nil, err
	}
	connection := tls.Client(rawConn, configuration)
	if err := connection.HandshakeContext(ctx); err != nil {
		return nil, nil, err
	}
	state := connection.ConnectionState()
	return connection, credentials.TLSInfo{State: state, CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}}, nil
}

func (*clientCredentials) ServerHandshake(net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, nil, errors.New("client TLS credentials cannot perform a server handshake")
}

func (credentialsValue *clientCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "tls", SecurityVersion: "1.3", ServerName: credentialsValue.serverName}
}

func (credentialsValue *clientCredentials) Clone() credentials.TransportCredentials {
	return &clientCredentials{material: credentialsValue.material, serverName: credentialsValue.serverName}
}

func (credentialsValue *clientCredentials) OverrideServerName(serverName string) error {
	if serverName == "" {
		return errors.New("TLS server name is required")
	}
	credentialsValue.serverName = serverName
	return nil
}
