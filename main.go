package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Version is the current version of Sanctify.
const Version = "0.1.2"

const commitIterationLimit = 100

// Regular expressions moved to global scope for performance
var (
	// Group 1: Type, Group 2: Scope, Group 3: Breaking (!), Group 4: Description
	commitRegex         = regexp.MustCompile(`^([a-z]+)(?:\(([^)]+)\))?(!?): (.+)$`)
	breakingFooterRegex = regexp.MustCompile(`(?m)^BREAKING[ -]CHANGE:`)
	goVersionRegex      = regexp.MustCompile(`(?m)^(const\s+Version\s*=\s*)"[^"]+"`)
	jsonVersionRegex    = regexp.MustCompile(`(?m)^(\s*"version":\s*)"[^"]+"`)
	yamlVersionRegex    = regexp.MustCompile(`(?m)^(version:\s*).+`)
	hclVersionRegex     = regexp.MustCompile(`(?m)^(\s*(?:version|default)\s*=\s*)"[^"]+"`)
	// Preserves existing quotes (single, double or none)
	dockerVersionRegex = regexp.MustCompile(`(?m)^(\s*(?:LABEL|ENV)\s+(?:"VERSION"|'VERSION'|VERSION)[ \t]*=?[ \t]*)(["']?)[^"'\s]+(["']?)(.*)`)
)

type ReleaseContext string

const (
	CtxMain    ReleaseContext = "main"
	CtxPR      ReleaseContext = "pr"
	CtxFeature ReleaseContext = "feature"
)

type CommitInfo struct {
	Type        string
	Scope       string
	IsBreaking  bool
	Description string
}

type CommitGroup struct {
	Breaking    []string
	Features    []string
	Fixes       []string
	Maintenance []string
}

