package e2b

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newTestVolumeClient builds a Client pointed at srv and a Volume handle with a
// fixed token, sharing srv as the content API host.
func newTestVolumeClient(t *testing.T, srv *httptest.Server) (*Client, *Volume) {
	t.Helper()
	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	vol := &Volume{VolumeID: "vol-123", Name: "myvol", Token: "vol-token", client: client}
	return client, vol
}

// sampleEntryStatJSON returns a JSON body for a VolumeEntryStat with the given
// name and type.
func sampleEntryStatJSON(name, typ string) string {
	return `{
		"name": "` + name + `",
		"type": "` + typ + `",
		"path": "/` + name + `",
		"size": 42,
		"mode": 420,
		"uid": 1000,
		"gid": 1000,
		"atime": "2026-07-22T10:00:00Z",
		"mtime": "2026-07-22T11:00:00Z",
		"ctime": "2026-07-22T12:00:00Z"
	}`
}

// --- Management API ---

func TestCreateVolume(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/volumes" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["name"] != "myvol" {
			t.Errorf("name = %q", body["name"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"volumeID":"vol-123","name":"myvol","token":"secret-token"}`))
	}))
	defer srv.Close()

	client, _ := newTestVolumeClient(t, srv)
	vol, err := client.CreateVolume(context.Background(), "myvol")
	if err != nil {
		t.Fatalf("CreateVolume: %v", err)
	}
	if vol.VolumeID != "vol-123" || vol.Name != "myvol" || vol.Token != "secret-token" {
		t.Errorf("volume = %+v", vol)
	}
	if vol.client != client {
		t.Errorf("volume.client not wired to client")
	}
}

func TestListVolumes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/volumes" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"volumeID":"v1","name":"a"},{"volumeID":"v2","name":"b"}]`))
	}))
	defer srv.Close()

	client, _ := newTestVolumeClient(t, srv)
	volumes, err := client.ListVolumes(context.Background())
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(volumes) != 2 || volumes[0].VolumeID != "v1" || volumes[1].Name != "b" {
		t.Errorf("volumes = %+v", volumes)
	}
}

func TestGetVolumeInfo(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/volumes/vol-123" {
				t.Errorf("path = %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"volumeID":"vol-123","name":"myvol","token":"tok"}`))
		}))
		defer srv.Close()

		client, _ := newTestVolumeClient(t, srv)
		info, err := client.GetVolumeInfo(context.Background(), "vol-123")
		if err != nil {
			t.Fatalf("GetVolumeInfo: %v", err)
		}
		if info.Token != "tok" {
			t.Errorf("token = %q", info.Token)
		}
	})

	t.Run("not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		client, _ := newTestVolumeClient(t, srv)
		_, err := client.GetVolumeInfo(context.Background(), "missing")
		var vnf *VolumeNotFoundError
		if !errors.As(err, &vnf) {
			t.Fatalf("expected *VolumeNotFoundError, got %v", err)
		}
		if vnf.VolumeID != "missing" {
			t.Errorf("VolumeID = %q", vnf.VolumeID)
		}
	})
}

