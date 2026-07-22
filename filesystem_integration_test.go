//go:build integration

package e2b

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Run with:
//
//	E2B_API_KEY=e2b_xxx E2B_TEMPLATE=nlhz8vlwyupq845jsdg9 go test -tags=integration -v -run TestIntegrationFilesystem ./...

func integrationFilesystemClient(t *testing.T) (*Client, string) {
	t.Helper()

	apiKey := os.Getenv("E2B_API_KEY")
	if apiKey == "" {
		t.Skip("E2B_API_KEY not set, skipping integration test")
	}

	template := os.Getenv("E2B_TEMPLATE")
	if template == "" {
		template = "base"
	}

	client, err := NewClient(ClientConfig{APIKey: apiKey})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, template
}

func newIntegrationSandbox(t *testing.T) *Sandbox {
	t.Helper()

	client, template := integrationFilesystemClient(t)

	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		Template: template,
		Timeout:  300,
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	t.Cleanup(func() {
		if err := sbx.Close(); err != nil {
			t.Logf("cleanup Close: %v", err)
		}
	})
	return sbx
}

// --- Integration: Write → Read round-trip ---

func TestIntegrationFilesystemWriteReadRoundTrip(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	const content = "hello from integration test"
	const path = "/tmp/integration_roundtrip.txt"

	// Write
	info, err := sbx.Filesystem.WriteString(ctx, path, content)
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if info.Path != path {
		t.Errorf("WriteString Path = %q, want %q", info.Path, path)
	}
	t.Logf("wrote file: name=%s size=%d", info.Name, info.Size)

	// Read
	got, err := sbx.Filesystem.ReadString(ctx, path)
	if err != nil {
		t.Fatalf("ReadString: %v", err)
	}
	if got != content {
		t.Errorf("ReadString = %q, want %q", got, content)
	}
	t.Logf("read file: content=%q", got)
}

// --- Integration: List directory ---

func TestIntegrationFilesystemListRoot(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	entries, err := sbx.Filesystem.List(ctx, "/")
	if err != nil {
		t.Fatalf("List(/): %v", err)
	}

	t.Logf("List(/) returned %d entries:", len(entries))
	foundHome := false
	foundTmp := false
	foundEtc := false
	for _, e := range entries {
		t.Logf("  [%s] %s (size=%d, mtime=%v)", e.Type, e.Name, e.Size, e.ModTime)
		switch e.Name {
		case "home":
			foundHome = true
		case "tmp":
			foundTmp = true
		case "etc":
			foundEtc = true
		}
	}

	if !foundHome {
		t.Error("expected /home in root listing")
	}
	if !foundTmp {
		t.Error("expected /tmp in root listing")
	}
	if !foundEtc {
		t.Error("expected /etc in root listing")
	}
}

func TestIntegrationFilesystemListHome(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	entries, err := sbx.Filesystem.List(ctx, "/home")
	if err != nil {
		t.Fatalf("List(/home): %v", err)
	}

	t.Logf("List(/home) returned %d entries:", len(entries))
	foundUser := false
	for _, e := range entries {
		t.Logf("  [%s] %s (size=%d)", e.Type, e.Name, e.Size)
		if e.Name == "user" {
			foundUser = true
		}
	}
	if !foundUser {
		t.Error("expected 'user' directory in /home listing")
	}
}

func TestIntegrationFilesystemListEmptyDir(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	// Create an empty directory.
	dirPath := "/tmp/integration_empty_dir"
	if _, err := sbx.Filesystem.MakeDir(ctx, dirPath); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}

	// List should return empty or just "." entries.
	entries, err := sbx.Filesystem.List(ctx, dirPath)
	if err != nil {
		t.Fatalf("List(%s): %v", dirPath, err)
	}
	t.Logf("List(%s) returned %d entries", dirPath, len(entries))
}

func TestIntegrationFilesystemListWithFiles(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	dirPath := "/tmp/integration_list_test"
	if _, err := sbx.Filesystem.MakeDir(ctx, dirPath); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}

	// Create a few files.
	files := []string{"alpha.txt", "beta.txt", "gamma.log"}
	for _, f := range files {
		_, err := sbx.Filesystem.WriteString(ctx, dirPath+"/"+f, "content-"+f)
		if err != nil {
			t.Fatalf("WriteString %s: %v", f, err)
		}
	}

	entries, err := sbx.Filesystem.List(ctx, dirPath)
	if err != nil {
		t.Fatalf("List(%s): %v", dirPath, err)
	}

	t.Logf("List(%s) returned %d entries:", dirPath, len(entries))
	found := make(map[string]bool)
	for _, e := range entries {
		t.Logf("  [%s] %s (size=%d)", e.Type, e.Name, e.Size)
		found[e.Name] = true
	}

	for _, f := range files {
		if !found[f] {
			t.Errorf("expected %q in listing, not found", f)
		}
	}
}