func main() {
	if err := run(os.Args, os.Stdout, os.Stderr, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer, now time.Time) error {
	fs := flag.NewFlagSet("sanctify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: sanctify [options] [version]\n\nOptions:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nArguments:\n  version    Optional: Manually override the calculated version (e.g. 1.2.3)\n")
	}

	dryRun := fs.Bool("dry-run", false, "Calculate version and print it without making any changes")
	ctxFlag := fs.String("context", "", "Override release context (main, pr, feature)")
	tagBranchFlag := fs.String("tag-branch", "main,master", "Comma-separated list of branches to treat as main")
	versionFlag := fs.Bool("version", false, "Print the current version of sanctify")

	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if *versionFlag {
		fmt.Fprintln(stdout, Version)
		return nil
	}

	repo, err := git.PlainOpen(".")
	isRepo := err == nil

	var lastVer *semver.Version = semver.MustParse("0.0.0")
	var lastTag plumbing.Hash
	var head *plumbing.Reference
	var headHash plumbing.Hash

	if isRepo {
		head, err = repo.Head()
		if err == nil && head != nil {
			headHash = head.Hash()
			lastTag, lastVer, _ = getLastStableTag(repo)
		}
	}

	var nextVer *semver.Version
	var commits []CommitInfo
	if fs.NArg() > 0 {
		nextVer, err = semver.NewVersion(fs.Arg(0))
		if err != nil {
			return fmt.Errorf("invalid version argument: %w", err)
		}
	} else if isRepo && head != nil {
		commits, err = getCommitsSinceTag(repo, headHash, lastTag)
		if err != nil {
			return fmt.Errorf("failed to analyze commits: %w", err)
		}
		nextVer = calculateNextVersion(commits, lastVer)
	} else {
		v := lastVer.IncPatch()
		nextVer = &v
	}

	relCtx, prNum := determineContext(head, *tagBranchFlag)
	if *ctxFlag != "" {
		switch *ctxFlag {
		case "main":
			relCtx = CtxMain
		case "pr":
			relCtx = CtxPR
			if prNum == "" {
				prNum = "0"
			}
		case "feature":
			relCtx = CtxFeature
		default:
			return fmt.Errorf("invalid context: %s (must be main, pr, or feature)", *ctxFlag)
		}
	}

	hashStr := "0000000"
	if head != nil {
		hashStr = head.Hash().String()[:7]
	}
	finalVersion := formatVersion(nextVer, relCtx, prNum, hashStr)

	if *dryRun {
		fmt.Fprintln(stdout, finalVersion)
		return nil
	}

	fmt.Fprintf(stdout, "Releasing version: %s (Context: %s)\n", finalVersion, relCtx)
	if err := updateVersionInFiles(finalVersion); err != nil {
		return fmt.Errorf("failed to update version in files: %w", err)
	}

	if !isRepo || head == nil {
		fmt.Fprintln(stdout, "Not a Git repository or empty. Skipping Git operations.")
		return nil
	}

	if relCtx == CtxMain {
		if err := updateChangelog(commits, finalVersion, now); err != nil {
			return fmt.Errorf("failed to update changelog: %w", err)
		}
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	author := getAuthor(repo, now)
	files := []string{"main.go", "VERSION", "package.json", "package-lock.json", "composer.json", "metadata.json", "meta/main.yml", "version.tf", "CHANGELOG.md"}
	if drupalFiles, err := filepath.Glob("*.info.yml"); err == nil {
		files = append(files, drupalFiles...)
	}
	if dockerFiles, err := filepath.Glob("Dockerfile*"); err == nil {
		files = append(files, dockerFiles...)
	}

	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			if _, err := w.Add(f); err != nil {
				return fmt.Errorf("failed to stage file %s: %w", f, err)
			}
		}
	}

	commitMsg := fmt.Sprintf("chore(release): %s", finalVersion)
	if relCtx != CtxMain {
		commitMsg = fmt.Sprintf("chore: bump version to %s", finalVersion)
	}

	commitHash, err := w.Commit(commitMsg, &git.CommitOptions{Author: author})
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	tagName := "v" + finalVersion
	_, err = repo.CreateTag(tagName, commitHash, &git.CreateTagOptions{
		Tagger:  author,
		Message: "Release " + finalVersion,
	})
	if err != nil {
		return fmt.Errorf("failed to create tag: %w", err)
	}

	return pushToRemote(repo, head.Name().String(), tagName)
}

func parseCommit(message string) CommitInfo {
	lines := strings.Split(message, "\n")
	header := lines[0]
	info := CommitInfo{}

	matches := commitRegex.FindStringSubmatch(header)
	if matches != nil {
		info.Type = matches[1]
		info.Scope = matches[2]
		info.IsBreaking = matches[3] == "!"
		info.Description = matches[4]
	} else {
		info.Description = header
	}

	if !info.IsBreaking && breakingFooterRegex.MatchString(message) {
		info.IsBreaking = true
	}

	return info
}

func getLastStableTag(repo *git.Repository) (plumbing.Hash, *semver.Version, error) {
	tags, err := repo.Tags()
	if err != nil {
		return plumbing.ZeroHash, semver.MustParse("0.0.0"), err
	}
	var lastVer *semver.Version
	var lastHash plumbing.Hash

	err = tags.ForEach(func(t *plumbing.Reference) error {
		vStr := strings.TrimPrefix(t.Name().Short(), "v")
		v, err := semver.NewVersion(vStr)
		if err == nil && v.Prerelease() == "" {
			if lastVer == nil || v.GreaterThan(lastVer) {
				lastVer = v
				lastHash = t.Hash()
				if tagObj, err := repo.TagObject(t.Hash()); err == nil {
					lastHash = tagObj.Target
				}
			}
		}
		return nil
	})

	if lastVer == nil {
		return plumbing.ZeroHash, semver.MustParse("0.0.0"), nil
	}
	return lastHash, lastVer, err
}

