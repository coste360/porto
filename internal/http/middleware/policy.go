package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/coste360/porto/internal/reqctx"
)

// Policy is what an operation of the contract accepts, read from its `security` requirements.
type Policy struct {
	OperationID string
	// Methods are the accepted authentication methods; empty means the operation is public.
	Methods []reqctx.AuthMethod
	// CSRF is true when the cookie alternative also requires the csrfToken scheme.
	CSRF bool
	// MaxBodyBytes is the request body limit of the operation (x-porto-max-body-bytes); 0 = the default.
	MaxBodyBytes int64
	// BodyTypes are the request content types the operation declares (empty = no request body).
	BodyTypes []string
}

// BinaryBody reports whether the operation takes a raw (non-JSON) body: it declares request content types and
// none of them is JSON. Such bodies reach the handler as they are; the service checks the type after
// authorising (m1-plan §4.1).
func (p Policy) BinaryBody() bool {
	if len(p.BodyTypes) == 0 {
		return false
	}
	for _, t := range p.BodyTypes {
		if isJSONType(t) {
			return false
		}
	}
	return true
}

func isJSONType(ct string) bool { return ct == "application/json" || strings.HasSuffix(ct, "+json") }

// Public reports whether the operation needs no authentication.
func (p Policy) Public() bool { return len(p.Methods) == 0 }

// Accepts reports whether m is one of the accepted methods.
func (p Policy) Accepts(m reqctx.AuthMethod) bool { return slices.Contains(p.Methods, m) }

// Policies maps "METHOD /route/pattern" to the policy of that operation.
type Policies map[string]Policy

// Lookup returns the policy of a routed request.
func (ps Policies) Lookup(method, route string) (Policy, bool) {
	p, ok := ps[method+" "+route]
	return p, ok
}

var schemeMethods = map[string]reqctx.AuthMethod{
	"cookieAuth": reqctx.AuthSession,
	"apiKeyAuth": reqctx.AuthAPIKey,
	"deviceAuth": reqctx.AuthDevice,
}

type specOperation struct {
	OperationID  string                 `yaml:"operationId"`
	Security     *[]map[string][]string `yaml:"security"`
	MaxBodyBytes int64                  `yaml:"x-porto-max-body-bytes"`
	RequestBody  *struct {
		Content map[string]yaml.Node `yaml:"content"`
	} `yaml:"requestBody"`
}

type specDocument struct {
	Security []map[string][]string           `yaml:"security"`
	Paths    map[string]map[string]yaml.Node `yaml:"paths"`
}

var httpMethods = []string{"get", "put", "post", "delete", "patch", "head", "options"}

// ParsePolicies reads the security requirements of every operation of an OpenAPI document. An operation
// without a security list inherits the document-level one; an unknown scheme is an error.
func ParsePolicies(spec []byte) (Policies, error) {
	var doc specDocument
	if err := yaml.Unmarshal(spec, &doc); err != nil {
		return nil, fmt.Errorf("middleware: parse contract: %w", err)
	}
	out := Policies{}
	for path, item := range doc.Paths {
		for name, node := range item {
			if !slices.Contains(httpMethods, name) {
				continue
			}
			var op specOperation
			if err := node.Decode(&op); err != nil {
				return nil, fmt.Errorf("middleware: %s %s: %w", name, path, err)
			}
			reqs := doc.Security
			if op.Security != nil {
				reqs = *op.Security
			}
			if op.MaxBodyBytes < 0 || op.MaxBodyBytes > MaxBodyCap {
				return nil, fmt.Errorf("middleware: %s %s: x-porto-max-body-bytes out of range", name, path)
			}
			p := Policy{OperationID: op.OperationID, MaxBodyBytes: op.MaxBodyBytes}
			if op.RequestBody != nil {
				for ct := range op.RequestBody.Content {
					p.BodyTypes = append(p.BodyTypes, strings.ToLower(ct))
				}
				slices.Sort(p.BodyTypes)
			}
			for _, alt := range reqs {
				var method reqctx.AuthMethod
				csrf := false
				for scheme := range alt {
					if m, ok := schemeMethods[scheme]; ok {
						method = m
					} else if scheme == "csrfToken" {
						csrf = true
					} else {
						return nil, fmt.Errorf("middleware: %s %s: unknown security scheme %q", name, path, scheme)
					}
				}
				if method == "" {
					return nil, fmt.Errorf("middleware: %s %s: security alternative without authentication", name, path)
				}
				if csrf && method != reqctx.AuthSession {
					return nil, errors.New("middleware: csrfToken only goes with cookieAuth")
				}
				if !slices.Contains(p.Methods, method) {
					p.Methods = append(p.Methods, method)
				}
				if method == reqctx.AuthSession && csrf {
					p.CSRF = true
				}
			}
			out[strings.ToUpper(name)+" "+path] = p
		}
	}
	return out, nil
}

// Request body limits (m1-plan §4.1, D20).
const (
	// DefaultMaxBodyBytes applies to operations without x-porto-max-body-bytes.
	DefaultMaxBodyBytes int64 = 1 << 20
	// MaxBodyCap is the hard cap of every request body (the global BodyLimit).
	MaxBodyCap int64 = 16 << 20
)

// unsafeMethod reports whether an HTTP method can change state.
func unsafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	}
	return true
}
