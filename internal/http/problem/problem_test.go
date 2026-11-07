package problem

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/reqctx"
)

func request(method string) *http.Request {
	r := httptest.NewRequest(method, "/api/admin/v1/marinas/x", nil)
	return r.WithContext(reqctx.With(r.Context(), &reqctx.Info{RequestID: "req-123", Method: method, Route: "/api/admin/v1/marinas/{marinaId}"}))
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return m
}

func TestWrite_ContentTypeAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, request(http.MethodPost), ValidationFailed,
		Detail("check the fields"),
		Fields(FieldError{Field: "slug", Code: "invalid_format", Message: "lowercase letters, digits and dashes"}),
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != ContentType {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q", cc)
	}
	body := decode(t, rec)
	if body["type"] != "validation_failed" || body["title"] != "Validation failed" || body["status"] != float64(400) ||
		body["detail"] != "check the fields" || body["request_id"] != "req-123" {
		t.Fatalf("body = %v", body)
	}
	errs, _ := body["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("errors = %v", body["errors"])
	}
	fe, _ := errs[0].(map[string]any)
	if fe["field"] != "slug" || fe["code"] != "invalid_format" || fe["message"] == "" {
		t.Fatalf("field error = %v", fe)
	}
	for _, absent := range []string{"instance", "retry_after_seconds"} {
		if _, ok := body[absent]; ok {
			t.Fatalf("%s should be omitted", absent)
		}
	}
}

func TestWrite_StatusOverride(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, request(http.MethodPost), InvalidTOTP, Status(http.StatusUnauthorized))
	if rec.Code != http.StatusUnauthorized || decode(t, rec)["status"] != float64(401) {
		t.Fatalf("status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	Write(rec, request(http.MethodGet), NotFound, Status(200)) // non-error status is ignored
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestWrite_HeadHasNoBody(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, request(http.MethodHead), NotFound)
	if rec.Code != http.StatusNotFound || rec.Body.Len() != 0 {
		t.Fatalf("HEAD: %d %q", rec.Code, rec.Body.String())
	}
}

func TestRetryAfterHeader(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30"},
		{1500 * time.Millisecond, "2"},
		{time.Millisecond, "1"},
		{0, "1"},
		{15 * time.Minute, "900"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		Write(rec, request(http.MethodPost), RateLimited, RetryAfter(tt.d))
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d", rec.Code)
		}
		if got := rec.Header().Get("Retry-After"); got != tt.want {
			t.Errorf("Retry-After(%v) = %q, want %q", tt.d, got, tt.want)
		}
		if got := decode(t, rec)["retry_after_seconds"]; got != mustFloat(tt.want) {
			t.Errorf("retry_after_seconds(%v) = %v", tt.d, got)
		}
	}
	rec := httptest.NewRecorder()
	Write(rec, request(http.MethodPost), RateLimited)
	if rec.Header().Get("Retry-After") != "" {
		t.Fatal("Retry-After set without the option")
	}
}

func mustFloat(s string) float64 {
	var f float64
	_ = json.Unmarshal([]byte(s), &f)
	return f
}

var errKnown = errors.New("marina: slug taken")

func mapper(err error) (Type, []Option, bool) {
	if errors.Is(err, errKnown) {
		return SlugTaken, []Option{Detail("choose another slug")}, true
	}
	return "", nil, false
}

func TestWriteError_Mapped(t *testing.T) {
	var logs bytes.Buffer
	rec := httptest.NewRecorder()
	WriteError(rec, request(http.MethodPost), errors.Join(errKnown), mapper, slog.New(slog.NewJSONHandler(&logs, nil)))
	if rec.Code != http.StatusConflict || decode(t, rec)["type"] != "slug_taken" {
		t.Fatalf("mapped: %d %s", rec.Code, rec.Body.String())
	}
	if logs.Len() != 0 {
		t.Fatalf("mapped errors are not logged: %s", logs.String())
	}
}

func TestWriteError_UnmappedIsInternal(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	for _, m := range []Mapper{mapper, nil} {
		logs.Reset()
		rec := httptest.NewRecorder()
		WriteError(rec, request(http.MethodGet), errors.New("pq: connection refused to 10.0.0.1"), m, log)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", rec.Code)
		}
		body := decode(t, rec)
		if body["type"] != "internal_error" || body["request_id"] != "req-123" {
			t.Fatalf("body = %v", body)
		}
		if strings.Contains(rec.Body.String(), "10.0.0.1") {
			t.Fatal("internal error text leaked to the client")
		}
		var entry map[string]any
		if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
			t.Fatalf("log %q: %v", logs.String(), err)
		}
		if entry["level"] != "ERROR" || entry["request_id"] != "req-123" || entry["route"] != "/api/admin/v1/marinas/{marinaId}" {
			t.Fatalf("log entry = %v", entry)
		}
	}
}

