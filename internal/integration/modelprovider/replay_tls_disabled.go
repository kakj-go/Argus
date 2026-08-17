//go:build !m4e2e

package modelprovider

import "net/http"

func configureE2EReplayTLS(*http.Transport) {}
