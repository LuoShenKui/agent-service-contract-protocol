package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

// FileStore is the security boundary for staged input files. Implementations
// must bind every file to an authenticated actor/principal, validate declared
// digest and size, and prevent unscanned or expired content from being used.
type FileStore interface {
	Prepare(identity Identity, request ascp.FileUploadRequest, baseURL string, now time.Time) (ascp.FileUploadTicket, *ascp.Problem)
	Upload(identity Identity, fileID, uploadToken, mediaType string, content []byte, now time.Time) (ascp.FileRef, *ascp.Problem)
	Get(identity Identity, fileID string, now time.Time) (ascp.FileRef, *ascp.Problem)
	Content(identity Identity, fileID string, now time.Time) (ascp.FileRef, []byte, *ascp.Problem)
	Validate(identity Identity, refs []ascp.FileRef, now time.Time) *ascp.Problem
}

type storedFile struct {
	Owner           Identity
	Reference       ascp.FileRef
	UploadTokenHash [sha256.Size]byte
	Content         []byte
	CreatedAt       time.Time
}

// MemoryFileStore is a reference-only implementation. It marks uploaded files
// clean immediately so tests can exercise the protocol; a production store must
// quarantine content until an independent malware-scanning pipeline succeeds.
type MemoryFileStore struct {
	mu               sync.RWMutex
	files            map[string]storedFile
	maximumFileBytes int64
	uploadTTL        time.Duration
	fileTTL          time.Duration
}

// NewMemoryFileStore creates a bounded in-memory file store for local demos.
func NewMemoryFileStore(maximumFileBytes int64) *MemoryFileStore {
	if maximumFileBytes <= 0 {
		maximumFileBytes = 10 << 20 // Ten MiB reference limit.
	}
	return &MemoryFileStore{
		files:            make(map[string]storedFile),
		maximumFileBytes: maximumFileBytes,
		uploadTTL:        10 * time.Minute,
		fileTTL:          24 * time.Hour,
	}
}

// Prepare validates metadata and creates a one-use scoped upload ticket.
func (s *MemoryFileStore) Prepare(identity Identity, request ascp.FileUploadRequest, baseURL string, now time.Time) (ascp.FileUploadTicket, *ascp.Problem) {
	name := strings.TrimSpace(request.Name)
	if name == "" || len(name) > 512 || strings.ContainsAny(name, "\r\n\x00/\\") {
		return ascp.FileUploadTicket{}, fileProblem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "file name is missing, unsafe, or too long", false)
	}
	mediaType, _, err := mime.ParseMediaType(request.MediaType)
	if err != nil || mediaType == "" {
		return ascp.FileUploadTicket{}, fileProblem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "file media_type is invalid", false)
	}
	if request.Size < 0 || request.Size > s.maximumFileBytes {
		return ascp.FileUploadTicket{}, fileProblem(http.StatusRequestEntityTooLarge, ascp.ErrFileTooLarge, "declared file size exceeds the service limit", false)
	}
	if err := ascp.ValidateSHA256Digest(request.Digest); err != nil {
		return ascp.FileUploadTicket{}, fileProblem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "digest must be a canonical ASCP sha256 digest", false)
	}
	if !request.ExpiresAt.IsZero() && !request.ExpiresAt.After(now) {
		return ascp.FileUploadTicket{}, fileProblem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "requested file expiry must be in the future", false)
	}

	fileID := ascp.MustNewID("fil")
	token, err := randomUploadToken()
	if err != nil {
		return ascp.FileUploadTicket{}, fileProblem(http.StatusInternalServerError, ascp.ErrInternal, "could not create upload credential", true)
	}
	expiresAt := now.Add(s.fileTTL)
	if !request.ExpiresAt.IsZero() && request.ExpiresAt.Before(expiresAt) && request.ExpiresAt.After(now) {
		expiresAt = request.ExpiresAt
	}
	ref := ascp.FileRef{
		FileID:      fileID,
		URI:         "ascp-file://" + fileID,
		Name:        name,
		MediaType:   mediaType,
		Size:        request.Size,
		Digest:      request.Digest,
		Disposition: "attachment",
		Purpose:     request.Purpose,
		State:       "pending_upload",
		ScanStatus:  "not_scanned",
		ExpiresAt:   expiresAt,
	}
	s.mu.Lock()
	s.files[fileID] = storedFile{
		Owner:           cloneIdentity(identity),
		Reference:       ref,
		UploadTokenHash: sha256.Sum256([]byte(token)),
		CreatedAt:       now,
	}
	s.mu.Unlock()

	return ascp.FileUploadTicket{
		FileID:       fileID,
		UploadURL:    strings.TrimRight(baseURL, "/") + "/v1/files/" + fileID + "/content",
		UploadMethod: http.MethodPut,
		UploadToken:  token,
		RequiredHeaders: map[string]string{
			"Content-Type":          mediaType,
			"X-ASCP-Content-Digest": request.Digest,
		},
		MaximumBytes: request.Size,
		ExpiresAt:    now.Add(s.uploadTTL),
	}, nil
}

