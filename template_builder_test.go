package e2b

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNewTemplate(t *testing.T) {
	b := NewTemplate()
	if b == nil {
		t.Fatal("NewTemplate() returned nil")
	}
	if len(b.steps) != 0 {
		t.Errorf("steps = %d, want 0", len(b.steps))
	}
}

func TestFromImage(t *testing.T) {
	b := NewTemplate().FromImage("python:3.11")
	if b.fromImage != "python:3.11" {
		t.Errorf("fromImage = %q, want %q", b.fromImage, "python:3.11")
	}
	if b.fromTemplate != "" {
		t.Errorf("fromTemplate = %q, want empty", b.fromTemplate)
	}
}

func TestFromTemplate(t *testing.T) {
	b := NewTemplate().FromTemplate("tmpl-abc")
	if b.fromTemplate != "tmpl-abc" {
		t.Errorf("fromTemplate = %q, want %q", b.fromTemplate, "tmpl-abc")
	}
	if b.fromImage != "" {
		t.Errorf("fromImage = %q, want empty", b.fromImage)
	}
}

func TestFromBaseImage(t *testing.T) {
	b := NewTemplate().FromBaseImage()
	if b.fromImage != "" {
		t.Errorf("fromImage = %q, want empty", b.fromImage)
	}
	if b.fromTemplate != "" {
		t.Errorf("fromTemplate = %q, want empty", b.fromTemplate)
	}
}

func TestFromImageClearsFromTemplate(t *testing.T) {
	b := NewTemplate().FromTemplate("tmpl-abc").FromImage("python:3.11")
	if b.fromImage != "python:3.11" {
		t.Errorf("fromImage = %q, want %q", b.fromImage, "python:3.11")
	}
	if b.fromTemplate != "" {
		t.Errorf("fromTemplate = %q, want empty after FromImage", b.fromTemplate)
	}
}

func TestRunCmd(t *testing.T) {
	b := NewTemplate().RunCmd("echo hello")
	if len(b.steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(b.steps))
	}
	s := b.steps[0]
	if s.Type != "run" {
		t.Errorf("Type = %q, want %q", s.Type, "run")
	}
	if len(s.Args) != 1 || s.Args[0] != "echo hello" {
		t.Errorf("Args = %v, want [echo hello]", s.Args)
	}
	if s.Force {
		t.Error("Force = true, want false")
	}
}

func TestCopy(t *testing.T) {
	b := NewTemplate().Copy("main.py", "/app/main.py")
	if len(b.steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(b.steps))
	}
	s := b.steps[0]
	if s.Type != "copy" {
		t.Errorf("Type = %q, want %q", s.Type, "copy")
	}
	if len(s.Args) != 2 || s.Args[0] != "main.py" || s.Args[1] != "/app/main.py" {
		t.Errorf("Args = %v, want [main.py /app/main.py]", s.Args)
	}
	if len(b.fileBundles) != 1 {
		t.Fatalf("len(fileBundles) = %d, want 1", len(b.fileBundles))
	}
	if b.fileBundles[0].step != 0 {
		t.Errorf("fileBundle.step = %d, want 0", b.fileBundles[0].step)
	}
}

func TestSetEnvsSorted(t *testing.T) {
	b := NewTemplate().SetEnvs(map[string]string{
		"PORT":     "8080",
		"API_KEY":  "secret",
		"LOG_LEVEL": "info",
	})
	if len(b.steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(b.steps))
	}
	s := b.steps[0]
	if s.Type != "env" {
		t.Errorf("Type = %q, want %q", s.Type, "env")
	}
	// Keys should be sorted: API_KEY, LOG_LEVEL, PORT
	want := []string{"API_KEY=secret", "LOG_LEVEL=info", "PORT=8080"}
	if len(s.Args) != len(want) {
		t.Fatalf("len(Args) = %d, want %d", len(s.Args), len(want))
	}
	for i, w := range want {
		if s.Args[i] != w {
			t.Errorf("Args[%d] = %q, want %q", i, s.Args[i], w)
		}
	}
}

