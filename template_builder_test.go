package e2b

import (
	"strings"
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