// Upload atomically accepts bytes only when identity, token, media type, size,
// and digest match the prepared ticket. Re-uploading identical bytes is safe;
// different bytes under the same file ID are rejected.
func (s *MemoryFileStore) Upload(identity Identity, fileID, uploadToken, mediaType string, content []byte, now time.Time) (ascp.FileRef, *ascp.Problem) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok := s.files[fileID]
	if !ok || !sameIdentity(stored.Owner, identity) {
		return ascp.FileRef{}, fileProblem(http.StatusNotFound, ascp.ErrNotFound, "file upload target was not found", false)
	}
	if now.After(stored.CreatedAt.Add(s.uploadTTL)) {
		return ascp.FileRef{}, fileProblem(http.StatusGone, ascp.ErrUploadExpired, "upload ticket has expired", false)
	}
	presentedTokenHash := sha256.Sum256([]byte(uploadToken))
	if subtle.ConstantTimeCompare(presentedTokenHash[:], stored.UploadTokenHash[:]) != 1 {
		return ascp.FileRef{}, fileProblem(http.StatusForbidden, ascp.ErrForbidden, "upload token is invalid", false)
	}
	parsedType, _, err := mime.ParseMediaType(mediaType)
	if err != nil || !strings.EqualFold(parsedType, stored.Reference.MediaType) {
		return ascp.FileRef{}, fileProblem(http.StatusUnsupportedMediaType, ascp.ErrUnsupportedMediaType, "uploaded media type differs from the prepared metadata", false)
	}
	if int64(len(content)) != stored.Reference.Size {
		return ascp.FileRef{}, fileProblem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "uploaded byte length differs from the declared size", false)
	}
	actualDigest := ascp.SHA256Digest(content)
	if actualDigest != stored.Reference.Digest {
		return ascp.FileRef{}, fileProblem(http.StatusUnprocessableEntity, ascp.ErrDigestMismatch, "uploaded content digest differs from the declared digest", false)
	}
	if stored.Reference.State == "ready" {
		if ascp.SHA256Digest(stored.Content) != actualDigest {
			return ascp.FileRef{}, fileProblem(http.StatusConflict, ascp.ErrIdempotencyConflict, "file ID already contains different bytes", false)
		}
		return stored.Reference, nil
	}

	stored.Content = append([]byte(nil), content...)
	stored.Reference.State = "ready"
	stored.Reference.ScanStatus = "clean"
	s.files[fileID] = stored
	return stored.Reference, nil
}

// Get returns metadata only after owner and expiry checks.
func (s *MemoryFileStore) Get(identity Identity, fileID string, now time.Time) (ascp.FileRef, *ascp.Problem) {
	s.mu.RLock()
	stored, ok := s.files[fileID]
	s.mu.RUnlock()
	if !ok || !sameIdentity(stored.Owner, identity) {
		return ascp.FileRef{}, fileProblem(http.StatusNotFound, ascp.ErrNotFound, "file was not found", false)
	}
	if !stored.Reference.ExpiresAt.IsZero() && !stored.Reference.ExpiresAt.After(now) {
		return ascp.FileRef{}, fileProblem(http.StatusGone, ascp.ErrFileNotReady, "file reference has expired", false)
	}
	return stored.Reference, nil
}

// Content returns a defensive copy of ready and clean bytes.
func (s *MemoryFileStore) Content(identity Identity, fileID string, now time.Time) (ascp.FileRef, []byte, *ascp.Problem) {
	ref, problem := s.Get(identity, fileID, now)
	if problem != nil {
		return ascp.FileRef{}, nil, problem
	}
	if ref.State != "ready" || ref.ScanStatus != "clean" {
		return ascp.FileRef{}, nil, fileProblem(http.StatusConflict, ascp.ErrFileNotReady, "file is not ready for use", true)
	}
	s.mu.RLock()
	content := append([]byte(nil), s.files[fileID].Content...)
	s.mu.RUnlock()
	return ref, content, nil
}

// Validate confirms that every caller-supplied reference exactly matches the
// provider's authoritative metadata and is ready for safe consumption.
func (s *MemoryFileStore) Validate(identity Identity, refs []ascp.FileRef, now time.Time) *ascp.Problem {
	for index, supplied := range refs {
		actual, problem := s.Get(identity, supplied.FileID, now)
		if problem != nil {
			problem.Detail = fmt.Sprintf("input_files[%d]: %s", index, problem.Detail)
			return problem
		}
		if actual.State != "ready" || actual.ScanStatus != "clean" {
			return fileProblem(http.StatusConflict, ascp.ErrFileNotReady, fmt.Sprintf("input_files[%d] is not ready and clean", index), true)
		}
		if supplied.URI != actual.URI || supplied.Name != actual.Name || supplied.MediaType != actual.MediaType ||
			supplied.Size != actual.Size || supplied.Digest != actual.Digest {
			return fileProblem(http.StatusConflict, ascp.ErrFileRejected, fmt.Sprintf("input_files[%d] metadata does not match the provider record", index), false)
		}
	}
	return nil
}

func sameIdentity(left, right Identity) bool {
	return left.Actor == right.Actor && left.Principal == right.Principal
}

func cloneIdentity(identity Identity) Identity {
	copy := identity
	copy.Scopes = make(map[string]bool, len(identity.Scopes))
	for scope, allowed := range identity.Scopes {
		copy.Scopes[scope] = allowed
	}
	return copy
}

func randomUploadToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func fileProblem(status int, code, detail string, retryable bool) *ascp.Problem {
	problem := &ascp.Problem{
		Type:      "urn:ascp:problem:" + code,
		Title:     strings.ReplaceAll(code, "_", " "),
		Status:    status,
		Detail:    detail,
		Code:      code,
		Category:  "file",
		Retryable: retryable,
	}
	return problem
}