func TestSetWorkdir(t *testing.T) {
	b := NewTemplate().SetWorkdir("/app")
	if len(b.steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(b.steps))
	}
	s := b.steps[0]
	if s.Type != "workdir" {
		t.Errorf("Type = %q, want %q", s.Type, "workdir")
	}
	if len(s.Args) != 1 || s.Args[0] != "/app" {
		t.Errorf("Args = %v, want [/app]", s.Args)
	}
}

func TestSetUser(t *testing.T) {
	b := NewTemplate().SetUser("root")
	if len(b.steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(b.steps))
	}
	s := b.steps[0]
	if s.Type != "user" {
		t.Errorf("Type = %q, want %q", s.Type, "user")
	}
	if len(s.Args) != 1 || s.Args[0] != "root" {
		t.Errorf("Args = %v, want [root]", s.Args)
	}
}

func TestAptInstall(t *testing.T) {
	b := NewTemplate().AptInstall("curl", "git")
	if len(b.steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(b.steps))
	}
	s := b.steps[0]
	if s.Type != "run" {
		t.Errorf("Type = %q, want %q", s.Type, "run")
	}
	if !strings.Contains(s.Args[0], "apt-get install -y curl git") {
		t.Errorf("Args[0] = %q, want apt-get install command", s.Args[0])
	}
	if !strings.Contains(s.Args[0], "apt-get update") {
		t.Errorf("Args[0] = %q, want apt-get update prefix", s.Args[0])
	}
}

func TestPipInstall(t *testing.T) {
	b := NewTemplate().PipInstall("requests", "numpy")
	if len(b.steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(b.steps))
	}
	s := b.steps[0]
	if s.Type != "run" {
		t.Errorf("Type = %q, want %q", s.Type, "run")
	}
	if s.Args[0] != "pip install requests numpy" {
		t.Errorf("Args[0] = %q, want %q", s.Args[0], "pip install requests numpy")
	}
}

func TestNpmInstall(t *testing.T) {
	b := NewTemplate().NpmInstall("express", "nodemon")
	if len(b.steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(b.steps))
	}
	s := b.steps[0]
	if s.Type != "run" {
		t.Errorf("Type = %q, want %q", s.Type, "run")
	}
	if s.Args[0] != "npm install -g express nodemon" {
		t.Errorf("Args[0] = %q, want %q", s.Args[0], "npm install -g express nodemon")
	}
}

func TestSkipCachePerStep(t *testing.T) {
	b := NewTemplate().
		FromImage("python:3.11").
		RunCmd("echo before").     // no force
		SkipCache().               // all subsequent steps get force
		RunCmd("echo after").      // force
		SetWorkdir("/app")         // force

	if len(b.steps) != 3 {
		t.Fatalf("len(steps) = %d, want 3", len(b.steps))
	}
	if b.steps[0].Force {
		t.Error("steps[0].Force = true, want false (before SkipCache)")
	}
	if !b.steps[1].Force {
		t.Error("steps[1].Force = false, want true (after SkipCache)")
	}
	if !b.steps[2].Force {
		t.Error("steps[2].Force = false, want true (after SkipCache)")
	}
	if b.force {
		t.Error("template-wide force = true, want false (SkipCache was after FromImage)")
	}
}

func TestSkipCacheEscalation(t *testing.T) {
	b := NewTemplate().
		SkipCache().                // before FromImage → escalates
		FromImage("python:3.12").
		PipInstall("django")

	if !b.force {
		t.Error("template-wide force = false, want true (SkipCache before FromImage should escalate)")
	}
	// After escalation, forceNextLayer should be reset.
	if b.forceNextLayer {
		t.Error("forceNextLayer = true, want false after escalation")
	}
	// Steps added after escalation should NOT have per-step force
	// (template-wide force handles it).
	if b.steps[0].Force {
		t.Error("steps[0].Force = true, want false (template-wide force covers it)")
	}
}