func TestIntegrationFilesystemListNotFound(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	_, err := sbx.Filesystem.List(ctx, "/nonexistent/path/xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	t.Logf("expected error: %v", err)
}

// --- Integration: Stat file/directory ---

func TestIntegrationFilesystemStatFile(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	const content = "stat file content"
	const path = "/tmp/integration_stat_file.txt"

	_, err := sbx.Filesystem.WriteString(ctx, path, content)
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	info, err := sbx.Filesystem.Stat(ctx, path)
	if err != nil {
		t.Fatalf("Stat(%s): %v", path, err)
	}

	t.Logf("Stat file: name=%s type=%s size=%d path=%s mtime=%v",
		info.Name, info.Type, info.Size, info.Path, info.ModTime)

	if info.Name != "integration_stat_file.txt" {
		t.Errorf("Name = %q, want %q", info.Name, "integration_stat_file.txt")
	}
	if info.Type != "file" {
		t.Errorf("Type = %q, want file", info.Type)
	}
}

func TestIntegrationFilesystemStatDirectory(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	const dirPath = "/tmp/integration_stat_dir"
	if _, err := sbx.Filesystem.MakeDir(ctx, dirPath); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}

	info, err := sbx.Filesystem.Stat(ctx, dirPath)
	if err != nil {
		t.Fatalf("Stat(%s): %v", dirPath, err)
	}

	t.Logf("Stat directory: name=%s type=%s path=%s mtime=%v",
		info.Name, info.Type, info.Path, info.ModTime)

	if info.Name != "integration_stat_dir" {
		t.Errorf("Name = %q, want %q", info.Name, "integration_stat_dir")
	}
	if info.Type != "directory" {
		t.Errorf("Type = %q, want directory", info.Type)
	}
}

func TestIntegrationFilesystemStatRoot(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	info, err := sbx.Filesystem.Stat(ctx, "/")
	if err != nil {
		t.Fatalf("Stat(/): %v", err)
	}

	t.Logf("Stat /: name=%s type=%s", info.Name, info.Type)

	if info.Type != "directory" {
		t.Errorf("Type = %q, want directory", info.Type)
	}
}

func TestIntegrationFilesystemStatNotFound(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	_, err := sbx.Filesystem.Stat(ctx, "/nonexistent/file.xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	t.Logf("expected error: %v", err)
}

// --- Integration: MakeDir ---

func TestIntegrationFilesystemMakeDirSimple(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	dirPath := "/tmp/integration_mkdir_simple"
	if _, err := sbx.Filesystem.MakeDir(ctx, dirPath); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}

	// Verify it exists by listing.
	entries, err := sbx.Filesystem.List(ctx, "/tmp")
	if err != nil {
		t.Fatalf("List(/tmp): %v", err)
	}

	found := false
	for _, e := range entries {
		if e.Name == "integration_mkdir_simple" {
			found = true
			if e.Type != "directory" {
				t.Errorf("Type = %q, want directory", e.Type)
			}
			break
		}
	}
	if !found {
		t.Error("created directory not found in /tmp listing")
	}
}

func TestIntegrationFilesystemMakeDirNested(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	// Create nested directories in one call (mkdir -p semantics).
	nestedPath := "/tmp/integration_mkdir_nested/a/b/c"
	if _, err := sbx.Filesystem.MakeDir(ctx, nestedPath); err != nil {
		t.Fatalf("MakeDir nested: %v", err)
	}

	// Verify that the deepest directory exists.
	info, err := sbx.Filesystem.Stat(ctx, nestedPath)
	if err != nil {
		t.Fatalf("Stat(%s): %v", nestedPath, err)
	}
	if info.Type != "directory" {
		t.Errorf("Type = %q, want directory", info.Type)
	}
	t.Logf("nested dir created: name=%s type=%s", info.Name, info.Type)

	// Also verify parent directories exist.
	for _, p := range []string{
		"/tmp/integration_mkdir_nested",
		"/tmp/integration_mkdir_nested/a",
		"/tmp/integration_mkdir_nested/a/b",
	} {
		info, err := sbx.Filesystem.Stat(ctx, p)
		if err != nil {
			t.Errorf("Stat(%s): %v", p, err)
		} else {
			t.Logf("  parent %s: type=%s", p, info.Type)
		}
	}
}

