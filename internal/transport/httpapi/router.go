// Package httpapi contains the public HTTP transport for argus-server.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kakj-go/Argus/internal/buildinfo"
)

type response struct {
	Service string `json:"service,omitempty"`
	Version string `json:"version,omitempty"`
	Status  string `json:"status,omitempty"`
	Locale  string `json:"locale,omitempty"`
}

type localeContextKey struct{}

const DefaultLocale = "zh-CN"

func NewRouter() http.Handler {
	router := chi.NewRouter()
	router.Use(localeMiddleware)
	router.Get("/", serviceInfo)
	router.Get("/healthz", health)
	router.Get("/readyz", ready)
	return router
}

func serviceInfo(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, response{
		Service: "argus-server",
		Version: buildinfo.Version,
		Locale:  LocaleFromContext(request.Context()),
	})
}

func LocaleFromContext(ctx context.Context) string {
	if locale, ok := ctx.Value(localeContextKey{}).(string); ok {
		return locale
	}
	return DefaultLocale
}

func localeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		locale := negotiateLocale(request.Header.Get("Accept-Language"))
		writer.Header().Set("Content-Language", locale)
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), localeContextKey{}, locale)))
	})
}

func negotiateLocale(header string) string {
	for _, candidate := range strings.Split(header, ",") {
		language := strings.ToLower(strings.TrimSpace(strings.SplitN(candidate, ";", 2)[0]))
		switch {
		case strings.HasPrefix(language, "en"):
			return "en-US"
		case strings.HasPrefix(language, "zh"):
			return "zh-CN"
		}
	}
	return DefaultLocale
}

func health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, response{Status: "ok"})
}

func ready(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, response{Status: "ready"})
}

func writeJSON(writer http.ResponseWriter, status int, body response) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