func getCommitsSinceTag(repo *git.Repository, head, since plumbing.Hash) ([]CommitInfo, error) {
	commits, err := repo.Log(&git.LogOptions{From: head})
	if err != nil {
		return nil, err
	}
	defer commits.Close()

	var result []CommitInfo
	count := 0
	for {
		c, err := commits.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if c.Hash == since {
			break
		}
		if since == plumbing.ZeroHash && count >= commitIterationLimit {
			break
		}
		result = append(result, parseCommit(c.Message))
		count++
	}
	return result, nil
}

func calculateNextVersion(commits []CommitInfo, lastVer *semver.Version) *semver.Version {
	major, minor, patch := false, false, false
	for _, info := range commits {
		patch = true
		if info.IsBreaking {
			major = true
		} else if info.Type == "feat" {
			minor = true
		}
	}

	res := *lastVer
	if major {
		res = res.IncMajor()
	} else if minor {
		res = res.IncMinor()
	} else if patch {
		res = res.IncPatch()
	}
	return &res
}

func determineContext(head *plumbing.Reference, tagBranches string) (ReleaseContext, string) {
	ciVars := []string{"GITHUB_PR_NUMBER", "CI_MERGE_REQUEST_IID", "BITBUCKET_PR_ID", "CHANGE_ID"}
	for _, v := range ciVars {
		if val := os.Getenv(v); val != "" {
			return CtxPR, val
		}
	}

	if head != nil {
		branch := head.Name().Short()
		for _, b := range strings.Split(tagBranches, ",") {
			if branch == strings.TrimSpace(b) {
				return CtxMain, ""
			}
		}
	}

	return CtxFeature, ""
}

func formatVersion(v *semver.Version, ctx ReleaseContext, prNum, hash string) string {
	base := v.String()
	switch ctx {
	case CtxPR:
		return fmt.Sprintf("%s-rc.%s", base, prNum)
	case CtxFeature:
		return fmt.Sprintf("%s-%s", base, hash)
	default:
		return base
	}
}

func updateVersionInFiles(ver string) error {
	staticFiles := []struct {
		path string
		re   *regexp.Regexp
		repl string
	}{
		{"main.go", goVersionRegex, `${1}"` + ver + `"`},
		{"package.json", jsonVersionRegex, `${1}"` + ver + `"`},
		{"package-lock.json", jsonVersionRegex, `${1}"` + ver + `"`},
		{"composer.json", jsonVersionRegex, `${1}"` + ver + `"`},
		{"metadata.json", jsonVersionRegex, `${1}"` + ver + `"`},
		{"meta/main.yml", yamlVersionRegex, `${1}` + ver},
		{"version.tf", hclVersionRegex, `${1}"` + ver + `"`},
	}
	for _, f := range staticFiles {
		if err := updateFile(f.path, f.re, f.repl); err != nil {
			return err
		}
	}

	if err := updatePlainVersion("VERSION", ver); err != nil {
		return err
	}

	globPatterns := []struct {
		pattern string
		re      *regexp.Regexp
		repl    string
	}{
		{"*.info.yml", yamlVersionRegex, `${1}` + ver},
		{"Dockerfile*", dockerVersionRegex, `${1}${2}` + ver + `${3}${4}`},
	}
	for _, g := range globPatterns {
		files, err := filepath.Glob(g.pattern)
		if err != nil {
			continue
		}
		for _, f := range files {
			if err := updateFile(f, g.re, g.repl); err != nil {
				return err
			}
		}
	}

	return nil
}

