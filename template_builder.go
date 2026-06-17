package e2b

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
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
	hash string // SHA-256 hex hash (populated during Build)
	data []byte // gzipped tar archive (populated during Build)
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

// BuildConfig holds configuration for Build() and BuildInBackground().
type BuildConfig struct {
	// Name is the template name (required).
	Name string

	// Tags are labels to assign to the template build.
	Tags []string

	// CPUCount is the number of CPU cores for sandbox instances.
	CPUCount int

	// MemoryMB is the memory in MiB for sandbox instances.
	MemoryMB int

	// SkipCache forces a full rebuild, ignoring all caches.
	SkipCache bool

	// OnLog is called for each log entry received during polling.
	// Only used by Build(), not BuildInBackground().
	OnLog func(entry BuildLogEntry)
}

// BuildResult holds the result of a successful template build.
type BuildResult struct {
	TemplateID string
	BuildID    string
	Names      []string
	Public     bool
}

// Build executes the full template build lifecycle synchronously:
// 1. Create template (allocate build ID)
// 2. Compute file hashes and upload any uncached file bundles
// 3. Start the build
// 4. Poll until "ready" or "error", streaming logs via OnLog
func (b *TemplateBuilder) Build(ctx context.Context, client *Client, cfg BuildConfig) (*BuildResult, error) {
	if err := b.validate(cfg); err != nil {
		return nil, err
	}

	// Phase 1: Create template.
	info, err := client.CreateTemplate(ctx, CreateTemplateConfig{
		Name:     cfg.Name,
		Tags:     cfg.Tags,
		CPUCount: cfg.CPUCount,
		MemoryMB: cfg.MemoryMB,
	})
	if err != nil {
		return nil, fmt.Errorf("e2b: build create template: %w", err)
	}

	// Phase 2: Compute file hashes and upload.
	if err := b.processFileBundles(ctx, client, info.TemplateID); err != nil {
		return nil, err
	}

	// Phase 3: Start the build.
	if err := b.startBuild(ctx, client, info.TemplateID, info.BuildID, cfg); err != nil {
		return nil, err
	}

	// Phase 4: Poll until terminal status.
	if err := b.pollBuild(ctx, client, info.TemplateID, info.BuildID, cfg.OnLog); err != nil {
		return nil, err
	}

	return &BuildResult{
		TemplateID: info.TemplateID,
		BuildID:    info.BuildID,
		Names:      info.Names,
		Public:     info.Public,
	}, nil
}

// BuildInBackground executes phases 1-3 (create, upload, start) and returns
// immediately without polling for completion.
func (b *TemplateBuilder) BuildInBackground(ctx context.Context, client *Client, cfg BuildConfig) (*BuildResult, error) {
	if err := b.validate(cfg); err != nil {
		return nil, err
	}

	// Phase 1: Create template.
	info, err := client.CreateTemplate(ctx, CreateTemplateConfig{
		Name:     cfg.Name,
		Tags:     cfg.Tags,
		CPUCount: cfg.CPUCount,
		MemoryMB: cfg.MemoryMB,
	})
	if err != nil {
		return nil, fmt.Errorf("e2b: build create template: %w", err)
	}

	// Phase 2: Compute file hashes and upload.
	if err := b.processFileBundles(ctx, client, info.TemplateID); err != nil {
		return nil, err
	}

	// Phase 3: Start the build.
	if err := b.startBuild(ctx, client, info.TemplateID, info.BuildID, cfg); err != nil {
		return nil, err
	}

	return &BuildResult{
		TemplateID: info.TemplateID,
		BuildID:    info.BuildID,
		Names:      info.Names,
		Public:     info.Public,
	}, nil
}

// validate checks that the builder configuration is valid.
func (b *TemplateBuilder) validate(cfg BuildConfig) error {
	if cfg.Name == "" {
		return &Error{Message: "template name is required in BuildConfig"}
	}
	return nil
}

// processFileBundles computes file hashes and uploads any uncached bundles.
func (b *TemplateBuilder) processFileBundles(ctx context.Context, client *Client, templateID string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("e2b: get working directory: %w", err)
	}

	for i := range b.fileBundles {
		fb := &b.fileBundles[i]
		src := b.steps[fb.step].Args[0] // copy step's source path

		hash, data, err := computeFilesHash(cwd, src)
		if err != nil {
			return fmt.Errorf("e2b: compute files hash for %s: %w", src, err)
		}
		fb.hash = hash
		fb.data = data

		// Set the hash on the corresponding step.
		b.steps[fb.step].FilesHash = hash

		// Check if already cached.
		status, err := client.CheckBuildFiles(ctx, templateID, hash)
		if err != nil {
			return fmt.Errorf("e2b: check build files: %w", err)
		}

		if !status.Present {
			if err := client.UploadBuildFiles(ctx, status.URL, bytes.NewReader(data)); err != nil {
				return fmt.Errorf("e2b: upload build files: %w", err)
			}
		}
	}
	return nil
}

// startBuild triggers the template build with the configured steps.
func (b *TemplateBuilder) startBuild(ctx context.Context, client *Client, templateID, buildID string, cfg BuildConfig) error {
	force := b.force || cfg.SkipCache
	return client.StartTemplateBuild(ctx, templateID, buildID, StartBuildConfig{
		FromImage:    b.fromImage,
		FromTemplate: b.fromTemplate,
		Force:        force,
		Steps:        b.steps,
		StartCmd:     b.startCmd,
		ReadyCmd:     b.readyCmd,
	})
}

// pollBuild polls GetBuildStatus until the build reaches a terminal state.
// Logs are streamed incrementally via the onLog callback.
func (b *TemplateBuilder) pollBuild(ctx context.Context, client *Client, templateID, buildID string, onLog func(BuildLogEntry)) error {
	logsOffset := 0
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("e2b: build poll canceled: %w", err)
		}

		status, err := client.GetBuildStatus(ctx, templateID, buildID,
			WithBuildStatusLogsOffset(logsOffset),
		)
		if err != nil {
			return fmt.Errorf("e2b: get build status: %w", err)
		}

		// Deliver log entries.
		if onLog != nil {
			for _, entry := range status.LogEntries {
				onLog(entry)
			}
		}
		logsOffset += len(status.LogEntries)

		switch status.Status {
		case "ready":
			// Drain remaining logs.
			if err := b.drainLogs(ctx, client, templateID, buildID, logsOffset, onLog); err != nil {
				return err
			}
			return nil
		case "error":
			// Drain remaining logs before returning error.
			_ = b.drainLogs(ctx, client, templateID, buildID, logsOffset, onLog)
			reason := BuildStatusReason{}
			if status.Reason != nil {
				reason = *status.Reason
			}
			return &TemplateBuildError{
				TemplateID: templateID,
				BuildID:    buildID,
				Reason:     reason,
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// drainLogs fetches any remaining log entries after a terminal status.
func (b *TemplateBuilder) drainLogs(ctx context.Context, client *Client, templateID, buildID string, offset int, onLog func(BuildLogEntry)) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("e2b: drain logs canceled: %w", err)
		}

		status, err := client.GetBuildStatus(ctx, templateID, buildID,
			WithBuildStatusLogsOffset(offset),
		)
		if err != nil {
			return fmt.Errorf("e2b: drain logs: %w", err)
		}

		if len(status.LogEntries) == 0 {
			return nil
		}

		if onLog != nil {
			for _, entry := range status.LogEntries {
				onLog(entry)
			}
		}
		offset += len(status.LogEntries)
	}
}