// --- Integration: Remove ---

func TestIntegrationFilesystemRemoveFile(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	const path = "/tmp/integration_remove_file.txt"
	_, err := sbx.Filesystem.WriteString(ctx, path, "to be deleted")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	// Remove it.
	if err := sbx.Filesystem.Remove(ctx, path); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Verify it's gone.
	_, err = sbx.Filesystem.Stat(ctx, path)
	if err == nil {
		t.Fatal("expected error: file should be deleted")
	}
	t.Logf("file removed, Stat returns: %v", err)
}

func TestIntegrationFilesystemRemoveDirectory(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	dirPath := "/tmp/integration_remove_dir"
	if _, err := sbx.Filesystem.MakeDir(ctx, dirPath); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}

	// Add a file inside.
	_, err := sbx.Filesystem.WriteString(ctx, dirPath+"/nested.txt", "inside")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	// Remove recursively.
	if err := sbx.Filesystem.Remove(ctx, dirPath); err != nil {
		t.Fatalf("Remove dir: %v", err)
	}

	// Verify it's gone.
	_, err = sbx.Filesystem.Stat(ctx, dirPath)
	if err == nil {
		t.Fatal("expected error: directory should be deleted")
	}
	t.Logf("directory removed, Stat returns: %v", err)
}

func TestIntegrationFilesystemRemoveNotFound(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	// envd treats removing a non-existent path as a no-op (idempotent).
	err := sbx.Filesystem.Remove(ctx, "/nonexistent/file.txt")
	if err != nil {
		t.Logf("Remove returned error (also acceptable): %v", err)
	} else {
		t.Log("Remove of non-existent path is a no-op (expected envd behavior)")
	}

	// Verify it really doesn't exist.
	exists, err := sbx.Filesystem.Exists(ctx, "/nonexistent/file.txt")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("non-existent path should not exist")
	}
}

// --- Integration: Full workflow (Write → Stat → List → Remove → Verify) ---

