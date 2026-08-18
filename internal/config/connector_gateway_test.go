package config

import "testing"

func TestConnectorGatewayRequiresCertificateIssuer(t *testing.T) {
	valid := ConnectorGateway{
		GRPCAddress: ":9443", HealthAddress: ":8081", DatabaseURL: "postgres://argus", RedisURL: "redis://argus",
		InstanceID: "gateway-1", TLSCertificate: "tls.crt", TLSPrivateKey: "tls.key", ClientCABundle: "ca.crt",
		SecretKEKPath: "keyring.json", SystemNamespace: "argus-system", IssuerName: "argus-connector-ca", IssuerGeneration: 1,
		RemoteWSSAddress: ":9445", RemoteRPCAddress: ":9446", RemoteOrigin: "https://remote.example.com",
		RemotePeerServerName: "argus-connector-gateway", RemotePeerHeadlessSuffix: "argus-connector-gateway-headless.argus-system.svc", RemotePeerPort: "9446",
		RemoteAllowedOrigins: []string{"https://enterprise.example.com"}, DirectExecutorEndpoint: "argus-direct-executor:9444",
		DirectExecutorServerName: "argus-direct-executor", DirectExecutorTLSCert: "client.crt", DirectExecutorTLSKey: "client.key",
		DirectExecutorCABundle: "direct-ca.crt", DirectExecutorRecipientID: "argus-direct-executor",
		ObjectStoreURL: "https://minio.example.com", ObjectStoreBucket: "remote-recordings", ObjectStoreAccess: "access", ObjectStoreSecret: "secret",
		RemoteUserLimit: 3, RemoteHostLimit: 5, RemoteTenantLimit: 50,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Connector Gateway configuration failed: %v", err)
	}
	for name, mutate := range map[string]func(*ConnectorGateway){
		"namespace":   func(value *ConnectorGateway) { value.SystemNamespace = "" },
		"issuer":      func(value *ConnectorGateway) { value.IssuerName = "" },
		"generation":  func(value *ConnectorGateway) { value.IssuerGeneration = 0 },
		"origin":      func(value *ConnectorGateway) { value.RemoteOrigin = "" },
		"objectstore": func(value *ConnectorGateway) { value.ObjectStoreURL = "" },
		"limits":      func(value *ConnectorGateway) { value.RemoteUserLimit = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			value := valid
			mutate(&value)
			if value.Validate() == nil {
				t.Fatal("invalid cert-manager issuer configuration was accepted")
			}
		})
	}
}
