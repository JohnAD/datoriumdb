package server

import (
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/commandreq"
	"github.com/JohnAD/datoriumdb/internal/engine"
	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// handleCommand serves POST /datoriumdb/v1/command.
// Document/search/fileList/fileDelete/fileRead use application/json.
// fileCreate/fileUpdate use multipart/form-data with parts "command" and "content".
// Admin ensure commands require an admin JWT and the establishment server.
func (s *HTTPServer) handleCommand(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	if s.Engine.Cfg == nil {
		if err := s.Engine.Reload(); err != nil {
			writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "configError", Message: err.Error()}))
			return
		}
	}
	ct := r.Header.Get("Content-Type")
	mediatype, params, err := mime.ParseMediaType(ct)
	if err != nil {
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "contentTypeRequired",
			Message: "Content-Type must be application/json or multipart/form-data",
		}))
		return
	}
	switch mediatype {
	case "multipart/form-data":
		s.handleMultipartCommand(w, r, params["boundary"])
	case "application/json":
		s.handleJSONCommand(w, r, claims)
	default:
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "contentTypeRequired",
			Message: "Content-Type must be application/json or multipart/form-data",
		}))
	}
}

func (s *HTTPServer) handleJSONCommand(w http.ResponseWriter, r *http.Request, claims auth.Claims) {
	body, ferr := readBodyLimited(w, r, maxCommandBodyBytes)
	if ferr != nil {
		writeJSON(w, envelope.Fail(nil, *ferr))
		return
	}
	req, err := commandreq.DecodeJSON(body)
	if err != nil {
		writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "invalidRequest", Message: err.Error()}))
		return
	}
	switch req.Command {
	case "fileCreate", "fileUpdate":
		writeJSON(w, envelope.Fail(map[string]any{"command": req.Command}, envelope.Error{
			Code:    "contentTypeRequired",
			Message: "fileCreate and fileUpdate require multipart/form-data with a command JSON part and a content part",
		}))
		return
	case "fileRead":
		s.handleFileReadCommand(w, req)
		return
	case "fileList":
		writeJSON(w, s.Engine.ListFiles(req.Target, req.Parameter))
		return
	case "fileDelete":
		writeJSON(w, s.dispatchFileDelete(req))
		return
	case "collectionEnsure", "searchEnsure", "searchDelete":
		if err := auth.RequireAdmin(claims); err != nil {
			writeJSON(w, envelope.Fail(map[string]any{"command": req.Command}, toEnvelopeError(err)))
			return
		}
		writeJSON(w, s.Engine.Execute(req))
		return
	default:
		writeJSON(w, s.Engine.Execute(req))
	}
}

func (s *HTTPServer) handleMultipartCommand(w http.ResponseWriter, r *http.Request, boundary string) {
	if boundary == "" {
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "invalidRequest",
			Message: "multipart boundary is required",
		}))
		return
	}
	// Bound the overall request; individual content streaming still enforces maxFileBytes.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30+maxCommandBodyBytes)
	mr := multipart.NewReader(r.Body, boundary)
	var (
		req     commandreq.Request
		haveCmd bool
	)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "invalidRequest", Message: err.Error()}))
			return
		}
		name := part.FormName()
		switch name {
		case "command":
			raw, err := io.ReadAll(io.LimitReader(part, maxCommandBodyBytes+1))
			_ = part.Close()
			if err != nil {
				writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "invalidRequest", Message: err.Error()}))
				return
			}
			if int64(len(raw)) > maxCommandBodyBytes {
				writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "bodyTooLarge", Message: "command part exceeds 1 MiB"}))
				return
			}
			parsed, err := commandreq.DecodeJSON(raw)
			if err != nil {
				writeJSON(w, envelope.Fail(nil, envelope.Error{Code: "invalidRequest", Message: err.Error()}))
				return
			}
			req = parsed
			haveCmd = true
		case "content":
			contentCT := part.Header.Get("Content-Type")
			if !haveCmd {
				writeJSON(w, envelope.Fail(nil, envelope.Error{
					Code:    "invalidRequest",
					Message: "multipart command part must precede the content part",
				}))
				_ = part.Close()
				return
			}
			s.applyFileWrite(w, req, part, contentCT)
			_ = part.Close()
			return
		default:
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
		}
	}
	if !haveCmd {
		writeJSON(w, envelope.Fail(nil, envelope.Error{
			Code:    "invalidRequest",
			Message: "multipart command part is required",
		}))
		return
	}
	writeJSON(w, envelope.Fail(map[string]any{"command": req.Command}, envelope.Error{
		Code:    "invalidRequest",
		Message: "multipart content part is required for fileCreate and fileUpdate",
	}))
}

