package config

import "testing"

func TestConnectorGatewayRequiresCertificateIssuer(t *testing.T) {
	valid := ConnectorGateway{
		GRPCAddress: ":9443", HealthAddress: ":8081", DatabaseURL: "postgres://argus", RedisURL: "redis://argus",
		InstanceID: "gateway-1", TLSCertificate: "tls.crt", TLSPrivateKey: "tls.key", ClientCABundle: "ca.crt",
		SecretKEKPath: "keyring.json", SystemNamespace: "argus-system", IssuerName: "argus-connector-ca", IssuerGeneration: 1,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Connector Gateway configuration failed: %v", err)
	}
	for name, mutate := range map[string]func(*ConnectorGateway){
		"namespace":  func(value *ConnectorGateway) { value.SystemNamespace = "" },
		"issuer":     func(value *ConnectorGateway) { value.IssuerName = "" },
		"generation": func(value *ConnectorGateway) { value.IssuerGeneration = 0 },
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
