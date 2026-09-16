package runtimectl

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/JC-SYSU/pubmed-surfing/internal/pubmed"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Manifest struct {
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	ReleaseID  string            `json:"release_id"`
	GitCommit  string            `json:"git_commit"`
	GitDirty   bool              `json:"git_dirty"`
	GOOS       string            `json:"goos"`
	GOARCH     string            `json:"goarch"`
	BuiltAt    string            `json:"built_at"`
	Entrypoint string            `json:"entrypoint"`
	Control    string            `json:"control"`
	Payload    map[string]string `json:"payload_sha256"`
}
type platform struct{ os, arch, ext, archive string }

var platforms = []platform{{"darwin", "arm64", "", "tar.gz"}, {"windows", "amd64", ".exe", "zip"}}

func projectRoot() (string, error) {
	cwd, e := os.Getwd()
	if e != nil {
		return "", e
	}
	for p := cwd; ; p = filepath.Dir(p) {
		if _, e := os.Stat(filepath.Join(p, "go.mod")); e == nil {
			return p, nil
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return "", errors.New("run from the go module")
}
func run(dir string, env []string, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	b, e := c.CombinedOutput()
	if e != nil {
		return string(b), fmt.Errorf("%s %v: %w: %s", name, args, e, b)
	}
	return strings.TrimSpace(string(b)), nil
}
func digest(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}

func Release(allowDirty bool) ([]string, error) {
	root, e := projectRoot()
	if e != nil {
		return nil, e
	}
	repo, e := run(root, nil, "git", "rev-parse", "--show-toplevel")
	if e != nil {
		return nil, errors.New("release requires a git repository; clone the source first")
	}
	status, e := run(repo, nil, "git", "status", "--porcelain")
	if e != nil {
		return nil, e
	}
	if status != "" && !allowDirty {
		return nil, errors.New("refusing to build a release from a dirty worktree; pass --allow-dirty for a marked development artifact")
	}
	commit, e := run(repo, nil, "git", "rev-parse", "--short=12", "HEAD")
	if e != nil {
		return nil, e
	}
	id := pubmed.Version
	if status != "" {
		id = fmt.Sprintf("%s-%s-dirty-%s", pubmed.Version, commit, time.Now().UTC().Format("20060102T150405Z"))
	}
	artifacts := filepath.Join(root, "artifacts")
	if e = os.MkdirAll(artifacts, 0755); e != nil {
		return nil, e
	}
	out := []string{}
	for _, p := range platforms {
		stage, e := os.MkdirTemp("", "pubmed-surfing-go-release-")
		if e != nil {
			return nil, e
		}
		defer os.RemoveAll(stage)
		server := "pubmed-surfing" + p.ext
		ctl := "pubmed-surfingctl" + p.ext
		for _, b := range []struct{ name, pkg string }{{server, "./cmd/pubmed-surfing"}, {ctl, "./cmd/pubmed-surfingctl"}} {
			if _, e = run(root, []string{"GOOS=" + p.os, "GOARCH=" + p.arch, "CGO_ENABLED=0"}, "go", "build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(stage, b.name), b.pkg); e != nil {
				return nil, e
			}
		}
		payload := map[string]string{}
		for _, name := range []string{server, ctl} {
			payload[name], e = digest(filepath.Join(stage, name))
			if e != nil {
				return nil, e
			}
		}
		m := Manifest{"pubmed-surfing-go", pubmed.Version, id, commit, status != "", p.os, p.arch, time.Now().UTC().Format(time.RFC3339), server, ctl, payload}
		if e = writeJSON(filepath.Join(stage, "RELEASE.json"), m); e != nil {
			return nil, e
		}
		base := fmt.Sprintf("pubmed-surfing-go-%s-%s-%s", id, p.os, p.arch)
		artifact := filepath.Join(artifacts, base+"."+p.archive)
		if _, e = os.Stat(artifact); e == nil {
			return nil, fmt.Errorf("release artifact already exists: %s", artifact)
		}
		if p.archive == "zip" {
			e = zipDir(stage, artifact)
		} else {
			e = tarDir(stage, artifact)
		}
		if e != nil {
			return nil, e
		}
		sum, e := digest(artifact)
		if e != nil {
			return nil, e
		}
		if e = os.WriteFile(artifact+".sha256", []byte(sum+"  "+filepath.Base(artifact)+"\n"), 0644); e != nil {
			return nil, e
		}
		out = append(out, artifact)
	}
	return out, nil
}
func files(dir string) ([]string, error) {
	out := []string{}
	e := filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			r, _ := filepath.Rel(dir, path)
			out = append(out, filepath.ToSlash(r))
		}
		return nil
	})
	sort.Strings(out)
	return out, e
}
func tarDir(dir, dst string) error {
	f, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	names, e := files(dir)
	if e != nil {
		return e
	}
	for _, name := range names {
		path := filepath.Join(dir, filepath.FromSlash(name))
		info, e := os.Stat(path)
		if e != nil {
			return e
		}
		h, e := tar.FileInfoHeader(info, "")
		if e != nil {
			return e
		}
		h.Name = name
		h.ModTime = time.Unix(0, 0)
		if e = tw.WriteHeader(h); e != nil {
			return e
		}
		in, e := os.Open(path)
		if e != nil {
			return e
		}
		_, copyErr := io.Copy(tw, in)
		in.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}
func zipDir(dir, dst string) error {
	f, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	names, e := files(dir)
	if e != nil {
		return e
	}
	for _, name := range names {
		path := filepath.Join(dir, filepath.FromSlash(name))
		info, e := os.Stat(path)
		if e != nil {
			return e
		}
		h, e := zip.FileInfoHeader(info)
		if e != nil {
			return e
		}
		h.Name = name
		h.Method = zip.Deflate
		w, e := zw.CreateHeader(h)
		if e != nil {
			return e
		}
		in, e := os.Open(path)
		if e != nil {
			return e
		}
		_, copyErr := io.Copy(w, in)
		in.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func DefaultHome() string {
	if v := os.Getenv("PUBMED_SURFING_GO_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "pubmed-surfing-go")
		}
	}
	return filepath.Join(home, ".local", "share", "pubmed-surfing-go")
}
func safeName(name string) bool {
	if name == "" || filepath.IsAbs(name) {
		return false
	}
	clean := filepath.Clean(name)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && !strings.Contains(name, "\\")
}
func extract(artifact, dst string) error {
	if strings.HasSuffix(artifact, ".zip") {
		z, e := zip.OpenReader(artifact)
		if e != nil {
			return e
		}
		defer z.Close()
		for _, f := range z.File {
			if !safeName(f.Name) || f.FileInfo().Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe archive entry: %s", f.Name)
			}
			path := filepath.Join(dst, filepath.FromSlash(f.Name))
			if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
				return e
			}
			in, e := f.Open()
			if e != nil {
				return e
			}
			out, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, f.Mode())
			if e != nil {
				in.Close()
				return e
			}
			_, ce := io.Copy(out, in)
			out.Close()
			in.Close()
			if ce != nil {
				return ce
			}
		}
		return nil
	}
	f, e := os.Open(artifact)
	if e != nil {
		return e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return e
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if !safeName(h.Name) || h.Typeflag != tar.TypeReg {
			return fmt.Errorf("unsafe archive entry: %s", h.Name)
		}
		path := filepath.Join(dst, filepath.FromSlash(h.Name))
		if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			return e
		}
		out, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(h.Mode))
		if e != nil {
			return e
		}
		_, ce := io.Copy(out, tr)
		out.Close()
		if ce != nil {
			return ce
		}
	}
	return nil
}
func loadManifest(dir string) (Manifest, error) {
	var m Manifest
	b, e := os.ReadFile(filepath.Join(dir, "RELEASE.json"))
	if e != nil {
		return m, e
	}
	e = json.Unmarshal(b, &m)
	return m, e
}
func verifyPayload(dir string, m Manifest) error {
	names, e := files(dir)
	if e != nil {
		return e
	}
	actual := []string{}
	for _, n := range names {
		if n != "RELEASE.json" {
			actual = append(actual, n)
		}
	}
	expected := make([]string, 0, len(m.Payload))
	for n := range m.Payload {
		expected = append(expected, n)
	}
	sort.Strings(expected)
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		return errors.New("release payload file list does not match RELEASE.json")
	}
	for _, n := range expected {
		sum, e := digest(filepath.Join(dir, filepath.FromSlash(n)))
		if e != nil {
			return e
		}
		if sum != m.Payload[n] {
			return fmt.Errorf("release payload checksum mismatch: %s", n)
		}
	}
	return nil
}
func verifySidecar(path string) error {
	b, e := os.ReadFile(path + ".sha256")
	if e != nil {
		return fmt.Errorf("artifact checksum not found: %w", e)
	}
	want := strings.Fields(string(b))
	if len(want) == 0 {
		return errors.New("empty artifact checksum")
	}
	got, e := digest(path)
	if e != nil {
		return e
	}
	if got != want[0] {
		return errors.New("artifact checksum mismatch")
	}
	return nil
}
func Install(artifact string) error {
	path, e := filepath.Abs(artifact)
	if e != nil {
		return e
	}
	if e = verifySidecar(path); e != nil {
		return e
	}
	home := DefaultHome()
	if e = os.MkdirAll(filepath.Join(home, "releases"), 0755); e != nil {
		return e
	}
	stage, e := os.MkdirTemp(home, ".installing-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	if e = extract(path, stage); e != nil {
		return e
	}
	m, e := loadManifest(stage)
	if e != nil {
		return e
	}
	if m.GOOS != runtime.GOOS || m.GOARCH != runtime.GOARCH {
		return fmt.Errorf("artifact platform %s/%s does not match host %s/%s", m.GOOS, m.GOARCH, runtime.GOOS, runtime.GOARCH)
	}
	if e = verifyPayload(stage, m); e != nil {
		return e
	}
	if e = Smoke(filepath.Join(stage, m.Entrypoint)); e != nil {
		return e
	}
	target := filepath.Join(home, "releases", m.ReleaseID)
	if _, e = os.Stat(target); e == nil {
		return fmt.Errorf("runtime release already exists: %s", target)
	}
	if e = os.Rename(stage, target); e != nil {
		return e
	}
	if runtime.GOOS == "windows" {
		stable := filepath.Join(home, "pubmed-surfingctl.exe")
		tmp := stable + fmt.Sprintf(".%d.tmp", os.Getpid())
		if e = copyFile(filepath.Join(target, m.Control), tmp, 0755); e != nil {
			return e
		}
		if e = os.Rename(tmp, stable); e != nil {
			os.Remove(tmp)
			return e
		}
	}
	return Activate(m.ReleaseID)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
func currentRelease(home string) (string, error) {
	if runtime.GOOS == "windows" {
		var x struct {
			ReleaseID string `json:"release_id"`
		}
		b, e := os.ReadFile(filepath.Join(home, "current.json"))
		if e != nil {
			return "", e
		}
		if e = json.Unmarshal(b, &x); e != nil {
			return "", e
		}
		return x.ReleaseID, nil
	}
	target, e := os.Readlink(filepath.Join(home, "current"))
	if e != nil {
		return "", e
	}
	return filepath.Base(target), nil
}
func Activate(id string) error {
	if id == "" || strings.ContainsAny(id, "/\\") {
		return errors.New("invalid release_id")
	}
	home := DefaultHome()
	target := filepath.Join(home, "releases", id)
	m, e := loadManifest(target)
	if e != nil {
		return e
	}
	if e = verifyPayload(target, m); e != nil {
		return e
	}
	if m.GOOS != runtime.GOOS || m.GOARCH != runtime.GOARCH {
		return errors.New("installed release platform mismatch")
	}
	if e = Smoke(filepath.Join(target, m.Entrypoint)); e != nil {
		return e
	}
	if runtime.GOOS == "windows" {
		tmp := filepath.Join(home, fmt.Sprintf(".current-%d.json", os.Getpid()))
		if e = writeJSON(tmp, map[string]string{"release_id": id}); e != nil {
			return e
		}
		return os.Rename(tmp, filepath.Join(home, "current.json"))
	}
	current := filepath.Join(home, "current")
	if info, e := os.Lstat(current); e == nil && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("refusing to replace non-symlink runtime entry: %s", current)
	}
	next := filepath.Join(home, fmt.Sprintf(".current-%d", os.Getpid()))
	os.Remove(next)
	if e = os.Symlink(filepath.Join("releases", id), next); e != nil {
		return e
	}
	return os.Rename(next, current)
}
func Verify(path string) error {
	home := DefaultHome()
	if path == "" {
		id, e := currentRelease(home)
		if e != nil {
			return e
		}
		path = filepath.Join(home, "releases", id)
	}
	resolved, e := resolveRuntimePath(path)
	if e != nil {
		return e
	}
	path = resolved
	m, e := loadManifest(path)
	if e != nil {
		return e
	}
	if e = verifyPayload(path, m); e != nil {
		return e
	}
	return Smoke(filepath.Join(path, m.Entrypoint))
}

