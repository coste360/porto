package signature

import (
	"context"
	"errors"
	"io"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// MaxScanBytes is the largest paper scan (PDF, JPEG or PNG).
const MaxScanBytes = 15 << 20

// UploadPaperScan stores the scan of a paper signature encrypted at rest and returns its file id
// (contracts.write; rights.manage for transfers). The scan is attached to the signers at confirmation.
func (s *Service) UploadPaperScan(ctx context.Context, p *auth.Principal, marinaID, requestID, declaredType string, body io.Reader) (string, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaRead)
	if err != nil {
		return "", err
	}
	r, err := store.GetSignatureRequest(ctx, s.d.DB.DB, scope, requestID)
	if err != nil {
		return "", notFound(err)
	}
	if !p.Can(scope.OrgID(), scope.ID(), writePermission(r.SubjectType)) {
		return "", auth.ErrForbidden
	}
	content, err := io.ReadAll(io.LimitReader(body, MaxScanBytes+1))
	if err != nil || len(content) == 0 {
		return "", document.ErrInvalidFile
	}
	if len(content) > MaxScanBytes {
		return "", document.ErrFileTooLarge
	}
	ct, _, _, err := filestore.Sniff(content, declaredType)
	switch {
	case err == nil && ct == filestore.TypeSVG:
		return "", document.ErrUnsupportedMedia
	case errors.Is(err, filestore.ErrUnsupportedType):
		return "", document.ErrUnsupportedMedia
	case err != nil:
		return "", document.ErrInvalidFile
	}
	pf, err := s.d.Docs.PutFile(ctx, scope, content, ct)
	if err != nil {
		return "", err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		return s.d.Docs.InsertFileTx(ctx, tx, scope, pf, p.UserID)
	})
	if err != nil {
		s.d.Docs.DiscardFile(pf)
		return "", err
	}
	return pf.ID, nil
}

// ConfirmPaper confirms the paper signature of some (or, with no ids, all) signers still to sign; with
// require_paper_scan an uploaded scan is required (ErrPaperScanRequired). When every signer has signed the
// request is completed in the same call: the signed PDF is the original plus a cover page appended by incremental
// update (contracts.write; rights.manage for transfers).
func (s *Service) ConfirmPaper(ctx context.Context, p *auth.Principal, marinaID, requestID string, ifMatch int, signerIDs []string, scanFileID string) (*Request, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaRead)
	if err != nil {
		return nil, err
	}
	all := false
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetSignatureRequest(ctx, tx, scope, requestID)
		if err != nil {
			return notFound(err)
		}
		if !p.Can(scope.OrgID(), scope.ID(), writePermission(r.SubjectType)) {
			return auth.ErrForbidden
		}
		if ifMatch != 0 && r.Version != ifMatch {
			return store.ErrVersionMismatch
		}
		if r.Status != StatusSent && r.Status != StatusPartiallySigned {
			return ErrNotSigner
		}
		cs, err := store.GetContractSettings(ctx, tx, scope, s.now())
		if err != nil {
			return err
		}
		if scanFileID != "" {
			if _, err := store.GetStoredFile(ctx, tx, scope, scanFileID); err != nil {
				return notFound(err)
			}
		} else if cs.RequirePaperScan {
			return ErrPaperScanRequired
		}
		signers, err := store.SignersOf(ctx, tx, scope, r.ID)
		if err != nil {
			return err
		}
		want := map[string]bool{}
		for _, id := range signerIDs {
			want[id] = true
		}
		n := 0
		for i := range signers {
			sg := &signers[i]
			if sg.Status == SignerSigned || (len(want) > 0 && !want[sg.ID]) {
				continue
			}
			// A paper confirmation covers the paper driver and the marina countersignature of other drivers.
			if r.Driver != DriverPaper && sg.Role != "marina" {
				continue
			}
			ev := evidence{Method: "paper", ConfirmedBy: p.UserID, ScanFileID: scanFileID, DocumentSHA256: s.documentSHA(ctx, tx, scope, r.DocumentID)}
			done, err := s.signerSignedTx(ctx, tx, scope, r, sg, ev, p.Actor(), []string{SignerWaitingStatus, SignerInvited})
			if err != nil {
				return err
			}
			all = done
			n++
		}
		if n == 0 {
			return ErrNotSigner
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if all {
		if err := s.complete(ctx, scope, requestID, nil); err != nil {
			return nil, err
		}
	}
	return s.getRequest(ctx, s.d.DB.DB, scope, p, requestID)
}

var _ = docs.KindSignatureEvidence