func TestConnectVolume(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"volumeID":"vol-123","name":"myvol","token":"tok"}`))
	}))
	defer srv.Close()

	client, _ := newTestVolumeClient(t, srv)
	vol, err := client.ConnectVolume(context.Background(), "vol-123")
	if err != nil {
		t.Fatalf("ConnectVolume: %v", err)
	}
	if vol.Name != "myvol" || vol.Token != "tok" || vol.client != client {
		t.Errorf("volume = %+v", vol)
	}
}

func TestDestroyVolume(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantOK     bool
		wantErr    bool
		wantVolErr bool
	}{
		{"destroyed", http.StatusOK, true, false, false},
		{"destroyed 204", http.StatusNoContent, true, false, false},
		{"not found", http.StatusNotFound, false, false, false},
		{"server error", http.StatusInternalServerError, false, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("method = %s", r.Method)
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			client, _ := newTestVolumeClient(t, srv)
			ok, err := client.DestroyVolume(context.Background(), "vol-123")
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v", ok, tc.wantOK)
			}
			if tc.wantVolErr {
				var ve *VolumeError
				if !errors.As(err, &ve) {
					t.Errorf("expected *VolumeError, got %v", err)
				}
			}
		})
	}
}

// --- Content API ---

func TestVolumeList(t *testing.T) {
	t.Run("ok with depth", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/volumecontent/vol-123/dir" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer vol-token" {
				t.Errorf("Authorization = %q", got)
			}
			if got := r.URL.Query().Get("path"); got != "/data" {
				t.Errorf("path = %q", got)
			}
			if got := r.URL.Query().Get("depth"); got != "3" {
				t.Errorf("depth = %q", got)
			}
			_, _ = w.Write([]byte(`[` + sampleEntryStatJSON("a", "file") + `,` + sampleEntryStatJSON("b", "directory") + `]`))
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		entries, err := vol.List(context.Background(), "/data", WithVolumeDepth(3))
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(entries) != 2 || entries[0].Name != "a" || entries[1].Type != VolumeFileTypeDirectory {
			t.Errorf("entries = %+v", entries)
		}
		if !entries[0].ATime.Equal(time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)) {
			t.Errorf("atime = %v", entries[0].ATime)
		}
	})

	t.Run("depth omitted when unset", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := r.URL.Query()["depth"]; ok {
				t.Errorf("depth should be omitted, got %q", r.URL.Query().Get("depth"))
			}
			_, _ = w.Write([]byte(`[]`))
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		if _, err := vol.List(context.Background(), "/data"); err != nil {
			t.Fatalf("List: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		_, err := vol.List(context.Background(), "/missing")
		var fnf *FileNotFoundError
		if !errors.As(err, &fnf) {
			t.Fatalf("expected *FileNotFoundError, got %v", err)
		}
	})
}

func TestVolumeMakeDir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/volumecontent/vol-123/dir" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("uid") != "1000" || q.Get("gid") != "1000" || q.Get("mode") != "493" || q.Get("force") != "true" {
			t.Errorf("query = %v", q)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(sampleEntryStatJSON("newdir", "directory")))
	}))
	defer srv.Close()

	_, vol := newTestVolumeClient(t, srv)
	stat, err := vol.MakeDir(context.Background(), "/newdir",
		WithVolumeUID(1000), WithVolumeGID(1000), WithVolumeMode(0o755), WithVolumeForce(true))
	if err != nil {
		t.Fatalf("MakeDir: %v", err)
	}
	if stat.Type != VolumeFileTypeDirectory {
		t.Errorf("type = %q", stat.Type)
	}
}

func TestVolumeMakeDirOmitsUnsetParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for _, k := range []string{"uid", "gid", "mode", "force"} {
			if _, ok := q[k]; ok {
				t.Errorf("%s should be omitted, got %q", k, q.Get(k))
			}
		}
		_, _ = w.Write([]byte(sampleEntryStatJSON("newdir", "directory")))
	}))
	defer srv.Close()

	_, vol := newTestVolumeClient(t, srv)
	if _, err := vol.MakeDir(context.Background(), "/newdir"); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}
}

func TestVolumeMakeDirZeroValuesAreSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("uid") != "0" || q.Get("mode") != "0" {
			t.Errorf("zero values should be sent: uid=%q mode=%q", q.Get("uid"), q.Get("mode"))
		}
		_, _ = w.Write([]byte(sampleEntryStatJSON("newdir", "directory")))
	}))
	defer srv.Close()

	_, vol := newTestVolumeClient(t, srv)
	if _, err := vol.MakeDir(context.Background(), "/newdir", WithVolumeUID(0), WithVolumeMode(0)); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}
}

func TestVolumeGetInfoAndExists(t *testing.T) {
	t.Run("get info", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/volumecontent/vol-123/path" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			_, _ = w.Write([]byte(sampleEntryStatJSON("f", "file")))
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		stat, err := vol.GetInfo(context.Background(), "/f")
		if err != nil {
			t.Fatalf("GetInfo: %v", err)
		}
		if stat.Size != 42 || stat.Mode != 420 || stat.UID != 1000 {
			t.Errorf("stat = %+v", stat)
		}
	})

	t.Run("exists true", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(sampleEntryStatJSON("f", "file")))
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		ok, err := vol.Exists(context.Background(), "/f")
		if err != nil || !ok {
			t.Errorf("Exists = %v, %v", ok, err)
		}
	})

	t.Run("exists false", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		ok, err := vol.Exists(context.Background(), "/f")
		if err != nil || ok {
			t.Errorf("Exists = %v, %v", ok, err)
		}
	})
}

func TestVolumeUpdateMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/volumecontent/vol-123/path" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["uid"] != float64(1000) || body["mode"] != float64(420) {
			t.Errorf("body = %v", body)
		}
		if _, ok := body["gid"]; ok {
			t.Errorf("gid should be omitted, got %v", body["gid"])
		}
		_, _ = w.Write([]byte(sampleEntryStatJSON("f", "file")))
	}))
	defer srv.Close()

	_, vol := newTestVolumeClient(t, srv)
	if _, err := vol.UpdateMetadata(context.Background(), "/f", WithVolumeUID(1000), WithVolumeMode(0o644)); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
}

func TestVolumeReadFile(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/volumecontent/vol-123/file" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			if got := r.URL.Query().Get("path"); got != "/f.txt" {
				t.Errorf("path = %q", got)
			}
			_, _ = w.Write([]byte("hello world"))
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		got, err := vol.ReadFileString(context.Background(), "/f.txt")
		if err != nil {
			t.Fatalf("ReadFileString: %v", err)
		}
		if got != "hello world" {
			t.Errorf("content = %q", got)
		}
	})

	t.Run("bytes", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte{0x00, 0x01, 0x02})
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		got, err := vol.ReadFileBytes(context.Background(), "/f.bin")
		if err != nil {
			t.Fatalf("ReadFileBytes: %v", err)
		}
		if len(got) != 3 || got[2] != 0x02 {
			t.Errorf("content = %v", got)
		}
	})

	t.Run("stream close releases context", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("streamed"))
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		rc, err := vol.ReadFile(context.Background(), "/f.txt")
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if string(data) != "streamed" {
			t.Errorf("content = %q", data)
		}
	})

	t.Run("not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		_, err := vol.ReadFile(context.Background(), "/missing")
		var fnf *FileNotFoundError
		if !errors.As(err, &fnf) {
			t.Fatalf("expected *FileNotFoundError, got %v", err)
		}
	})
}

func TestVolumeWriteFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/volumecontent/vol-123/file" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("Content-Type = %q", got)
		}
		if r.URL.Query().Get("force") != "true" {
			t.Errorf("force = %q", r.URL.Query().Get("force"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "payload" {
			t.Errorf("body = %q", body)
		}
		_, _ = w.Write([]byte(sampleEntryStatJSON("f.txt", "file")))
	}))
	defer srv.Close()

	_, vol := newTestVolumeClient(t, srv)
	stat, err := vol.WriteFileString(context.Background(), "/f.txt", "payload", WithVolumeForce(true))
	if err != nil {
		t.Fatalf("WriteFileString: %v", err)
	}
	if stat.Name != "f.txt" {
		t.Errorf("name = %q", stat.Name)
	}
}

func TestVolumeRemove(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete || r.URL.Path != "/volumecontent/vol-123/path" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		if err := vol.Remove(context.Background(), "/f"); err != nil {
			t.Fatalf("Remove: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, vol := newTestVolumeClient(t, srv)
		err := vol.Remove(context.Background(), "/f")
		var fnf *FileNotFoundError
		if !errors.As(err, &fnf) {
			t.Fatalf("expected *FileNotFoundError, got %v", err)
		}
	})
}

func TestVolumeContentErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad path"}`))
	}))
	defer srv.Close()

	_, vol := newTestVolumeClient(t, srv)
	_, err := vol.GetInfo(context.Background(), "/f")
	var ve *VolumeError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *VolumeError, got %v", err)
	}
	if ve.StatusCode != http.StatusBadRequest || ve.Message != "bad path" {
		t.Errorf("VolumeError = %+v", ve)
	}
	if !strings.Contains(ve.Error(), "bad path") {
		t.Errorf("Error() = %q", ve.Error())
	}
}