func updatePlainVersion(file, ver string) error {
	info, err := os.Stat(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(file, []byte(ver+"\n"), info.Mode())
}

func updateFile(file string, re *regexp.Regexp, replacement string) error {
	info, err := os.Stat(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	newContent := re.ReplaceAll(content, []byte(replacement))
	return os.WriteFile(file, newContent, info.Mode())
}

func updateChangelog(commits []CommitInfo, ver string, now time.Time) error {
	groups := CommitGroup{}
	for _, info := range commits {
		display := info.Description
		if info.Scope != "" {
			display = fmt.Sprintf("**%s**: %s", info.Scope, info.Description)
		}

		if info.IsBreaking {
			groups.Breaking = append(groups.Breaking, display)
		} else {
			switch info.Type {
			case "feat":
				groups.Features = append(groups.Features, display)
			case "fix", "perf":
				groups.Fixes = append(groups.Fixes, display)
			default:
				groups.Maintenance = append(groups.Maintenance, display)
			}
		}
	}

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("## [%s] - %s\n", ver, now.Format("2006-01-02")))
	sections := []struct {
		name string
		data []string
	}{
		{"BREAKING CHANGES", groups.Breaking},
		{"Features", groups.Features},
		{"Bug Fixes", groups.Fixes},
		{"Maintenance", groups.Maintenance},
	}
	for _, s := range sections {
		if len(s.data) > 0 {
			buf.WriteString("\n### " + s.name + "\n")
			for _, m := range s.data {
				buf.WriteString("- " + m + "\n")
			}
		}
	}
	buf.WriteString("\n")

	file := "CHANGELOG.md"
	info, err := os.Stat(file)
	var mode os.FileMode = 0644
	var oldContent []byte
	if err == nil {
		mode = info.Mode()
		oldContent, err = os.ReadFile(file)
		if err != nil {
			return err
		}
	}
	return os.WriteFile(file, append(buf.Bytes(), oldContent...), mode)
}

func getAuthor(repo *git.Repository, now time.Time) *object.Signature {
	name, email := os.Getenv("GIT_AUTHOR_NAME"), os.Getenv("GIT_AUTHOR_EMAIL")
	if name == "" || email == "" {
		if cfg, err := repo.Config(); err == nil {
			if name == "" {
				name = cfg.User.Name
			}
			if email == "" {
				email = cfg.User.Email
			}
		}
	}
	if name == "" {
		name = "Sanctify Bot"
	}
	if email == "" {
		email = "bot@sanctify.local"
	}
	return &object.Signature{Name: name, Email: email, When: now}
}

func pushToRemote(repo *git.Repository, branch, tag string) error {
	auth, cleanup, err := getAuth()
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	err = repo.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth:       auth,
		RefSpecs: []config.RefSpec{
			config.RefSpec(branch + ":" + branch),
			config.RefSpec("refs/tags/" + tag + ":refs/tags/" + tag),
		},
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

func getAuth() (transport.AuthMethod, func(), error) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return &http.BasicAuth{Username: "x-access-token", Password: token}, nil, nil
	}
	if token := os.Getenv("GITLAB_TOKEN"); token != "" {
		return &http.BasicAuth{Username: "oauth2", Password: token}, nil, nil
	}
	if token := os.Getenv("BITBUCKET_TOKEN"); token != "" {
		return &http.BasicAuth{Username: "x-token-auth", Password: token}, nil, nil
	}

	if key := os.Getenv("SSH_PRIVATE_KEY"); key != "" {
		pass := os.Getenv("SSH_KEY_PASSPHRASE")
		auth, err := ssh.NewPublicKeys("git", []byte(key), pass)
		return auth, nil, err
	}

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			agentClient := agent.NewClient(conn)
			return &ssh.PublicKeysCallback{
				User:     "git",
				Callback: agentClient.Signers,
			}, func() { conn.Close() }, nil
		}
	}

	home, _ := os.UserHomeDir()
	paths := []string{filepath.Join(home, ".ssh", "id_ed25519"), filepath.Join(home, ".ssh", "id_rsa")}
	for _, p := range paths {
		auth, err := ssh.NewPublicKeysFromFile("git", p, os.Getenv("SSH_KEY_PASSPHRASE"))
		if err == nil {
			return auth, nil, nil
		}
	}
	return nil, nil, nil
}
