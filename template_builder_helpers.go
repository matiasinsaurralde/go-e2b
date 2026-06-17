package e2b

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// computeFilesHash computes a SHA-256 hash over the contents of src (file or
// directory) and produces a gzipped tar archive of those files. The hash is
// deterministic: files are sorted by relative path, and each file contributes
// its path, content, and permission mode to the hash.
//
// basePath is the directory that src is relative to. If src is absolute,
// basePath is ignored.
func computeFilesHash(basePath, src string) (hash string, tarData []byte, err error) {
	resolved := src
	if !filepath.IsAbs(src) {
		resolved = filepath.Join(basePath, src)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", nil, fmt.Errorf("e2b: stat %s: %w", resolved, err)
	}

	type fileEntry struct {
		relPath string
		absPath string
		info    fs.FileInfo
	}

	var entries []fileEntry

	if info.IsDir() {
		err = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(resolved, path)
			if relErr != nil {
				return relErr
			}
			fi, fiErr := d.Info()
			if fiErr != nil {
				return fiErr
			}
			entries = append(entries, fileEntry{relPath: rel, absPath: path, info: fi})
			return nil
		})
		if err != nil {
			return "", nil, fmt.Errorf("e2b: walk %s: %w", resolved, err)
		}
	} else {
		entries = append(entries, fileEntry{
			relPath: filepath.Base(resolved),
			absPath: resolved,
			info:    info,
		})
	}

	// Sort for deterministic hashing.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].relPath < entries[j].relPath
	})

	// Compute hash and build tar in a single pass.
	h := sha256.New()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, e := range entries {
		content, readErr := os.ReadFile(e.absPath)
		if readErr != nil {
			return "", nil, fmt.Errorf("e2b: read %s: %w", e.absPath, readErr)
		}

		// Hash: path + content + mode (octal).
		_, _ = io.WriteString(h, e.relPath)
		_, _ = h.Write(content)
		_, _ = io.WriteString(h, fmt.Sprintf("%o", e.info.Mode().Perm()))

		// Tar entry.
		hdr := &tar.Header{
			Name: e.relPath,
			Size: int64(len(content)),
			Mode: int64(e.info.Mode().Perm()),
		}
		if err = tw.WriteHeader(hdr); err != nil {
			return "", nil, fmt.Errorf("e2b: tar header %s: %w", e.relPath, err)
		}
		if _, err = tw.Write(content); err != nil {
			return "", nil, fmt.Errorf("e2b: tar write %s: %w", e.relPath, err)
		}
	}

	if err = tw.Close(); err != nil {
		return "", nil, fmt.Errorf("e2b: close tar: %w", err)
	}
	if err = gw.Close(); err != nil {
		return "", nil, fmt.Errorf("e2b: close gzip: %w", err)
	}

	return fmt.Sprintf("%x", h.Sum(nil)), buf.Bytes(), nil
}