func TestVolumeTokenGuard(t *testing.T) {
	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: "https://api.example.com"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	vol := &Volume{VolumeID: "vol-123", Name: "myvol", Token: "", client: client}

	_, err = vol.GetInfo(context.Background(), "/f")
	var ae *AuthenticationError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AuthenticationError, got %v", err)
	}
}

func TestVolumeEntryStatSymlink(t *testing.T) {
	body := `{
		"name": "link",
		"type": "symlink",
		"path": "/link",
		"size": 0,
		"mode": 511,
		"uid": 0,
		"gid": 0,
		"atime": "2026-07-22T10:00:00.5Z",
		"mtime": "2026-07-22T11:00:00Z",
		"ctime": "2026-07-22T12:00:00Z",
		"target": "/real/target"
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	_, vol := newTestVolumeClient(t, srv)
	stat, err := vol.GetInfo(context.Background(), "/link")
	if err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	if stat.Type != VolumeFileTypeSymlink || stat.Target != "/real/target" {
		t.Errorf("stat = %+v", stat)
	}
	// RFC3339Nano fractional seconds parse.
	if stat.ATime.Nanosecond() != 500_000_000 {
		t.Errorf("atime nanos = %d", stat.ATime.Nanosecond())
	}
}

func TestVolumeAsMount(t *testing.T) {
	vol := &Volume{VolumeID: "vol-123", Name: "myvol"}
	m := vol.AsMount("/mnt/data")
	if m.Name != "myvol" || m.Path != "/mnt/data" {
		t.Errorf("mount = %+v", m)
	}
}

func TestAddOptionalParams(t *testing.T) {
	uid, gid, depth := 5, 6, 2
	mode := uint32(0o644)
	force := true
	cfg := volumeReqConfig{uid: &uid, gid: &gid, mode: &mode, force: &force, depth: &depth}
	values := url.Values{}
	addOptionalParams(values, &cfg)
	if values.Get("uid") != "5" || values.Get("mode") != strconv.Itoa(0o644) || values.Get("force") != "true" || values.Get("depth") != "2" {
		t.Errorf("values = %v", values)
	}
}