func TestIntegrationFilesystemFullWorkflow(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	workDir := "/tmp/integration_workflow"

	// Step 1: MakeDir
	t.Log("Step 1: MakeDir")
	if _, err := sbx.Filesystem.MakeDir(ctx, workDir); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}

	// Step 2: Write files
	t.Log("Step 2: Write files")
	files := map[string]string{
		"hello.txt":   "Hello, World!",
		"config.json": `{"key":"value"}`,
		"data.bin":    "\x00\x01\x02\x03",
	}
	for name, content := range files {
		info, err := sbx.Filesystem.WriteString(ctx, workDir+"/"+name, content)
		if err != nil {
			t.Fatalf("WriteString %s: %v", name, err)
		}
		t.Logf("  wrote %s: path=%s size=%d", name, info.Path, info.Size)
	}

	// Step 3: Stat each file
	t.Log("Step 3: Stat each file")
	for name := range files {
		info, err := sbx.Filesystem.Stat(ctx, workDir+"/"+name)
		if err != nil {
			t.Fatalf("Stat %s: %v", name, err)
		}
		t.Logf("  stat %s: type=%s size=%d mtime=%v", name, info.Type, info.Size, info.ModTime)
		if info.Type != "file" {
			t.Errorf("Stat %s: type=%q, want file", name, info.Type)
		}
	}

	// Step 4: List directory
	t.Log("Step 4: List directory")
	entries, err := sbx.Filesystem.List(ctx, workDir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	t.Logf("  List returned %d entries:", len(entries))
	if len(entries) != len(files) {
		t.Errorf("List returned %d entries, want %d", len(entries), len(files))
	}
	for _, e := range entries {
		t.Logf("    [%s] %s (size=%d)", e.Type, e.Name, e.Size)
		if _, ok := files[e.Name]; !ok {
			t.Errorf("unexpected file in listing: %s", e.Name)
		}
	}

	// Step 5: Read back content
	t.Log("Step 5: Read back content")
	for name, expected := range files {
		got, err := sbx.Filesystem.ReadString(ctx, workDir+"/"+name)
		if err != nil {
			t.Fatalf("ReadString %s: %v", name, err)
		}
		if got != expected {
			t.Errorf("ReadString %s: got %q, want %q", name, got, expected)
		}
	}

	// Step 6: Remove all files
	t.Log("Step 6: Remove files")
	for name := range files {
		if err := sbx.Filesystem.Remove(ctx, workDir+"/"+name); err != nil {
			t.Fatalf("Remove %s: %v", name, err)
		}
		t.Logf("  removed %s", name)
	}

	// Step 7: Verify directory is now empty
	t.Log("Step 7: Verify empty")
	entries, err = sbx.Filesystem.List(ctx, workDir)
	if err != nil {
		t.Fatalf("List after clean: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("List after clean returned %d entries, want 0", len(entries))
	}

	// Step 8: Remove empty directory
	t.Log("Step 8: Remove directory")
	if err := sbx.Filesystem.Remove(ctx, workDir); err != nil {
		t.Fatalf("Remove dir: %v", err)
	}

	// Step 9: Verify directory is gone
	t.Log("Step 9: Verify directory gone")
	_, err = sbx.Filesystem.Stat(ctx, workDir)
	if err == nil {
		t.Fatal("expected error: directory should be gone")
	}
	t.Logf("workflow complete, directory removed: %v", err)
}

// --- Integration: List with user context ---

func TestIntegrationFilesystemListWithUser(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	// Test listing with root user.
	entries, err := sbx.Filesystem.List(ctx, "/root", WithFileUser("root"))
	if err != nil {
		t.Fatalf("List(/root, user=root): %v", err)
	}

	t.Logf("List(/root, user=root) returned %d entries:", len(entries))
	for _, e := range entries {
		t.Logf("  [%s] %s", e.Type, e.Name)
	}
}

// --- Integration: Stat with user context ---

func TestIntegrationFilesystemStatWithUser(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	// Write a file as root in /root.
	rootFile := "/root/integration_stat_user.txt"
	_, err := sbx.Filesystem.WriteString(ctx, rootFile, "root content", WithFileUser("root"))
	if err != nil {
		t.Fatalf("WriteString(root): %v", err)
	}

	// Stat as root should succeed.
	info, err := sbx.Filesystem.Stat(ctx, rootFile, WithFileUser("root"))
	if err != nil {
		t.Fatalf("Stat(%s, user=root): %v", rootFile, err)
	}
	t.Logf("stat as root: name=%s type=%s size=%d", info.Name, info.Type, info.Size)
}

// --- Integration: MakeDir with user context ---

func TestIntegrationFilesystemMakeDirWithUser(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	dirPath := "/root/integration_mkdir_user"
	if _, err := sbx.Filesystem.MakeDir(ctx, dirPath, WithFileUser("root")); err != nil {
		t.Fatalf("MakeDir(%s, user=root): %v", dirPath, err)
	}

	// Verify as root.
	info, err := sbx.Filesystem.Stat(ctx, dirPath, WithFileUser("root"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	t.Logf("created as root: name=%s type=%s", info.Name, info.Type)
}

// --- Integration: Concurrent operations test ---

func TestIntegrationFilesystemConcurrent(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	baseDir := "/tmp/integration_concurrent"
	if _, err := sbx.Filesystem.MakeDir(ctx, baseDir); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}

	const numGoroutines = 10
	errCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			path := fmt.Sprintf("%s/file_%d.txt", baseDir, idx)
			content := fmt.Sprintf("goroutine %d", idx)

			_, err := sbx.Filesystem.WriteString(ctx, path, content)
			errCh <- err
		}(i)
	}

	// Wait for all writes.
	for i := 0; i < numGoroutines; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("WriteString goroutine %d: %v", i, err)
		}
	}

	// List and verify all files are present.
	entries, err := sbx.Filesystem.List(ctx, baseDir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(entries) != numGoroutines {
		t.Errorf("List returned %d entries, want %d", len(entries), numGoroutines)
	}
	t.Logf("concurrent writes: %d files created and listed", len(entries))
}

// --- Integration: Exists ---

func TestIntegrationFilesystemExistsFile(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	const path = "/tmp/integration_exists_file.txt"
	_, err := sbx.Filesystem.WriteString(ctx, path, "exists test")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	exists, err := sbx.Filesystem.Exists(ctx, path)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Error("file should exist")
	}
	t.Logf("file exists: %v", exists)
}

func TestIntegrationFilesystemExistsDirectory(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	exists, err := sbx.Filesystem.Exists(ctx, "/tmp")
	if err != nil {
		t.Fatalf("Exists(/tmp): %v", err)
	}
	if !exists {
		t.Error("/tmp should exist")
	}
	t.Logf("/tmp exists: %v", exists)
}

func TestIntegrationFilesystemExistsNonExistent(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	exists, err := sbx.Filesystem.Exists(ctx, "/nonexistent/path/xyz")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("non-existent path should return false")
	}
	t.Logf("non-existent path exists: %v", exists)
}

// --- Integration: Rename ---

