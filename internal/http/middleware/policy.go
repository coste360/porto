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
	// MaxStreamBytes, when > 0, is the limit of a raw body streamed to disk (x-porto-max-stream-bytes, M6a desktop
	// restore); it replaces MaxBodyBytes and the global MaxBodyCap for that operation.
	MaxStreamBytes int64
	// InstanceToken is true when the operation is authenticated by the Porto-Instance-Token header (desktop
	// activation): the middleware treats it as public and the handler checks the token.
	InstanceToken bool
	// Offline is true when the operation may be replayed from the offline queue (x-porto-offline: true, M6b,
	// m6b-plan §4.4): OfflineGuard refuses the Porto-Offline-Queued-At header on every other operation and the
	// idempotency middleware keeps its keys 7 days.
	Offline bool
	// ProxyOnly is true when every security alternative of the operation is one answered by the desktop remote
	// proxy (appPin, remoteCookie, M6b): this router treats it as public and its handler answers 404, because the
	// operation exists only in remote mode where no Porto server runs in the app.
	ProxyOnly bool
	// DomainIdempotency is true when the domain handles Idempotency-Key itself (x-porto-idempotency: domain, M8
	// createBooking): the idempotency middleware lets the request through untouched and stores nothing, so no
	// response carrying a token is persisted.
	DomainIdempotency bool
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
	OperationID    string                 `yaml:"operationId"`
	Security       *[]map[string][]string `yaml:"security"`
	MaxBodyBytes   int64                  `yaml:"x-porto-max-body-bytes"`
	MaxStreamBytes int64                  `yaml:"x-porto-max-stream-bytes"`
	Offline        bool                   `yaml:"x-porto-offline"`
	Idempotency    string                 `yaml:"x-porto-idempotency"`
	RequestBody    *struct {
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
			p := Policy{
				OperationID: op.OperationID, MaxBodyBytes: op.MaxBodyBytes, MaxStreamBytes: op.MaxStreamBytes,
				Offline: op.Offline, DomainIdempotency: op.Idempotency == "domain",
			}
			if op.Idempotency != "" && op.Idempotency != "domain" {
				return nil, fmt.Errorf("middleware: %s %s: x-porto-idempotency must be domain", name, path)
			}
			if op.RequestBody != nil {
				for ct := range op.RequestBody.Content {
					p.BodyTypes = append(p.BodyTypes, strings.ToLower(ct))
				}
				slices.Sort(p.BodyTypes)
			}
			if op.MaxStreamBytes != 0 {
				if op.MaxStreamBytes < 0 || op.MaxStreamBytes > MaxStreamCap || op.MaxBodyBytes != 0 || !p.BinaryBody() ||
					strings.Contains(path, "{") {
					return nil, fmt.Errorf("middleware: %s %s: x-porto-max-stream-bytes needs a raw body on a fixed path, "+
						"at most %d, without x-porto-max-body-bytes", name, path, MaxStreamCap)
				}
			}
			proxyAlternatives := 0
			for _, alt := range reqs {
				if proxyAlternative(alt) {
					if len(alt) != 1 {
						return nil, fmt.Errorf("middleware: %s %s: appPin and remoteCookie go alone", name, path)
					}
					proxyAlternatives++
					continue
				}
				if _, ok := alt[instanceTokenScheme]; ok {
					if len(alt) != 1 {
						return nil, fmt.Errorf("middleware: %s %s: instanceToken goes alone", name, path)
					}
					p.InstanceToken = true
					continue
				}
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
			if proxyAlternatives > 0 && proxyAlternatives == len(reqs) {
				p.ProxyOnly = true
			}
			if p.Offline && (p.Public() || p.ProxyOnly || p.InstanceToken) {
				return nil, fmt.Errorf("middleware: %s %s: x-porto-offline needs an authenticated operation", name, path)
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
	// MaxBodyCap is the hard cap of every request body (the global BodyLimit), except streamed bodies.
	MaxBodyCap int64 = 16 << 20
	// MaxStreamCap bounds x-porto-max-stream-bytes (desktop restore upload, M6a).
	MaxStreamCap int64 = 4 << 30
)

// instanceTokenScheme is the security scheme of the desktop activation (checked by the handler).
const instanceTokenScheme = "instanceToken"

// Security schemes of the desktop remote proxy (M6b, m6b-plan §7.4, §7.5): the app PIN header and the per-launch
// proxy cookie. They are checked by internal/desktop/remote, never by this router, which ignores the alternatives.
const (
	appPinScheme       = "appPin"
	remoteCookieScheme = "remoteCookie"
)

func proxyAlternative(alt map[string][]string) bool {
	_, pin := alt[appPinScheme]
	_, cookie := alt[remoteCookieScheme]
	return pin || cookie
}

// OfflineOperations returns the operation ids flagged x-porto-offline, sorted.
func (ps Policies) OfflineOperations() []string {
	var out []string
	for _, p := range ps {
		if p.Offline {
			out = append(out, p.OperationID)
		}
	}
	slices.Sort(out)
	return out
}

// StreamedPaths returns the fixed request paths ("METHOD /path") whose operation streams a large raw body
// (x-porto-max-stream-bytes) with their limit: the global body limit lets them through, BodyLimitByPolicy applies
// the stream limit.
func (ps Policies) StreamedPaths() map[string]int64 {
	out := map[string]int64{}
	for k, p := range ps {
		if p.MaxStreamBytes > 0 {
			out[k] = p.MaxStreamBytes
		}
	}
	return out
}

// unsafeMethod reports whether an HTTP method can change state.
func unsafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	}
	return true
}