func resolveRuntimePath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

func RunCurrent() error {
	home := DefaultHome()
	id, e := currentRelease(home)
	if e != nil {
		return e
	}
	m, e := loadManifest(filepath.Join(home, "releases", id))
	if e != nil {
		return e
	}
	cmd := exec.Command(filepath.Join(home, "releases", id, m.Entrypoint))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
func Smoke(binary string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "pubmed-surfing-go-smoke", Version: pubmed.Version}, nil)
	session, e := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(binary)}, nil)
	if e != nil {
		return e
	}
	defer session.Close()
	listed, e := session.ListTools(ctx, nil)
	if e != nil {
		return e
	}
	want := []string{"pubmed_clear_cache", "pubmed_convert_ids", "pubmed_fetch_abstract", "pubmed_fetch_abstracts_batch", "pubmed_find_related", "pubmed_format_citations", "pubmed_get_total_count", "pubmed_journal_mesh_profile", "pubmed_journal_profile", "pubmed_search", "pubmed_verify_article_type"}
	got := []string{}
	for _, t := range listed.Tools {
		got = append(got, t.Name)
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("unexpected MCP tools: %v", got)
	}
	res, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "pubmed_clear_cache", Arguments: map[string]any{}})
	if e != nil {
		return e
	}
	if res.IsError {
		return errors.New("pubmed_clear_cache returned MCP error")
	}
	return nil
}