func TestSkipCacheEscalationFromTemplate(t *testing.T) {
	b := NewTemplate().
		SkipCache().
		FromTemplate("tmpl-abc")

	if !b.force {
		t.Error("template-wide force = false, want true (SkipCache before FromTemplate should escalate)")
	}
}

func TestSkipCacheEscalationFromBaseImage(t *testing.T) {
	b := NewTemplate().
		SkipCache().
		FromBaseImage()

	if !b.force {
		t.Error("template-wide force = false, want true (SkipCache before FromBaseImage should escalate)")
	}
}

func TestSetStartCmd(t *testing.T) {
	b := NewTemplate().SetStartCmd("python server.py")
	if b.startCmd != "python server.py" {
		t.Errorf("startCmd = %q, want %q", b.startCmd, "python server.py")
	}
}

func TestSetReadyCmd(t *testing.T) {
	b := NewTemplate().SetReadyCmd("curl localhost:8080")
	if b.readyCmd != "curl localhost:8080" {
		t.Errorf("readyCmd = %q, want %q", b.readyCmd, "curl localhost:8080")
	}
}

func TestMethodChaining(t *testing.T) {
	b := NewTemplate()
	result := b.
		FromImage("python:3.11").
		AptInstall("curl", "git").
		PipInstall("requests").
		SetEnvs(map[string]string{"PORT": "8080"}).
		SetWorkdir("/app").
		SetUser("user").
		RunCmd("echo setup").
		SetStartCmd("python app.py").
		SetReadyCmd(WaitForPort(8080))

	// All methods should return the same pointer.
	if result != b {
		t.Error("method chaining returned different pointer")
	}

	if b.fromImage != "python:3.11" {
		t.Errorf("fromImage = %q", b.fromImage)
	}
	// apt + pip + env + workdir + user + run = 6 steps
	if len(b.steps) != 6 {
		t.Errorf("len(steps) = %d, want 6", len(b.steps))
	}
	if b.startCmd != "python app.py" {
		t.Errorf("startCmd = %q", b.startCmd)
	}
	if b.readyCmd == "" {
		t.Error("readyCmd is empty")
	}
}

// ---------------------------------------------------------------------------
// WaitFor* helpers
// ---------------------------------------------------------------------------

func TestWaitForPort(t *testing.T) {
	cmd := WaitForPort(8080)
	if !strings.Contains(cmd, ":8080") {
		t.Errorf("WaitForPort(8080) = %q, want port number", cmd)
	}
}

func TestWaitForURL(t *testing.T) {
	cmd := WaitForURL("http://localhost:3000/health", 200)
	if !strings.Contains(cmd, "localhost:3000/health") {
		t.Errorf("WaitForURL = %q, want URL", cmd)
	}
	if !strings.Contains(cmd, "200") {
		t.Errorf("WaitForURL = %q, want status code", cmd)
	}
}

func TestWaitForProcess(t *testing.T) {
	cmd := WaitForProcess("nginx")
	if !strings.Contains(cmd, "nginx") {
		t.Errorf("WaitForProcess = %q, want process name", cmd)
	}
	if !strings.Contains(cmd, "pgrep") {
		t.Errorf("WaitForProcess = %q, want pgrep command", cmd)
	}
}

func TestWaitForFile(t *testing.T) {
	cmd := WaitForFile("/tmp/ready")
	if !strings.Contains(cmd, "/tmp/ready") {
		t.Errorf("WaitForFile = %q, want file path", cmd)
	}
}

func TestWaitForTimeout(t *testing.T) {
	cmd := WaitForTimeout(5000)
	if !strings.Contains(cmd, "sleep") {
		t.Errorf("WaitForTimeout = %q, want sleep command", cmd)
	}
	if !strings.Contains(cmd, "5.0") {
		t.Errorf("WaitForTimeout(5000) = %q, want 5.0 seconds", cmd)
	}
}

