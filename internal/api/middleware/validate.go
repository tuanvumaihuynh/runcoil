package middleware

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/api/httperr"
	"github.com/runcoil/runcoil/internal/apperr"
)

const invalidRequestMessage = "request is not valid"

// ValidateRequest refuses a request that does not match spec, whose paths sit under
// prefix.
func ValidateRequest(spec *openapi3.T, prefix string) func(http.Handler) http.Handler {
	return nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
		ErrorHandlerWithOpts: handleValidationError,
		DoNotValidateServers: true,
		Prefix:               prefix,
	})
}

func handleValidationError(_ context.Context, err error, w http.ResponseWriter, r *http.Request, _ nethttpmiddleware.ErrorHandlerOpts) {
	RecordError(r.Context(), err)

	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		httperr.Write(w, http.StatusRequestEntityTooLarge, httperr.New(apperr.CodePayloadTooLarge, "request body is too large"))
		return
	}

	if errors.Is(err, routers.ErrPathNotFound) {
		httperr.Write(w, http.StatusNotFound, httperr.New(apperr.CodeNotFound, "no such operation"))
		return
	}

	if errors.Is(err, routers.ErrMethodNotAllowed) {
		httperr.Write(w, http.StatusMethodNotAllowed, httperr.New(apperr.CodeInvalidRequest, "method not allowed"))
		return
	}

	var violation *jsonschema.ValidationError
	var requestErr *openapi3filter.RequestError
	isRequestErr := errors.As(err, &requestErr)

	if isRequestErr && requestErr.Parameter != nil {
		name := requestErr.Parameter.Name
		text := requestErrorText(requestErr)
		if errors.As(err, &violation) && len(violation.Causes) == 0 {
			text = name + " is not valid: " + violation.ErrorKind.LocalizedString(message.NewPrinter(language.English))
		}

		httperr.Write(w, http.StatusBadRequest, invalidRequest([]gen.ErrorParam{{Field: name, Message: text}}))

		return
	}

	if errors.As(err, &violation) {
		httperr.Write(w, http.StatusBadRequest, invalidRequest(violationParams(violation)))
		return
	}

	if isRequestErr && requestErrorText(requestErr) != "" {
		httperr.Write(w, http.StatusBadRequest, httperr.New(apperr.CodeInvalidRequest, requestErrorText(requestErr)))
		return
	}

	httperr.Write(w, http.StatusBadRequest, httperr.New(apperr.CodeInvalidRequest, invalidRequestMessage))
}

func requestErrorText(requestErr *openapi3filter.RequestError) string {
	if requestErr.Reason != "" || requestErr.Err == nil {
		return requestErr.Reason
	}

	return requestErr.Err.Error()
}

func invalidRequest(params []gen.ErrorParam) gen.ErrorResponse {
	text := invalidRequestMessage
	if len(params) == 1 {
		text = params[0].Message
	}

	return httperr.New(apperr.CodeInvalidRequest, text, params...)
}

func violationParams(violation *jsonschema.ValidationError) []gen.ErrorParam {
	if len(violation.Causes) > 0 {
		var params []gen.ErrorParam
		for _, cause := range violation.Causes {
			params = append(params, violationParams(cause)...)
		}

		return params
	}

	location := violation.InstanceLocation

	switch k := violation.ErrorKind.(type) {
	case *kind.Required:
		params := make([]gen.ErrorParam, 0, len(k.Missing))
		for _, name := range k.Missing {
			field := fieldPath(location, name)
			params = append(params, gen.ErrorParam{Field: field, Message: field + " is required"})
		}

		return params
	case *kind.AdditionalProperties:
		params := make([]gen.ErrorParam, 0, len(k.Properties))
		for _, name := range k.Properties {
			field := fieldPath(location, name)
			params = append(params, gen.ErrorParam{Field: field, Message: field + " is not allowed"})
		}

		return params
	default:
		field := fieldPath(location, "")
		text := field + " is not valid: " + k.LocalizedString(message.NewPrinter(language.English))

		return []gen.ErrorParam{{Field: field, Message: text}}
	}
}

func fieldPath(location []string, name string) string {
	path := location
	if name != "" {
		path = append(slices.Clone(location), name)
	}

	if len(path) == 0 {
		return "body"
	}

	return strings.Join(path, ".")
}