func TestIntegrationFilesystemRenameFile(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	oldPath := "/tmp/integration_rename_old.txt"
	newPath := "/tmp/integration_rename_new.txt"

	_, err := sbx.Filesystem.WriteString(ctx, oldPath, "rename me")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	info, err := sbx.Filesystem.Rename(ctx, oldPath, newPath)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	t.Logf("renamed: name=%s path=%s type=%s", info.Name, info.Path, info.Type)

	if info.Name != "integration_rename_new.txt" {
		t.Errorf("Name = %q, want integration_rename_new.txt", info.Name)
	}
	if info.Path != newPath {
		t.Errorf("Path = %q, want %q", info.Path, newPath)
	}

	// Old path should not exist.
	exists, err := sbx.Filesystem.Exists(ctx, oldPath)
	if err != nil {
		t.Fatalf("Exists(old): %v", err)
	}
	if exists {
		t.Error("old path should not exist after rename")
	}

	// New path should exist.
	exists, err = sbx.Filesystem.Exists(ctx, newPath)
	if err != nil {
		t.Fatalf("Exists(new): %v", err)
	}
	if !exists {
		t.Error("new path should exist after rename")
	}

	// Read content from new path.
	content, err := sbx.Filesystem.ReadString(ctx, newPath)
	if err != nil {
		t.Fatalf("ReadString: %v", err)
	}
	if content != "rename me" {
		t.Errorf("content = %q, want %q", content, "rename me")
	}
}

func TestIntegrationFilesystemRenameDirectory(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	oldDir := "/tmp/integration_rename_old_dir"
	newDir := "/tmp/integration_rename_new_dir"

	if _, err := sbx.Filesystem.MakeDir(ctx, oldDir); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}
	// Write a file inside.
	_, err := sbx.Filesystem.WriteString(ctx, oldDir+"/nested.txt", "inside")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	info, err := sbx.Filesystem.Rename(ctx, oldDir, newDir)
	if err != nil {
		t.Fatalf("Rename dir: %v", err)
	}
	t.Logf("renamed dir: name=%s path=%s type=%s", info.Name, info.Path, info.Type)

	// Content inside should still be accessible.
	content, err := sbx.Filesystem.ReadString(ctx, newDir+"/nested.txt")
	if err != nil {
		t.Fatalf("ReadString nested: %v", err)
	}
	if content != "inside" {
		t.Errorf("nested content = %q, want inside", content)
	}
}

// --- Integration: WatchDir ---

func TestIntegrationFilesystemWatchDir(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	watchDir := "/tmp/integration_watch_dir"
	if _, err := sbx.Filesystem.MakeDir(ctx, watchDir); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}

	// Start watching the directory.
	handle, err := sbx.Filesystem.WatchDir(ctx, watchDir, false)
	if err != nil {
		t.Fatalf("WatchDir: %v", err)
	}
	defer func() {
		if err := handle.Stop(ctx); err != nil {
			t.Logf("Stop watcher: %v", err)
		}
	}()

	// Write a file to trigger a create event.
	_, err = sbx.Filesystem.WriteString(ctx, watchDir+"/watch_test.txt", "hello")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	// Give the filesystem a moment to register events.
	time.Sleep(500 * time.Millisecond)

	events, err := handle.GetEvents(ctx)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}

	t.Logf("watch events: %d", len(events))
	for _, e := range events {
		t.Logf("  event: type=%s name=%s", e.Type, e.Name)
	}

	if len(events) == 0 {
		t.Log("no events received (envd may batch or delay delivery)")
	}
}

func TestIntegrationFilesystemWatchDirRecursive(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	watchDir := "/tmp/integration_watch_recursive"
	if _, err := sbx.Filesystem.MakeDir(ctx, watchDir); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}
	if _, err := sbx.Filesystem.MakeDir(ctx, watchDir+"/sub"); err != nil {
		t.Fatalf("MakeDir sub: %v", err)
	}

	handle, err := sbx.Filesystem.WatchDir(ctx, watchDir, true)
	if err != nil {
		t.Fatalf("WatchDir recursive: %v", err)
	}
	defer func() {
		if err := handle.Stop(ctx); err != nil {
			t.Logf("Stop watcher: %v", err)
		}
	}()

	// Write a file in the subdirectory.
	_, err = sbx.Filesystem.WriteString(ctx, watchDir+"/sub/deep.txt", "deep")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	events, err := handle.GetEvents(ctx)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}

	t.Logf("recursive watch events: %d", len(events))
	for _, e := range events {
		t.Logf("  event: type=%s name=%s", e.Type, e.Name)
	}
}
