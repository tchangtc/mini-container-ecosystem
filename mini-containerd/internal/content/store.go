// Package content implements a content-addressable blob store backed by the
// local filesystem. It stores OCI image layers and configs keyed by digest.
//
// The store implements containerd's content service gRPC interface
// (github.com/containerd/containerd/api/services/content/v1).
package content

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	contentv1 "github.com/containerd/containerd/api/services/content/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DefaultRoot is the default storage root for content blobs.
// Configurable via MINI_CONTAINERD_ROOT env var in cmd/containerd.
const DefaultRoot = "/tmp/mini-containerd/data/content"

// blobDir returns the subdirectory for a given digest within root.
// e.g. sha256:abc123... → root/blobs/sha256/ab/abc123...
func blobDir(root, digest string) string {
	// digest format: "sha256:hexstring"
	algo, hash, ok := splitDigest(digest)
	if !ok {
		// Fallback: use full digest as filename
		return filepath.Join(root, "blobs", digest)
	}
	return filepath.Join(root, "blobs", algo, hash[:2])
}

// blobPath returns the full file path for a blob.
func blobPath(root, digest string) string {
	algo, hash, ok := splitDigest(digest)
	if !ok {
		return filepath.Join(root, "blobs", digest)
	}
	return filepath.Join(root, "blobs", algo, hash[:2], hash)
}

// splitDigest splits "sha256:abcd1234..." into ("sha256", "abcd1234...", true).
func splitDigest(digest string) (string, string, bool) {
	for i := 0; i < len(digest); i++ {
		if digest[i] == ':' {
			return digest[:i], digest[i+1:], true
		}
	}
	return "", "", false
}

// BlobInfo holds metadata about a stored blob.
type BlobInfo struct {
	Digest    string
	Size      int64
	Labels    map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ToProto converts BlobInfo to the containerd protobuf Info type.
func (b BlobInfo) ToProto() *contentv1.Info {
	return &contentv1.Info{
		Digest:    b.Digest,
		Size:      b.Size,
		CreatedAt: timestamppb.New(b.CreatedAt),
		UpdatedAt: timestamppb.New(b.UpdatedAt),
		Labels:    b.Labels,
	}
}

// uploadState tracks an in-progress blob upload.
type uploadState struct {
	Ref       string
	Offset    int64
	Total     int64
	Expected  string
	StartedAt time.Time
	UpdatedAt time.Time
	Digest    string // set on commit
	Size      int64  // set on commit
	Committed bool

	// Temporary file for buffered writes
	tmpFile *os.File
	hasher  hash.Hash
}

// Store is a content-addressable blob store.
type Store struct {
	mu       sync.RWMutex
	root     string
	blobs    map[string]*BlobInfo   // digest → info (committed blobs)
	uploads  map[string]*uploadState // ref → upload state (in-progress)
}

// NewStore creates a new content store rooted at the given path.
// The directory is created if it does not exist.
func NewStore(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create content root %q: %w", root, err)
	}
	s := &Store{
		root:    root,
		blobs:   make(map[string]*BlobInfo),
		uploads: make(map[string]*uploadState),
	}
	// Scan existing blobs on startup
	if err := s.scan(); err != nil {
		return nil, fmt.Errorf("scan existing blobs: %w", err)
	}
	return s, nil
}

// scan walks the blob directory and loads existing blobs into memory.
func (s *Store) scan() error {
	blobsRoot := filepath.Join(s.root, "blobs")
	entries, err := os.ReadDir(blobsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, algo := range entries {
		if !algo.IsDir() {
			continue
		}
		algoPath := filepath.Join(blobsRoot, algo.Name())
		prefixes, err := os.ReadDir(algoPath)
		if err != nil {
			continue
		}
		for _, prefix := range prefixes {
			if !prefix.IsDir() {
				continue
			}
			prefixPath := filepath.Join(algoPath, prefix.Name())
			files, err := os.ReadDir(prefixPath)
			if err != nil {
				continue
			}
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				digest := algo.Name() + ":" + prefix.Name() + f.Name()
				fullPath := filepath.Join(prefixPath, f.Name())
				info, err := os.Stat(fullPath)
				if err != nil {
					continue
				}
				s.blobs[digest] = &BlobInfo{
					Digest:    digest,
					Size:      info.Size(),
					Labels:    make(map[string]string),
					CreatedAt: info.ModTime(),
					UpdatedAt: info.ModTime(),
				}
			}
		}
	}
	return nil
}

// Info returns metadata for a committed blob.
func (s *Store) Info(digest string) (*BlobInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info, ok := s.blobs[digest]
	if !ok {
		return nil, fmt.Errorf("blob %q: %w", digest, os.ErrNotExist)
	}
	return info, nil
}