func TestMultipleCopySteps(t *testing.T) {
	b := NewTemplate().
		Copy("src/", "/app/src/").
		Copy("package.json", "/app/package.json")

	if len(b.steps) != 2 {
		t.Fatalf("len(steps) = %d, want 2", len(b.steps))
	}
	if len(b.fileBundles) != 2 {
		t.Fatalf("len(fileBundles) = %d, want 2", len(b.fileBundles))
	}
	if b.fileBundles[0].step != 0 {
		t.Errorf("fileBundles[0].step = %d, want 0", b.fileBundles[0].step)
	}
	if b.fileBundles[1].step != 1 {
		t.Errorf("fileBundles[1].step = %d, want 1", b.fileBundles[1].step)
	}
}

// ---------------------------------------------------------------------------
// Build() / BuildInBackground() tests
// ---------------------------------------------------------------------------

// buildMockServer creates a mock HTTP server that simulates the full build workflow.
// It tracks which endpoints were called via the returned callLog.
func buildMockServer(t *testing.T, opts buildMockOpts) (*httptest.Server, *buildCallLog) {
	t.Helper()
	log := &buildCallLog{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Phase 1: POST /v3/templates → create template
		if r.Method == http.MethodPost && path == "/v3/templates" {
			log.mu.Lock()
			log.createCalled = true
			log.mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(TemplateInfo{
				TemplateID: "tmpl-mock",
				BuildID:    "build-mock",
				Names:      []string{"test-template"},
				Public:     false,
			})
			return
		}

		// Phase 2a: GET /templates/{id}/files/{hash} → check files
		if r.Method == http.MethodGet && strings.HasPrefix(path, "/templates/tmpl-mock/files/") {
			log.mu.Lock()
			log.checkFilesCalled = true
			log.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			present := opts.filesCached
			resp := BuildFileStatus{Present: present}
			if !present {
				resp.URL = fmt.Sprintf("http://%s/upload", r.Host)
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Phase 2b: PUT /upload → upload files
		if r.Method == http.MethodPut && path == "/upload" {
			log.mu.Lock()
			log.uploadCalled = true
			log.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		// Phase 3: POST /v2/templates/{id}/builds/{buildID} → start build
		if r.Method == http.MethodPost && path == "/v2/templates/tmpl-mock/builds/build-mock" {
			log.mu.Lock()
			log.startCalled = true
			// Capture the StartBuildConfig for assertions.
			var cfg StartBuildConfig
			if err := json.NewDecoder(r.Body).Decode(&cfg); err == nil {
				log.startConfig = cfg
			}
			log.mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// Phase 4: GET /templates/{id}/builds/{buildID}/status → poll
		if r.Method == http.MethodGet && strings.HasPrefix(path, "/templates/tmpl-mock/builds/build-mock/status") {
			count := log.pollCount.Add(1)
			status := "building"
			var reason *BuildStatusReason
			var logEntries []BuildLogEntry

			if opts.buildError && count >= int64(opts.readyAfterPolls) {
				status = "error"
				reason = &BuildStatusReason{Message: "command failed", Step: "run"}
			} else if count >= int64(opts.readyAfterPolls) {
				status = "ready"
			}

			// Return some log entries on each poll (except drain polls).
			if count <= int64(opts.readyAfterPolls) {
				logEntries = []BuildLogEntry{
					{Timestamp: fmt.Sprintf("2026-01-01T00:00:%02dZ", count), Message: fmt.Sprintf("log-%d", count), Level: "info"},
				}
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(BuildStatus{
				TemplateID: "tmpl-mock",
				BuildID:    "build-mock",
				Status:     status,
				LogEntries: logEntries,
				Reason:     reason,
			})
			return
		}

		http.NotFound(w, r)
	}))

	return srv, log
}

type buildMockOpts struct {
	filesCached    bool
	readyAfterPolls int // poll count at which to return ready/error
	buildError     bool
}

type buildCallLog struct {
	mu              sync.Mutex
	createCalled    bool
	checkFilesCalled bool
	uploadCalled    bool
	startCalled     bool
	startConfig     StartBuildConfig
	pollCount       atomic.Int64
}

func TestBuildFullLifecycle(t *testing.T) {
	srv, log := buildMockServer(t, buildMockOpts{readyAfterPolls: 2})
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})

	b := NewTemplate().
		FromImage("python:3.11").
		RunCmd("echo hello").
		SetStartCmd("python app.py")

	result, err := b.Build(context.Background(), client, BuildConfig{Name: "test-template"})
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if result.TemplateID != "tmpl-mock" {
		t.Errorf("TemplateID = %q, want %q", result.TemplateID, "tmpl-mock")
	}
	if result.BuildID != "build-mock" {
		t.Errorf("BuildID = %q, want %q", result.BuildID, "build-mock")
	}

	log.mu.Lock()
	defer log.mu.Unlock()
	if !log.createCalled {
		t.Error("CreateTemplate was not called")
	}
	if !log.startCalled {
		t.Error("StartTemplateBuild was not called")
	}
	if log.startConfig.FromImage != "python:3.11" {
		t.Errorf("StartConfig.FromImage = %q, want %q", log.startConfig.FromImage, "python:3.11")
	}
}

func TestBuildSkipCacheSetsForce(t *testing.T) {
	srv, log := buildMockServer(t, buildMockOpts{readyAfterPolls: 1})
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})

	b := NewTemplate().FromImage("python:3.11").RunCmd("echo hi")
	_, err := b.Build(context.Background(), client, BuildConfig{
		Name:      "test",
		SkipCache: true,
	})
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	log.mu.Lock()
	defer log.mu.Unlock()
	if !log.startConfig.Force {
		t.Error("StartConfig.Force = false, want true when SkipCache is set")
	}
}

