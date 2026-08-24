package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"

	argusopenapi "github.com/kakj-go/Argus/api/openapi"
)

type openAPIValidationError struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	MessageKey string         `json:"message_key"`
	Params     map[string]any `json:"params,omitempty"`
	RequestID  string         `json:"request_id"`
	Retryable  bool           `json:"retryable"`
}

var (
	openAPIRouterOnce        sync.Once
	openAPIRouter            routers.Router
	openAPIRouterErr         error
	errRequestBodyNotAllowed = errors.New("request body not allowed for this request")
	requiredFieldRE          = regexp.MustCompile(`property "([A-Za-z0-9_.-]+)" is missing`)
	safeFieldRE              = regexp.MustCompile(`^[A-Za-z0-9_.\[\]-]+$`)
)

func loadOpenAPIRouter() (routers.Router, error) {
	openAPIRouterOnce.Do(func() {
		document, err := openapi3.NewLoader().LoadFromData(argusopenapi.BundledJSON)
		if err != nil {
			openAPIRouterErr = err
			return
		}
		// Redocly names bundled JSON-Schema $defs components with a '$'. The
		// references are already resolved by the loader, while kin-openapi's
		// document validator rejects '$' in component map keys. Removing only
		// those aliases preserves the resolved schemas used by route validation.
		for name := range document.Components.Schemas {
			if !safeFieldRE.MatchString(name) {
				delete(document.Components.Schemas, name)
			}
		}
		openAPIRouter, openAPIRouterErr = legacyrouter.NewRouter(document, openapi3.IsOpenAPI31OrLater())
	})
	return openAPIRouter, openAPIRouterErr
}

func openAPIRequestValidationMiddleware(next http.Handler) http.Handler {
	router, err := loadOpenAPIRouter()
	if err != nil {
		panic("load bundled OpenAPI contract: " + err.Error())
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.URL.Path, "/api/v1/") {
			next.ServeHTTP(writer, request)
			return
		}
		route, pathParams, routeErr := router.FindRoute(request)
		if routeErr != nil {
			next.ServeHTTP(writer, request)
			return
		}
		if route.Operation.RequestBody == nil && requestHasBody(request) {
			writeOpenAPIValidationError(writer, request, &openapi3filter.RequestError{
				RequestBody: nil,
				Err:         errRequestBodyNotAllowed,
			})
			return
		}
		validationInput := &openapi3filter.RequestValidationInput{
			Request:    request,
			PathParams: pathParams,
			Route:      route,
			Options: &openapi3filter.Options{
				AuthenticationFunc:  openapi3filter.NoopAuthenticationFunc,
				SkipSettingDefaults: true,
			},
		}
		if validationErr := openapi3filter.ValidateRequest(request.Context(), validationInput); validationErr != nil {
			writeOpenAPIValidationError(writer, request, validationErr)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func requestHasBody(request *http.Request) bool {
	return request.Body != nil && request.Body != http.NoBody && (request.ContentLength != 0 || len(request.TransferEncoding) > 0)
}

func writeOpenAPIValidationError(writer http.ResponseWriter, request *http.Request, validationErr error) {
	params := safeValidationParams(validationErr)
	message := "请求参数未通过校验，请检查标记字段后重试。"
	if LocaleFromContext(request.Context()) == "en-US" {
		message = "The request parameters are invalid. Check the indicated field and try again."
	}
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = "server-generated-request"
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(writer).Encode(openAPIValidationError{
		Code:       "INVALID_ARGUMENT",
		Message:    message,
		MessageKey: "errors.common.invalid_argument",
		Params:     params,
		RequestID:  requestID,
		Retryable:  false,
	})
}

func safeValidationParams(validationErr error) map[string]any {
	params := map[string]any{"rule": "invalid"}
	if errors.Is(validationErr, errRequestBodyNotAllowed) {
		params["rule"] = "unexpected_body"
		return params
	}
	var requestErr *openapi3filter.RequestError
	if errors.As(validationErr, &requestErr) && requestErr.Parameter != nil {
		if field := safeFieldName(requestErr.Parameter.Name); field != "" {
			params["field"] = field
		}
	}
	var schemaErr *openapi3.SchemaError
	if !errors.As(validationErr, &schemaErr) {
		if errors.Is(validationErr, openapi3filter.ErrInvalidRequired) {
			params["rule"] = "required"
		}
		return params
	}
	if pointer := schemaErr.JSONPointer(); len(pointer) > 0 {
		if field := safeFieldName(strings.Join(pointer, ".")); field != "" {
			params["field"] = field
		}
	} else if match := requiredFieldRE.FindStringSubmatch(schemaErr.Reason); len(match) == 2 {
		params["field"] = match[1]
	}
	params["rule"] = publicSchemaRule(schemaErr.SchemaField)
	addPublicSchemaBounds(params, schemaErr)
	return params
}

func safeFieldName(field string) string {
	if len(field) == 0 || len(field) > 256 || !safeFieldRE.MatchString(field) {
		return ""
	}
	return field
}

func publicSchemaRule(schemaField string) string {
	rules := map[string]string{
		"additionalProperties": "unknown_field",
		"enum":                 "enum",
		"format":               "format",
		"maxItems":             "max_items",
		"maxLength":            "max_length",
		"maximum":              "maximum",
		"minItems":             "min_items",
		"minLength":            "min_length",
		"minimum":              "minimum",
		"pattern":              "pattern",
		"required":             "required",
		"type":                 "type",
		"uniqueItems":          "unique",
	}
	if rule := rules[schemaField]; rule != "" {
		return rule
	}
	return "invalid"
}

func addPublicSchemaBounds(params map[string]any, schemaErr *openapi3.SchemaError) {
	if schemaErr.Schema == nil {
		return
	}
	schema := schemaErr.Schema
	switch schemaErr.SchemaField {
	case "minLength":
		params["min_length"] = schema.MinLength
	case "maxLength":
		if schema.MaxLength != nil {
			params["max_length"] = *schema.MaxLength
		}
	case "minimum":
		if schema.Min != nil {
			params["minimum"] = normalizedNumber(*schema.Min)
		}
	case "maximum":
		if schema.Max != nil {
			params["maximum"] = normalizedNumber(*schema.Max)
		}
	case "minItems":
		params["min_items"] = schema.MinItems
	case "maxItems":
		if schema.MaxItems != nil {
			params["max_items"] = *schema.MaxItems
		}
	case "format":
		if publicFormat(schema.Format) {
			params["format"] = schema.Format
		}
	}
}

func normalizedNumber(value float64) any {
	if value == float64(int64(value)) {
		return int64(value)
	}
	return value
}

func publicFormat(format string) bool {
	switch format {
	case "date", "date-time", "email", "hostname", "ipv4", "ipv6", "uri", "uuid":
		return true
	default:
		return false
	}
}
