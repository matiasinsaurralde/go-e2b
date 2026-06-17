package e2b

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeFilesHashSingleFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	hash, data, err := computeFilesHash(dir, "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64 hex chars", len(hash))
	}
	if len(data) == 0 {
		t.Fatal("tar data is empty")
	}

	// Verify the tar contains exactly one file with correct content.
	names, contents := extractTar(t, data)
	if len(names) != 1 {
		t.Fatalf("tar entries = %d, want 1", len(names))
	}
	if names[0] != "hello.txt" {
		t.Errorf("tar entry name = %q, want %q", names[0], "hello.txt")
	}
	if string(contents[0]) != "hello world" {
		t.Errorf("tar entry content = %q, want %q", contents[0], "hello world")
	}
}

func TestComputeFilesHashDirectory(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "util.go"), []byte("package util"), 0644); err != nil {
		t.Fatal(err)
	}

	hash, data, err := computeFilesHash(dir, "src")
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash))
	}

	names, _ := extractTar(t, data)
	if len(names) != 2 {
		t.Fatalf("tar entries = %d, want 2", len(names))
	}
	// Files should be sorted: main.go before util.go.
	if names[0] != "main.go" {
		t.Errorf("tar entry[0] = %q, want %q", names[0], "main.go")
	}
	if names[1] != "util.go" {
		t.Errorf("tar entry[1] = %q, want %q", names[1], "util.go")
	}
}

func TestComputeFilesHashDeterministic(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "d"), 0755); err != nil {
		t.Fatal(err)
	}
	// Create files in reverse alphabetical order to ensure sorting produces same hash.
	for _, name := range []string{"c.txt", "b.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(dir, "d", name), []byte("content-"+name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	hash1, _, err := computeFilesHash(dir, "d")
	if err != nil {
		t.Fatal(err)
	}
	hash2, _, err := computeFilesHash(dir, "d")
	if err != nil {
		t.Fatal(err)
	}

	if hash1 != hash2 {
		t.Errorf("hash not deterministic: %q != %q", hash1, hash2)
	}
}

func TestComputeFilesHashDifferentContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("version1"), 0644); err != nil {
		t.Fatal(err)
	}

	hash1, _, err := computeFilesHash(dir, "f.txt")
	if err != nil {
		t.Fatal(err)
	}

	// Change the file content.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("version2"), 0644); err != nil {
		t.Fatal(err)
	}

	hash2, _, err := computeFilesHash(dir, "f.txt")
	if err != nil {
		t.Fatal(err)
	}

	if hash1 == hash2 {
		t.Error("different content produced the same hash")
	}
}

func TestComputeFilesHashNonexistent(t *testing.T) {
	_, _, err := computeFilesHash(t.TempDir(), "does-not-exist")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestComputeFilesHashAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	absPath := filepath.Join(dir, "abs.txt")
	if err := os.WriteFile(absPath, []byte("absolute"), 0644); err != nil {
		t.Fatal(err)
	}

	// When src is absolute, basePath should be ignored.
	hash, data, err := computeFilesHash("/ignored", absPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash))
	}
	if len(data) == 0 {
		t.Fatal("tar data is empty")
	}

	names, _ := extractTar(t, data)
	if len(names) != 1 {
		t.Fatalf("tar entries = %d, want 1", len(names))
	}
	if names[0] != "abs.txt" {
		t.Errorf("tar entry name = %q, want %q", names[0], "abs.txt")
	}
}

func TestComputeFilesHashPermissionAffectsHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")

	// Create with 0644.
	if err := os.WriteFile(path, []byte("#!/bin/sh"), 0644); err != nil {
		t.Fatal(err)
	}
	hash1, _, err := computeFilesHash(dir, "script.sh")
	if err != nil {
		t.Fatal(err)
	}

	// Change to 0755.
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
	hash2, _, err := computeFilesHash(dir, "script.sh")
	if err != nil {
		t.Fatal(err)
	}

	if hash1 == hash2 {
		t.Error("different permissions produced the same hash")
	}
}

// extractTar decompresses gzipped tar data and returns entry names and contents.
func extractTar(t *testing.T, data []byte) (names []string, contents [][]byte) {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %s: %v", hdr.Name, err)
		}
		names = append(names, hdr.Name)
		contents = append(contents, body)
	}
	return
}