func TestBuildFileUpload(t *testing.T) {
	srv, log := buildMockServer(t, buildMockOpts{filesCached: false, readyAfterPolls: 1})
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})

	// Create a temp file so Copy has something to hash.
	b := NewTemplate().
		FromImage("python:3.11").
		Copy("template_builder.go", "/app/template_builder.go")

	_, err := b.Build(context.Background(), client, BuildConfig{Name: "test"})
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	log.mu.Lock()
	defer log.mu.Unlock()
	if !log.checkFilesCalled {
		t.Error("CheckBuildFiles was not called")
	}
	if !log.uploadCalled {
		t.Error("UploadBuildFiles was not called (files not cached)")
	}
	// Verify FilesHash was set on the copy step (index 0, since FromImage doesn't add a step).
	if b.steps[0].FilesHash == "" {
		t.Error("copy step FilesHash is empty, should be populated")
	}
}

func TestBuildFileCached(t *testing.T) {
	srv, log := buildMockServer(t, buildMockOpts{filesCached: true, readyAfterPolls: 1})
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})

	b := NewTemplate().
		FromImage("python:3.11").
		Copy("template_builder.go", "/app/template_builder.go")

	_, err := b.Build(context.Background(), client, BuildConfig{Name: "test"})
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	log.mu.Lock()
	defer log.mu.Unlock()
	if !log.checkFilesCalled {
		t.Error("CheckBuildFiles was not called")
	}
	if log.uploadCalled {
		t.Error("UploadBuildFiles was called but files should be cached")
	}
}