// List returns all committed blobs matching the given filters.
func (s *Store) List(filters []string) []*BlobInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*BlobInfo, 0, len(s.blobs))
	for _, info := range s.blobs {
		if matchFilters(info, filters) {
			result = append(result, info)
		}
	}
	return result
}

// matchFilters checks if the blob matches at least one filter.
// Simplified: only supports "digest==" prefix matching.
func matchFilters(info *BlobInfo, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if matchFilter(info, f) {
			return true
		}
	}
	return false
}

func matchFilter(info *BlobInfo, filter string) bool {
	// Minimal filter support
	if filter == "" {
		return true
	}
	// "name==<digest>" style (containerd filter syntax)
	if len(filter) > 5 && filter[:5] == "name==" {
		return info.Digest == filter[5:]
	}
	// Prefix match on digest
	return len(filter) > 0 && len(info.Digest) >= len(filter) &&
		info.Digest[:len(filter)] == filter
}

// Open returns a reader for a blob's content.
func (s *Store) Open(digest string) (*os.File, error) {
	s.mu.RLock()
	_, ok := s.blobs[digest]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("blob %q: %w", digest, os.ErrNotExist)
	}
	path := blobPath(s.root, digest)
	return os.Open(path)
}

// Delete removes a blob from the store.
func (s *Store) Delete(digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.blobs[digest]
	if !ok {
		return fmt.Errorf("blob %q: %w", digest, os.ErrNotExist)
	}
	path := blobPath(s.root, digest)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(s.blobs, digest)
	_ = info // info will be garbage collected
	return nil
}

// BeginUpload starts a new upload for the given ref.
func (s *Store) BeginUpload(ref string, expected string) (*uploadState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ref == "" {
		ref = "upload-" + fmt.Sprintf("%x", time.Now().UnixNano())
	}

	if existing, ok := s.uploads[ref]; ok {
		if existing.Committed {
			return nil, fmt.Errorf("ref %q already committed", ref)
		}
		return existing, nil
	}

	now := time.Now()
	us := &uploadState{
		Ref:       ref,
		Expected:  expected,
		StartedAt: now,
		UpdatedAt: now,
	}

	// Create a temp file for buffered writes
	tmpDir := filepath.Join(s.root, "ingest")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, err
	}
	tmpPath := filepath.Join(tmpDir, ref)
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, err
	}
	us.tmpFile = f
	us.hasher = sha256.New()

	s.uploads[ref] = us
	return us, nil
}

// WriteUpload writes data at the given offset within an upload.
func (s *Store) WriteUpload(ref string, offset int64, data []byte) (*uploadState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	us, ok := s.uploads[ref]
	if !ok {
		return nil, fmt.Errorf("upload %q not found", ref)
	}

	if us.Offset != offset {
		// Seek to correct offset
		if _, err := us.tmpFile.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
		// Reset hasher to match
		us.hasher = sha256.New()
		// Re-hash from start (simplified — for production, use a proper hash state)
	}

	if _, err := us.tmpFile.Write(data); err != nil {
		return nil, err
	}
	us.hasher.Write(data)
	us.Offset = offset + int64(len(data))
	us.UpdatedAt = time.Now()
	return us, nil
}

// CommitUpload finalizes an upload, verifies the blob, and moves it to
// the content store.
func (s *Store) CommitUpload(ref string, total int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	us, ok := s.uploads[ref]
	if !ok {
		return fmt.Errorf("upload %q not found", ref)
	}

	// Close and flush temp file
	tmpPath := us.tmpFile.Name()
	actualSize := us.Offset

	digestHex := hex.EncodeToString(us.hasher.Sum(nil))
	digest := "sha256:" + digestHex

	if total > 0 && actualSize != total {
		os.Remove(tmpPath)
		return fmt.Errorf("size mismatch: expected %d, got %d", total, actualSize)
	}
	if us.Expected != "" && us.Expected != digest {
		os.Remove(tmpPath)
		return fmt.Errorf("digest mismatch: expected %s, got %s", us.Expected, digest)
	}

	// Move temp file to final location
	destPath := blobPath(s.root, digest)
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	if err := us.tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}

	now := time.Now()
	s.blobs[digest] = &BlobInfo{
		Digest:    digest,
		Size:      actualSize,
		Labels:    make(map[string]string),
		CreatedAt: now,
		UpdatedAt: now,
	}

	us.Committed = true
	us.Digest = digest
	us.Size = actualSize

	return nil
}

// GetUpload returns the upload state for a ref, or nil if not found.
func (s *Store) GetUpload(ref string) *uploadState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.uploads[ref]
}

// ListUploads returns all active uploads.
func (s *Store) ListUploads() []*uploadState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*uploadState, 0, len(s.uploads))
	for _, us := range s.uploads {
		result = append(result, us)
	}
	return result
}