func TestTypes_Registry(t *testing.T) {
	slug := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	all := All()
	if len(all) != 114 {
		t.Fatalf("declared %d types, want 114 (m0-plan §4.4 + m1-plan §4.5 + m2-plan §4.6 + m3-plan §7.4 + m4-plan §7.4 + m5-plan §7.4 + m6a-plan §3.4)", len(all))
	}
	for _, ty := range all {
		if !slug.MatchString(string(ty)) {
			t.Errorf("type %q is not a snake_case slug", ty)
		}
		if s := StatusOf(ty); s < 400 || s > 599 {
			t.Errorf("StatusOf(%q) = %d", ty, s)
		}
		if TitleOf(ty) == "" || !Known(ty) {
			t.Errorf("type %q has no title", ty)
		}
	}
	want := map[Type]int{
		ValidationFailed: 400, InvalidTOTP: 400, InvalidCredentials: 401, CSRFFailed: 403, NotFound: 404,
		MethodNotAllowed: 405, LastOwner: 409, VersionConflict: 412, UnsupportedMediaType: 415,
		IdempotencyKeyReused: 422, PreconditionRequired: 428, RateLimited: 429, InternalError: 500, NotReady: 503,
		MapInvalid: 422, MapNotEmpty: 409, MapNotGeoreferenced: 409, InvalidFile: 400, FileTooLarge: 413,
		CSVInvalid: 400, CustomerAnonymized: 409, CustomerMerged: 409, DownloadLinkExpired: 403,
		NoPrice: 422, PromoNotApplicable: 422, PriceListInvalid: 422, PriceListImmutable: 409, PriceListOverlap: 409,
		InUse:          409,
		NotImplemented: 501, RenewalExists: 409, ProposalClosed: 409, SignatureOpen: 409, InvalidSignature: 401,
		OTPInvalid: 422, OTPLocked: 409, OTPRateLimited: 429, LinkInvalid: 404, TemplateInvalid: 422,
		TemplateLimits: 422, PaperScanRequired: 422, InsuranceRequired: 422, CustomerEmailMissing: 422,
		SharesInvalid: 422, TableIncomplete: 422, TableActive: 409, TransferRequirements: 422, MandateOverlap: 409,
		VesselInOtherFleet: 409, VesselNotInFleet: 422, ImportNotUndoable: 409, ProviderUnknown: 422, CodeTaken: 409,
		ChargeImmutable: 409, ChargeCancelled: 409, UseEdit: 409, MatchExceeds: 409, RefundExceeds: 409,
		AlreadyReversed: 409, CreditInsufficient: 409, NotDeposit: 409, NothingToSettle: 409, ChargeExists: 409,
		NoTerminationAmount: 409, NotExported: 409, ExportEmpty: 422, ExportTooLarge: 422, ChargesNotOpen: 422,
		ReleaseNotResold: 422, CashSessionRequired: 409, CashSessionOpen: 409, CashSessionClosed: 409,
		CashInsufficient: 409, RegisterInUse: 409, ProviderDisabled: 422, ProviderRefundUnsupported: 422,
		ProviderError: 502, BillingItemMissing: 422,
	}
	for ty, s := range want {
		if StatusOf(ty) != s {
			t.Errorf("StatusOf(%q) = %d, want %d", ty, StatusOf(ty), s)
		}
	}
	if StatusOf("nope") != 500 || Known("nope") || TitleOf("nope") != "Internal error" {
		t.Error("unknown type handling")
	}
}

func TestNew_Defaults(t *testing.T) {
	p := New(Forbidden, Instance("/x"))
	if p.Type != Forbidden || p.Status != 403 || p.Title != "Forbidden" || p.Instance != "/x" {
		t.Fatalf("New = %+v", p)
	}
}

func TestWrite_Issues(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, request(http.MethodPut), MapInvalid, WithIssues([]map[string]any{{"code": "berth_overlap", "refs": []string{"a", "b"}}}))
	body := decode(t, rec)
	if rec.Code != http.StatusUnprocessableEntity || body["type"] != "map_invalid" {
		t.Fatalf("status %d, body %v", rec.Code, body)
	}
	issues, _ := body["issues"].([]any)
	if len(issues) != 1 {
		t.Fatalf("issues = %v", body["issues"])
	}
	rec = httptest.NewRecorder()
	Write(rec, request(http.MethodPut), NotFound)
	if _, ok := decode(t, rec)["issues"]; ok {
		t.Fatal("issues should be omitted")
	}
}

func TestWrite_DetailsAndPriceListIssues(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, request(http.MethodPost), PriceListInvalid,
		WithPriceListIssues([]map[string]any{{"code": "period_overlap", "severity": "error", "ref": map[string]string{"kind": "period"}}}),
		WithDetails(map[string]any{"references": 2}))
	body := decode(t, rec)
	if rec.Code != http.StatusUnprocessableEntity || body["type"] != "price_list_invalid" {
		t.Fatalf("status %d, body %v", rec.Code, body)
	}
	if issues, _ := body["price_list_issues"].([]any); len(issues) != 1 {
		t.Fatalf("price_list_issues = %v", body["price_list_issues"])
	}
	if d, _ := body["details"].(map[string]any); d["references"] != float64(2) {
		t.Fatalf("details = %v", body["details"])
	}
	rec = httptest.NewRecorder()
	Write(rec, request(http.MethodGet), NotFound)
	b := decode(t, rec)
	for _, k := range []string{"details", "price_list_issues"} {
		if _, ok := b[k]; ok {
			t.Fatalf("%s should be omitted", k)
		}
	}
}
