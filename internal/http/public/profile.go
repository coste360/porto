package public

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/profile"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
)

// Public marina profile (M12, m12-plan §11, §12.2). Every operation answers 404 not_found with the body of an
// unknown slug when the profile is unpublished or the section is off, sets the documented Cache-Control and ETag
// (If-None-Match -> 304) and never sets cookies.

// Cache-Control of each operation (m12-plan §12.2).
const (
	ccProfile   = "public, max-age=300"
	ccMap       = "public, max-age=3600"
	ccPrices    = "public, max-age=3600"
	ccOccupancy = "public, max-age=60"
	ccPhoto     = "public, max-age=31536000, immutable"
)

// isProfileNotFound reports the errors every public profile operation answers as an unknown slug.
func isProfileNotFound(err error) bool {
	return errors.Is(err, profile.ErrNotPublished) || errors.Is(err, pricelist.ErrNoPublicMatrix) || errors.Is(err, auth.ErrNotFound)
}

func (h *Handler) profileErr(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil || isProfileNotFound(err) {
		err = profile.ErrNotPublished
		w.Header().Set("X-Robots-Tag", "noindex")
	}
	respond.Error(w, r, err)
}

func shortETag(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:6])
}

// etagMatch reports whether an If-None-Match header matches etag (weak comparison, lists and * accepted).
func etagMatch(header, etag string) bool {
	if header == "" {
		return false
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || strings.TrimPrefix(part, "W/") == want {
			return true
		}
	}
	return false
}

// cached writes v with Cache-Control and ETag, or 304 when If-None-Match matches.
func cached(w http.ResponseWriter, r *http.Request, cacheControl, etag string, v any) {
	h := w.Header()
	h.Set("Cache-Control", cacheControl)
	h.Set("ETag", etag)
	h.Set("Vary", "Accept-Encoding")
	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	respond.JSON(w, http.StatusOK, v)
}

func langParam(l *api.Locale) string {
	if l == nil {
		return "it"
	}
	return string(*l)
}

// GetPublicProfile implements api.ServerInterface.
func (h *Handler) GetPublicProfile(w http.ResponseWriter, r *http.Request, slug api.Slug, params api.GetPublicProfileParams) {
	if h.Profiles == nil {
		h.profileErr(w, r, nil)
		return
	}
	pub, err := h.Profiles.PublicProfile(r.Context(), slug, langParam(params.Lang))
	if err != nil {
		h.profileErr(w, r, err)
		return
	}
	cached(w, r, ccProfile, pub.ETag, pub.Data)
}

// GetPublicProfileMap implements api.ServerInterface.
func (h *Handler) GetPublicProfileMap(w http.ResponseWriter, r *http.Request, slug api.Slug, _ api.GetPublicProfileMapParams) {
	if h.Profiles == nil {
		h.profileErr(w, r, nil)
		return
	}
	m, etag, err := h.Profiles.PublicMap(r.Context(), slug)
	if err != nil {
		h.profileErr(w, r, err)
		return
	}
	cached(w, r, ccMap, etag, m)
}

// GetPublicProfilePrices implements api.ServerInterface.
func (h *Handler) GetPublicProfilePrices(w http.ResponseWriter, r *http.Request, slug api.Slug, params api.GetPublicProfilePricesParams) {
	if h.Profiles == nil {
		h.profileErr(w, r, nil)
		return
	}
	m, etag, err := h.Profiles.PublicPrices(r.Context(), slug, langParam(params.Lang))
	if err != nil {
		h.profileErr(w, r, err)
		return
	}
	cached(w, r, ccPrices, etag, m)
}

// GetPublicOccupancy implements api.ServerInterface.
func (h *Handler) GetPublicOccupancy(w http.ResponseWriter, r *http.Request, slug api.Slug, _ api.GetPublicOccupancyParams) {
	if h.Profiles == nil {
		h.profileErr(w, r, nil)
		return
	}
	o, etag, err := h.Profiles.PublicOccupancy(r.Context(), slug)
	if err != nil {
		h.profileErr(w, r, err)
		return
	}
	cached(w, r, ccOccupancy, etag, o)
}

// GetPublicProfilePhoto implements api.ServerInterface.
func (h *Handler) GetPublicProfilePhoto(w http.ResponseWriter, r *http.Request, slug api.Slug, fileID api.FileId) {
	if h.Profiles == nil {
		h.profileErr(w, r, nil)
		return
	}
	img, err := h.Profiles.PublicPhoto(r.Context(), slug, fileID)
	if err != nil {
		h.profileErr(w, r, err)
		return
	}
	writeImage(w, r, img, ccPhoto)
}

// writeImage writes photo bytes with their type, ETag and Cache-Control (nosniff: the type is the re-encoded one).
func writeImage(w http.ResponseWriter, r *http.Request, img *profile.Image, cacheControl string) {
	hd := w.Header()
	hd.Set("Content-Type", img.ContentType)
	hd.Set("Cache-Control", cacheControl)
	hd.Set("X-Content-Type-Options", "nosniff")
	if img.ETag != "" {
		hd.Set("ETag", img.ETag)
		if etagMatch(r.Header.Get("If-None-Match"), img.ETag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(img.Body)
	}
}