func TestBuildStatusError(t *testing.T) {
	srv, _ := buildMockServer(t, buildMockOpts{readyAfterPolls: 1, buildError: true})
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})

	b := NewTemplate().FromImage("python:3.11").RunCmd("exit 1")
	_, err := b.Build(context.Background(), client, BuildConfig{Name: "test"})
	if err == nil {
		t.Fatal("Build() expected error, got nil")
	}

	var buildErr *TemplateBuildError
	ok := false
	if e, is := err.(*TemplateBuildError); is {
		buildErr = e
		ok = true
	}
	if !ok {
		t.Fatalf("error type = %T, want *TemplateBuildError", err)
	}
	if buildErr.TemplateID != "tmpl-mock" {
		t.Errorf("TemplateID = %q, want %q", buildErr.TemplateID, "tmpl-mock")
	}
	if buildErr.Reason.Message != "command failed" {
		t.Errorf("Reason.Message = %q, want %q", buildErr.Reason.Message, "command failed")
	}
	if buildErr.Reason.Step != "run" {
		t.Errorf("Reason.Step = %q, want %q", buildErr.Reason.Step, "run")
	}
}

func TestBuildContextCanceled(t *testing.T) {
	// Server that always returns "building" so we rely on context cancel.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if r.Method == http.MethodPost && path == "/v3/templates" {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(TemplateInfo{
				TemplateID: "tmpl-mock", BuildID: "build-mock",
				Names: []string{"test"}, Public: false,
			})
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(path, "/v2/templates/") {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if r.Method == http.MethodGet && strings.Contains(path, "/status") {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(BuildStatus{
				TemplateID: "tmpl-mock", BuildID: "build-mock",
				Status: "building",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the poll loop exits.
	cancel()

	b := NewTemplate().FromImage("python:3.11").RunCmd("echo hi")
	_, err := b.Build(ctx, client, BuildConfig{Name: "test"})
	if err == nil {
		t.Fatal("Build() expected error from canceled context, got nil")
	}
}

func TestBuildOnLogCallback(t *testing.T) {
	srv, _ := buildMockServer(t, buildMockOpts{readyAfterPolls: 3})
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})

	var mu sync.Mutex
	var received []string

	b := NewTemplate().FromImage("python:3.11").RunCmd("echo hi")
	_, err := b.Build(context.Background(), client, BuildConfig{
		Name: "test",
		OnLog: func(entry BuildLogEntry) {
			mu.Lock()
			received = append(received, entry.Message)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// The mock returns 1 log entry per poll for readyAfterPolls=3, so we get 3 entries.
	if len(received) < 3 {
		t.Errorf("OnLog called %d times, want at least 3", len(received))
	}
}

func TestBuildInBackground(t *testing.T) {
	srv, log := buildMockServer(t, buildMockOpts{readyAfterPolls: 5})
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})

	b := NewTemplate().FromImage("python:3.11").RunCmd("echo hi")
	result, err := b.BuildInBackground(context.Background(), client, BuildConfig{Name: "test"})
	if err != nil {
		t.Fatalf("BuildInBackground() error: %v", err)
	}

	if result.TemplateID != "tmpl-mock" {
		t.Errorf("TemplateID = %q, want %q", result.TemplateID, "tmpl-mock")
	}

	log.mu.Lock()
	defer log.mu.Unlock()
	if !log.createCalled {
		t.Error("CreateTemplate was not called")
	}
	if !log.startCalled {
		t.Error("StartTemplateBuild was not called")
	}
	// No polling should have happened.
	if count := log.pollCount.Load(); count != 0 {
		t.Errorf("pollCount = %d, want 0 (BuildInBackground should not poll)", count)
	}
}

func TestBuildValidationEmptyName(t *testing.T) {
	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: "http://localhost"})
	b := NewTemplate().FromImage("python:3.11")

	_, err := b.Build(context.Background(), client, BuildConfig{})
	if err == nil {
		t.Fatal("Build() expected error for empty name, got nil")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("error = %q, want name-related message", err.Error())
	}
}

func TestBuildValidationEmptyNameBackground(t *testing.T) {
	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: "http://localhost"})
	b := NewTemplate().FromImage("python:3.11")

	_, err := b.BuildInBackground(context.Background(), client, BuildConfig{})
	if err == nil {
		t.Fatal("BuildInBackground() expected error for empty name, got nil")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("error = %q, want name-related message", err.Error())
	}
}
