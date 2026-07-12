package server

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

func (s *Server) handlePrepareUpload(writer http.ResponseWriter, request *http.Request) {
	s.handleMutating(writer, request, s.prepareUpload)
}

func (s *Server) prepareUpload(_ context.Context, identity Identity, requestID string, body []byte, _ string) operationResult {
	var input ascp.FileUploadRequest
	if p := decodeStrict(body, &input); p != nil {
		return problemResult(requestID, *p)
	}
	ticket, p := s.config.Files.Prepare(identity, input, s.publicBaseURL(), s.config.Now())
	if p != nil {
		return problemResult(requestID, *p)
	}
	s.appendAudit(ticket.FileID, "ascp.file.upload_prepared", identity.Actor, map[string]any{
		"file_id":       ticket.FileID,
		"maximum_bytes": ticket.MaximumBytes,
		"expires_at":    ticket.ExpiresAt,
	})
	return jsonResult(http.StatusCreated, ticket)
}

func (s *Server) handleFileRoutes(writer http.ResponseWriter, request *http.Request) {
	trimmed := strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1/files/"), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeProblem(writer, ascp.MustNewID("req"), problem(http.StatusNotFound, ascp.ErrNotFound, "File not found", "A file ID is required.", false))
		return
	}
	fileID := parts[0]
	switch {
	case len(parts) == 1 && request.Method == http.MethodGet:
		s.handleGetFile(writer, request, fileID)
	case len(parts) == 2 && parts[1] == "content" && request.Method == http.MethodPut:
		s.handleUploadContent(writer, request, fileID)
	case len(parts) == 2 && parts[1] == "content" && request.Method == http.MethodGet:
		s.handleDownloadContent(writer, request, fileID)
	default:
		writeProblem(writer, ascp.MustNewID("req"), problem(http.StatusNotFound, ascp.ErrNotFound, "Route not found", "The requested file operation is not available.", false))
	}
}

func (s *Server) handleUploadContent(writer http.ResponseWriter, request *http.Request, fileID string) {
	requestID, identity, ok := s.authenticateRead(writer, request)
	if !ok {
		return
	}
	ref, p := s.config.Files.Get(identity, fileID, s.config.Now())
	if p != nil {
		writeProblem(writer, requestID, *p)
		return
	}
	token := request.Header.Get("X-ASCP-Upload-Token")
	if token == "" {
		writeProblem(writer, requestID, problem(http.StatusForbidden, ascp.ErrForbidden, "Upload token required", "Provide the short-lived upload token returned by prepare-upload.", false))
		return
	}
	declaredDigest := request.Header.Get("X-ASCP-Content-Digest")
	if declaredDigest == "" || declaredDigest != ref.Digest {
		writeProblem(writer, requestID, problem(http.StatusUnprocessableEntity, ascp.ErrDigestMismatch, "Content digest mismatch", "X-ASCP-Content-Digest must match the digest declared during prepare-upload.", false))
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		writeProblem(writer, requestID, problem(http.StatusUnsupportedMediaType, ascp.ErrUnsupportedMediaType, "Content-Type required", "Upload content using the media type declared during prepare-upload.", false))
		return
	}
	maximum := ref.Size
	if maximum < 0 {
		maximum = 0
	}
	content, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maximum+1))
	if err != nil || int64(len(content)) > maximum {
		writeProblem(writer, requestID, problem(http.StatusRequestEntityTooLarge, ascp.ErrFileTooLarge, "Upload exceeds declared size", "The uploaded body is larger than the exact size declared during prepare-upload.", false))
		return
	}
	uploaded, uploadProblem := s.config.Files.Upload(identity, fileID, token, mediaType, content, s.config.Now())
	if uploadProblem != nil {
		writeProblem(writer, requestID, *uploadProblem)
		return
	}
	s.appendAudit(fileID, "ascp.file.ready", identity.Actor, map[string]any{
		"file_id":     fileID,
		"digest":      uploaded.Digest,
		"size":        uploaded.Size,
		"scan_status": uploaded.ScanStatus,
	})
	writeJSON(writer, http.StatusCreated, uploaded, http.Header{"X-Request-ID": []string{requestID}})
}

func (s *Server) handleGetFile(writer http.ResponseWriter, request *http.Request, fileID string) {
	requestID, identity, ok := s.authenticateRead(writer, request)
	if !ok {
		return
	}
	ref, p := s.config.Files.Get(identity, fileID, s.config.Now())
	if p != nil {
		writeProblem(writer, requestID, *p)
		return
	}
	writeJSON(writer, http.StatusOK, ref, http.Header{
		"ETag":         []string{`"` + ref.Digest + `"`},
		"X-Request-ID": []string{requestID},
	})
}

func (s *Server) handleDownloadContent(writer http.ResponseWriter, request *http.Request, fileID string) {
	requestID, identity, ok := s.authenticateRead(writer, request)
	if !ok {
		return
	}
	ref, content, p := s.config.Files.Content(identity, fileID, s.config.Now())
	if p != nil {
		writeProblem(writer, requestID, *p)
		return
	}
	writer.Header().Set("Content-Type", ref.MediaType)
	writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	writer.Header().Set("ETag", `"`+ref.Digest+`"`)
	writer.Header().Set("X-ASCP-Content-Digest", ref.Digest)
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Disposition", contentDisposition(ref.Disposition, ref.Name))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(content)
}

func (s *Server) publicBaseURL() string {
	if strings.TrimSpace(s.config.Manifest.BaseURL) != "" {
		return strings.TrimRight(s.config.Manifest.BaseURL, "/")
	}
	// A relative upload URL is safer than trusting Host or forwarding headers.
	// Production deployments should publish an explicit externally reachable
	// BaseURL in the manifest.
	return ""
}

func contentDisposition(disposition, name string) string {
	if disposition != "inline" {
		disposition = "attachment"
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		name = "file"
	}
	// RFC 5987 encoding avoids header injection and supports UTF-8 names. The
	// plain filename is deliberately ASCII and generic.
	return disposition + `; filename="file"; filename*=UTF-8''` + url.PathEscape(name)
}
