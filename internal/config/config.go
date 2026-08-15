// Package config loads process configuration from the environment.
package config

import "os"

const defaultHTTPAddress = ":8080"

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
