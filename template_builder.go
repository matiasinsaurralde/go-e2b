package e2b

import (
	"fmt"
	"sort"
	"strings"
)

// TemplateBuilder accumulates build steps for a template using a fluent API.
// Use NewTemplate() to create a builder, chain step methods, then call Build()
// or BuildInBackground() to execute.
type TemplateBuilder struct {
	fromImage      string
	fromTemplate   string
	force          bool // template-wide force (skip all caches)
	forceNextLayer bool // per-step force for subsequent steps
	steps          []TemplateStep
	startCmd       string
	readyCmd       string
	fileBundles    []fileBundle
}

// fileBundle tracks a copy step that needs file hashing and upload during Build().
// The hash and data fields are populated by computeFilesHash during Build().
type fileBundle struct {
	step int
	hash string //nolint:unused // populated by computeFilesHash, read in Build()
	data []byte //nolint:unused // populated by computeFilesHash, read in Build()
}

// NewTemplate creates a new TemplateBuilder.
func NewTemplate() *TemplateBuilder {
	return &TemplateBuilder{}
}

// FromImage sets the base Docker image (e.g. "python:3.11").
// Mutually exclusive with FromTemplate.
func (b *TemplateBuilder) FromImage(image string) *TemplateBuilder {
	b.fromImage = image
	b.fromTemplate = ""
	if b.forceNextLayer {
		b.force = true
		b.forceNextLayer = false
	}
	return b
}

// FromTemplate sets the base E2B template ID to extend.
// Mutually exclusive with FromImage.
func (b *TemplateBuilder) FromTemplate(templateID string) *TemplateBuilder {
	b.fromTemplate = templateID
	b.fromImage = ""
	if b.forceNextLayer {
		b.force = true
		b.forceNextLayer = false
	}
	return b
}

// FromBaseImage uses the default E2B base image (Ubuntu with envd pre-installed).
func (b *TemplateBuilder) FromBaseImage() *TemplateBuilder {
	b.fromImage = ""
	b.fromTemplate = ""
	if b.forceNextLayer {
		b.force = true
		b.forceNextLayer = false
	}
	return b
}

// RunCmd appends a "run" step that executes a shell command during the build.
func (b *TemplateBuilder) RunCmd(cmd string) *TemplateBuilder {
	b.steps = append(b.steps, TemplateStep{
		Type:  "run",
		Args:  []string{cmd},
		Force: b.forceNextLayer,
	})
	return b
}

// Copy appends a "copy" step that copies a local file or directory into the template.
// The src path is resolved relative to the working directory at Build() time.
// File hashing and tar creation happen during Build() (Step 6).
func (b *TemplateBuilder) Copy(src, dest string) *TemplateBuilder {
	stepIdx := len(b.steps)
	b.steps = append(b.steps, TemplateStep{
		Type:  "copy",
		Args:  []string{src, dest},
		Force: b.forceNextLayer,
		// FilesHash is populated during Build() after computing the tar hash.
	})
	b.fileBundles = append(b.fileBundles, fileBundle{
		step: stepIdx,
	})
	return b
}

// SetEnvs appends an "env" step with sorted KEY=VALUE pairs.
// Keys are sorted alphabetically for deterministic builds.
func (b *TemplateBuilder) SetEnvs(envs map[string]string) *TemplateBuilder {
	keys := make([]string, 0, len(envs))
	for k := range envs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	args := make([]string, 0, len(envs))
	for _, k := range keys {
		args = append(args, k+"="+envs[k])
	}

	b.steps = append(b.steps, TemplateStep{
		Type:  "env",
		Args:  args,
		Force: b.forceNextLayer,
	})
	return b
}

// SetWorkdir appends a "workdir" step that sets the working directory for subsequent steps.
func (b *TemplateBuilder) SetWorkdir(dir string) *TemplateBuilder {
	b.steps = append(b.steps, TemplateStep{
		Type:  "workdir",
		Args:  []string{dir},
		Force: b.forceNextLayer,
	})
	return b
}

// SetUser appends a "user" step that sets the user for subsequent steps.
func (b *TemplateBuilder) SetUser(user string) *TemplateBuilder {
	b.steps = append(b.steps, TemplateStep{
		Type:  "user",
		Args:  []string{user},
		Force: b.forceNextLayer,
	})
	return b
}

// AptInstall is a convenience method that runs "apt-get update && apt-get install -y ...".
func (b *TemplateBuilder) AptInstall(packages ...string) *TemplateBuilder {
	cmd := "apt-get update && apt-get install -y " + strings.Join(packages, " ")
	return b.RunCmd(cmd)
}

// PipInstall is a convenience method that runs "pip install ...".
func (b *TemplateBuilder) PipInstall(packages ...string) *TemplateBuilder {
	cmd := "pip install " + strings.Join(packages, " ")
	return b.RunCmd(cmd)
}

// NpmInstall is a convenience method that runs "npm install -g ...".
func (b *TemplateBuilder) NpmInstall(packages ...string) *TemplateBuilder {
	cmd := "npm install -g " + strings.Join(packages, " ")
	return b.RunCmd(cmd)
}

// SkipCache marks all subsequent steps as force (skip cache).
// If called before FromImage/FromTemplate/FromBaseImage, those methods
// escalate to template-wide force.
func (b *TemplateBuilder) SkipCache() *TemplateBuilder {
	b.forceNextLayer = true
	return b
}

// SetStartCmd sets the command to run when a sandbox starts from this template.
func (b *TemplateBuilder) SetStartCmd(cmd string) *TemplateBuilder {
	b.startCmd = cmd
	return b
}

// SetReadyCmd sets a health-check command that must exit 0 to indicate readiness.
// Use the WaitFor* helpers to generate common ready-check commands.
func (b *TemplateBuilder) SetReadyCmd(cmd string) *TemplateBuilder {
	b.readyCmd = cmd
	return b
}

// WaitForPort returns a ready-check command that waits for a TCP port to be listening.
func WaitForPort(port int) string {
	return fmt.Sprintf("while ! ss -tln | grep -q ':%d '; do sleep 0.1; done", port)
}

// WaitForURL returns a ready-check command that polls an HTTP URL until it returns the expected status.
func WaitForURL(url string, statusCode int) string {
	return fmt.Sprintf("while [ \"$(curl -s -o /dev/null -w '%%{http_code}' '%s')\" != \"%d\" ]; do sleep 0.1; done", url, statusCode)
}

// WaitForProcess returns a ready-check command that waits for a process to be running.
func WaitForProcess(name string) string {
	return fmt.Sprintf("while ! pgrep -x '%s' > /dev/null; do sleep 0.1; done", name)
}

// WaitForFile returns a ready-check command that waits for a file to exist.
func WaitForFile(path string) string {
	return fmt.Sprintf("while [ ! -f '%s' ]; do sleep 0.1; done", path)
}

// WaitForTimeout returns a ready-check command that simply sleeps for the given duration.
func WaitForTimeout(ms int) string {
	return fmt.Sprintf("sleep %s", fmt.Sprintf("%.1f", float64(ms)/1000.0))
}