func (s *HTTPServer) applyFileWrite(w http.ResponseWriter, req commandreq.Request, content io.Reader, partContentType string) {
	if req.Command != "fileCreate" && req.Command != "fileUpdate" {
		writeJSON(w, envelope.Fail(map[string]any{"command": req.Command}, envelope.Error{
			Code:    "invalidRequest",
			Message: "multipart uploads are only valid for fileCreate and fileUpdate",
		}))
		return
	}
	detail, err := req.DetailMap()
	if err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"command": req.Command}, envelope.Error{Code: "invalidDetail", Message: err.Error()}))
		return
	}
	filename := commandreq.StringField(detail, "filename")
	if !fsstore.SafeFileName(filename) {
		writeJSON(w, envelope.Fail(map[string]any{"command": req.Command, "collection": req.Target, "id": req.Parameter, "filename": filename}, envelope.Error{
			Code:    "invalidFileName",
			Message: "file name is invalid or unsafe for filesystem storage",
		}))
		return
	}
	contentType := commandreq.StringField(detail, "contentType")
	if contentType == "" {
		contentType = partContentType
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	opts := engine.PutFileOptions{
		ContentType: contentType,
		OperationID: commandreq.StringField(detail, "operationId"),
	}
	if req.Command == "fileUpdate" {
		opts.IfMatch = commandreq.StringField(detail, "version")
		if opts.IfMatch == "" {
			writeJSON(w, envelope.Fail(map[string]any{"command": "fileUpdate", "collection": req.Target, "id": req.Parameter, "filename": filename}, envelope.Error{
				Code:    "fileVersionMismatch",
				Message: "fileUpdate requires detail.version (If-Match equivalent)",
			}))
			return
		}
	}
	writeJSON(w, s.Engine.PutFile(content, req.Target, req.Parameter, filename, opts))
}

func (s *HTTPServer) dispatchFileDelete(req commandreq.Request) envelope.Result {
	detail, err := req.DetailMap()
	if err != nil {
		return envelope.Fail(map[string]any{"command": "fileDelete"}, envelope.Error{Code: "invalidDetail", Message: err.Error()})
	}
	filename := commandreq.StringField(detail, "filename")
	version := commandreq.StringField(detail, "version")
	opID := commandreq.StringField(detail, "operationId")
	return s.Engine.DeleteFile(req.Target, req.Parameter, filename, version, opID)
}

func (s *HTTPServer) handleFileReadCommand(w http.ResponseWriter, req commandreq.Request) {
	detail, err := req.DetailMap()
	if err != nil {
		writeJSON(w, envelope.Fail(map[string]any{"command": "fileRead"}, envelope.Error{Code: "invalidDetail", Message: err.Error()}))
		return
	}
	filename := commandreq.StringField(detail, "filename")
	entry, f, errRes := s.Engine.OpenFileForDownload(req.Target, req.Parameter, filename)
	if errRes != nil {
		writeJSON(w, *errRes)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", entry.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(entry.ByteSize, 10))
	w.Header().Set("X-DatoriumDB-SHA256", entry.SHA256)
	w.Header().Set("X-DatoriumDB-File-Version", entry.Version)
	w.Header().Set("X-DatoriumDB-Operation-Id", entry.OperationID)
	w.Header().Set("ETag", `"`+entry.Version+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// registerSysFileRoutes adds machine-authenticated binary replication routes.
func (s *HTTPServer) registerSysFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /datoriumdb/v1/sys/apply-file-write", s.withAuth(s.handleApplyFileWrite))
	mux.HandleFunc("POST /datoriumdb/v1/sys/pending-file-write-work-items", s.withAuth(s.handleListPendingFileWrites))
	mux.HandleFunc("GET /datoriumdb/v1/sys/pending-file-write-work-items/{itemID}/content", s.withAuth(s.handleFetchPendingFileContent))
	mux.HandleFunc("GET /datoriumdb/v1/sys/pending-file-write-work-items/{itemID}", s.withAuth(s.handleFetchPendingFileWrite))
	mux.HandleFunc("DELETE /datoriumdb/v1/sys/pending-file-write-work-items/{itemID}", s.withAuth(s.handleCompletePendingFileWrite))
}
