//go:build integration

package e2b

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// Run with:
//
//	E2B_API_KEY=e2b_xxx go test -tags=integration -v -run TestIntegrationVolume ./...

func integrationVolumeClient(t *testing.T) *Client {
	t.Helper()

	apiKey := os.Getenv("E2B_API_KEY")
	if apiKey == "" {
		t.Skip("E2B_API_KEY not set, skipping integration test")
	}

	client, err := NewClient(ClientConfig{APIKey: apiKey})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestIntegrationVolumeLifecycle(t *testing.T) {
	client := integrationVolumeClient(t)
	ctx := context.Background()

	name := fmt.Sprintf("go-sdk-test-%d", time.Now().UnixNano())

	// Create.
	vol, err := client.CreateVolume(ctx, name)
	if err != nil {
		t.Fatalf("CreateVolume: %v", err)
	}
	t.Logf("created volume %s (%s)", vol.VolumeID, vol.Name)
	if vol.Token == "" {
		t.Fatal("CreateVolume returned empty token")
	}

	// Ensure cleanup even if a later step fails.
	destroyed := false
	t.Cleanup(func() {
		if destroyed {
			return
		}
		if _, err := client.DestroyVolume(ctx, vol.VolumeID); err != nil {
			t.Logf("cleanup DestroyVolume: %v", err)
		}
	})

	// Write a file.
	const content = "hello from the go sdk"
	if _, err := vol.WriteFileString(ctx, "/greeting.txt", content); err != nil {
		t.Fatalf("WriteFileString: %v", err)
	}

	// GetInfo + Exists.
	stat, err := vol.GetInfo(ctx, "/greeting.txt")
	if err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	if stat.Type != VolumeFileTypeFile {
		t.Errorf("type = %q, want file", stat.Type)
	}
	if stat.Size != int64(len(content)) {
		t.Errorf("size = %d, want %d", stat.Size, len(content))
	}

	ok, err := vol.Exists(ctx, "/greeting.txt")
	if err != nil || !ok {
		t.Errorf("Exists(/greeting.txt) = %v, %v", ok, err)
	}
	ok, err = vol.Exists(ctx, "/does-not-exist")
	if err != nil || ok {
		t.Errorf("Exists(/does-not-exist) = %v, %v", ok, err)
	}

	// Read back (round-trip).
	got, err := vol.ReadFileString(ctx, "/greeting.txt")
	if err != nil {
		t.Fatalf("ReadFileString: %v", err)
	}
	if got != content {
		t.Errorf("read content = %q, want %q", got, content)
	}

	// MakeDir.
	if _, err := vol.MakeDir(ctx, "/subdir"); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}

	// List (with depth).
	entries, err := vol.List(ctx, "/", WithVolumeDepth(2))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) == 0 {
		t.Error("List returned no entries")
	}
	t.Logf("listed %d entries at /", len(entries))

	// UpdateMetadata.
	if _, err := vol.UpdateMetadata(ctx, "/greeting.txt", WithVolumeMode(0o600)); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	// Remove.
	if err := vol.Remove(ctx, "/greeting.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	ok, err = vol.Exists(ctx, "/greeting.txt")
	if err != nil || ok {
		t.Errorf("Exists after Remove = %v, %v", ok, err)
	}

	// Remove missing path → FileNotFoundError.
	err = vol.Remove(ctx, "/greeting.txt")
	var fnf *FileNotFoundError
	if !errors.As(err, &fnf) {
		t.Errorf("Remove(missing): expected *FileNotFoundError, got %v", err)
	}

	// ListVolumes contains it.
	volumes, err := client.ListVolumes(ctx)
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	found := false
	for _, v := range volumes {
		if v.VolumeID == vol.VolumeID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListVolumes does not contain %s", vol.VolumeID)
	}

	// GetVolumeInfo + ConnectVolume round-trip.
	info, err := client.GetVolumeInfo(ctx, vol.VolumeID)
	if err != nil {
		t.Fatalf("GetVolumeInfo: %v", err)
	}
	if info.Name != name {
		t.Errorf("GetVolumeInfo name = %q, want %q", info.Name, name)
	}

	connected, err := client.ConnectVolume(ctx, vol.VolumeID)
	if err != nil {
		t.Fatalf("ConnectVolume: %v", err)
	}
	if connected.Token == "" {
		t.Error("ConnectVolume returned empty token")
	}

	// Destroy (true), then again (false).
	gone, err := client.DestroyVolume(ctx, vol.VolumeID)
	if err != nil {
		t.Fatalf("DestroyVolume: %v", err)
	}
	if !gone {
		t.Error("DestroyVolume returned false for existing volume")
	}
	destroyed = true

	gone, err = client.DestroyVolume(ctx, vol.VolumeID)
	if err != nil {
		t.Fatalf("DestroyVolume (second): %v", err)
	}
	if gone {
		t.Error("DestroyVolume returned true for already-destroyed volume")
	}

	// GetVolumeInfo now → VolumeNotFoundError.
	_, err = client.GetVolumeInfo(ctx, vol.VolumeID)
	var vnf *VolumeNotFoundError
	if !errors.As(err, &vnf) {
		t.Errorf("GetVolumeInfo after destroy: expected *VolumeNotFoundError, got %v", err)
	}
}
