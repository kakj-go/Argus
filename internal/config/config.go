// Package config loads process configuration from the environment.
package config

import "os"

const defaultHTTPAddress = ":8080"
const defaultHealthAddress = ":8081"

type Server struct {
	Address string
}

func LoadServer() Server {
	address := os.Getenv("ARGUS_HTTP_ADDRESS")
	if address == "" {
		address = defaultHTTPAddress
	}

	return Server{Address: address}
}

func LoadHealthAddress() string {
	address := os.Getenv("ARGUS_HEALTH_ADDRESS")
	if address == "" {
		address = defaultHealthAddress
	}
	return address
}
