package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func commitFile(t *testing.T, w *git.Worktree, filename, msg string, auth *object.Signature) {
	err := os.WriteFile(filepath.Join(w.Filesystem.Root(), filename), []byte("test"), 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Add(filename)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Commit(msg, &git.CommitOptions{Author: auth})
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseCommit(t *testing.T) {
	tests := []struct {
		msg      string
		expected CommitInfo
	}{
		{"feat(ui): add button", CommitInfo{Type: "feat", Scope: "ui", Description: "add button"}},
		{"fix!: critical bug", CommitInfo{Type: "fix", IsBreaking: true, Description: "critical bug"}},
		{"chore: update deps\n\nBREAKING CHANGE: some error", CommitInfo{Type: "chore", IsBreaking: true, Description: "update deps"}},
		{"simple message", CommitInfo{Description: "simple message"}},
	}

	for _, tt := range tests {
		result := parseCommit(tt.msg)
		if result.Type != tt.expected.Type || result.IsBreaking != tt.expected.IsBreaking || result.Description != tt.expected.Description {
			t.Errorf("for %q, expected %+v, got %+v", tt.msg, tt.expected, result)
		}
	}
}

func TestFormatVersion(t *testing.T) {
	v := semver.MustParse("1.2.3")
	tests := []struct {
		name     string
		ctx      ReleaseContext
		prNum    string
		hash     string
		expected string
	}{
		{"Main context", CtxMain, "", "abcdef1", "1.2.3"},
		{"PR context", CtxPR, "42", "abcdef1", "1.2.3-rc.42"},
		{"Feature context", CtxFeature, "", "abcdef1", "1.2.3-abcdef1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatVersion(v, tt.ctx, tt.prNum, tt.hash)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestCalculateNextVersion(t *testing.T) {
	lastVer := semver.MustParse("1.0.0")

	tests := []struct {
		commits  []CommitInfo
		expected string
	}{
		{[]CommitInfo{{Type: "fix"}}, "1.0.1"},
		{[]CommitInfo{{Type: "feat"}}, "1.1.0"},
		{[]CommitInfo{{Type: "fix", IsBreaking: true}}, "2.0.0"},
		{[]CommitInfo{{Type: "chore"}}, "1.0.1"},
	}

	for _, tt := range tests {
		result := calculateNextVersion(tt.commits, lastVer)
		if result.String() != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, result.String())
		}
	}
}

func TestDetermineContext(t *testing.T) {
	ciVars := []string{"GITHUB_PR_NUMBER", "CI_MERGE_REQUEST_IID", "BITBUCKET_PR_ID", "CHANGE_ID"}
	for _, v := range ciVars {
		t.Setenv(v, "")
	}

	t.Setenv("GITHUB_PR_NUMBER", "123")
	ctx, pr := determineContext(nil, "main")
	if ctx != CtxPR || pr != "123" {
		t.Errorf("expected PR context, got %s", ctx)
	}
}

func TestUpdateVersionFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	pkgPath := "package.json"
	// Ensure precise format for testing
	os.WriteFile(pkgPath, []byte(`{
  "version": "0.0.0"
}`), 0644)

	verPath := "VERSION"
	os.WriteFile(verPath, []byte("0.0.0\n"), 0755)

	err := updateVersionInFiles("1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(pkgPath)
	if !strings.Contains(string(content), `"version": "1.2.3"`) {
		t.Errorf("package.json not updated correctly: %s", string(content))
	}

	content, _ = os.ReadFile(verPath)
	if string(content) != "1.2.3\n" {
		t.Errorf("VERSION not updated correctly: %q", string(content))
	}

	info, _ := os.Stat(verPath)
	if info.Mode() != 0755 {
		t.Errorf("file permissions not preserved: %v", info.Mode())
	}
}

func TestUpdateChangelog(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	commits := []CommitInfo{
		{Type: "feat", Description: "new feature"},
		{Type: "fix", Description: "bug fix"},
	}

	now := time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)
	err := updateChangelog(commits, "1.0.0", now)
	if err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile("CHANGELOG.md")
	expected := "## [1.0.0] - 2026-04-05\n\n### Features\n- new feature\n\n### Bug Fixes\n- bug fix\n\n"
	if string(content) != expected {
		t.Errorf("changelog mismatch.\nExpected:\n%q\nGot:\n%q", expected, string(content))
	}
}

func TestGetLastStableTag(t *testing.T) {
	dir := t.TempDir()
	repo, _ := git.PlainInit(dir, false)
	w, _ := repo.Worktree()
	signature := &object.Signature{Name: "Test", Email: "test@test.com", When: time.Now()}

	commitFile(t, w, "f1.txt", "initial", signature)
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	commitHash := head.Hash()

	err = repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/v1.0.0"), commitHash))
	if err != nil {
		t.Fatal(err)
	}

	hash, ver, err := getLastStableTag(repo)
	if err != nil {
		t.Fatal(err)
	}
	if ver.String() != "1.0.0" {
		t.Errorf("expected 1.0.0, got %s", ver.String())
	}
	if hash != commitHash {
		t.Errorf("hash mismatch: expected %s, got %s", commitHash, hash)
	}
}
