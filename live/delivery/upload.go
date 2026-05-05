package delivery

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// UploadHandler accepts a multipart PDF, stores it on disk under a UUID name,
// and returns the public URL where it will be served.
//
// The /uploads/* prefix is registered separately in app/routes.go as a static
// file server pointing at Dir.
type UploadHandler struct {
	dir      string
	maxBytes int64
	logger   *log.Logger
}

func NewUploadHandler(dir string, maxMB int, logger *log.Logger) *UploadHandler {
	if maxMB <= 0 {
		maxMB = 200
	}
	return &UploadHandler{
		dir:      dir,
		maxBytes: int64(maxMB) << 20,
		logger:   logger,
	}
}

func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBytes)
	// Cap in-memory storage at 32 MB; anything bigger spills to a temp file.
	// (The hard upload limit is enforced by MaxBytesReader above.)
	const inMemoryLimit = 32 << 20
	if err := r.ParseMultipartForm(inMemoryLimit); err != nil {
		h.logger.Printf("upload: parse multipart: %v (limit=%d MB)", err, h.maxBytes>>20)
		switch {
		case errors.Is(err, http.ErrBodyReadAfterClose):
			fallthrough
		case strings.Contains(err.Error(), "http: request body too large"):
			http.Error(w,
				fmt.Sprintf("file exceeds %d MB limit", h.maxBytes>>20),
				http.StatusRequestEntityTooLarge)
		default:
			http.Error(w, "malformed multipart body: "+err.Error(), http.StatusBadRequest)
		}
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing 'file' field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".pdf" {
		http.Error(w, "only .pdf is allowed", http.StatusUnsupportedMediaType)
		return
	}

	if err := os.MkdirAll(h.dir, 0o755); err != nil {
		h.logger.Printf("upload: mkdir %s: %v", h.dir, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	name := uuid.NewString() + ext
	path := filepath.Join(h.dir, name)
	out, err := os.Create(path)
	if err != nil {
		h.logger.Printf("upload: create %s: %v", path, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		_ = os.Remove(path)
		if errors.Is(err, http.ErrBodyReadAfterClose) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		h.logger.Printf("upload: copy %s: %v", path, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"url":  "/uploads/" + name,
		"name": header.Filename,
	})
}
