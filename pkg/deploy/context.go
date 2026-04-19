package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type RepoContext struct {
	RepoDir       string        `json:"repo_dir"`
	Tree          []string      `json:"tree"`
	KeyFiles      []RepoSnippet `json:"key_files"`
	DetectedHints []string      `json:"detected_hints"`
}

type RepoSnippet struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

var interestingFiles = []string{
	"README",
	"README.md",
	"README.txt",
	"package.json",
	"package-lock.json",
	"yarn.lock",
	"pnpm-lock.yaml",
	"requirements.txt",
	"pyproject.toml",
	"Pipfile",
	"Pipfile.lock",
	"setup.py",
	"manage.py",
	"app.py",
	"main.py",
	"go.mod",
	"main.go",
	"Cargo.toml",
	"pom.xml",
	"build.gradle",
	"composer.json",
	"Gemfile",
	"index.html",
	"vite.config.js",
	"vite.config.ts",
	"next.config.js",
	"next.config.mjs",
	"nuxt.config.js",
	"nuxt.config.ts",
	"angular.json",
}

func CollectRepoContext(repoDir string) (*RepoContext, error) {
	tree, err := collectTree(repoDir, 160)
	if err != nil {
		return nil, err
	}

	snippets, err := collectSnippets(repoDir)
	if err != nil {
		return nil, err
	}

	hints := detectHints(snippets)

	return &RepoContext{
		RepoDir:       repoDir,
		Tree:          tree,
		KeyFiles:      snippets,
		DetectedHints: hints,
	}, nil
}

func collectTree(repoDir string, maxEntries int) ([]string, error) {
	var out []string

	err := filepath.Walk(repoDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == repoDir {
			return nil
		}

		rel, err := filepath.Rel(repoDir, path)
		if err != nil {
			return err
		}

		name := info.Name()
		if info.IsDir() {
			if name == ".git" || name == "node_modules" || name == ".venv" || name == "venv" || name == "dist" || name == "build" || name == "__pycache__" {
				return filepath.SkipDir
			}
			out = append(out, rel+"/")
		} else {
			out = append(out, rel)
		}

		if len(out) >= maxEntries {
			return stopWalk
		}
		return nil
	})

	if err != nil && err != stopWalk {
		return nil, fmt.Errorf("collect repo tree: %w", err)
	}

	sort.Strings(out)
	return out, nil
}

func collectSnippets(repoDir string) ([]RepoSnippet, error) {
	type candidate struct {
		path  string
		score int
	}
	var candidates []candidate

	err := filepath.Walk(repoDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == ".venv" || name == "venv" || name == "dist" || name == "build" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(repoDir, path)
		if err != nil {
			return err
		}

		base := filepath.Base(rel)
		score := fileScore(base, rel)
		if score > 0 {
			candidates = append(candidates, candidate{path: rel, score: score})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect snippets: %w", err)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].score > candidates[j].score
	})

	seen := map[string]bool{}
	var snippets []RepoSnippet
	for _, c := range candidates {
		if len(snippets) >= 14 {
			break
		}
		if seen[c.path] {
			continue
		}
		seen[c.path] = true

		full := filepath.Join(repoDir, c.path)
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		content := trimSnippet(string(data), 3200)
		snippets = append(snippets, RepoSnippet{
			Path:    c.path,
			Content: content,
		})
	}

	return snippets, nil
}

func fileScore(base, rel string) int {
	for _, name := range interestingFiles {
		if base == name || strings.EqualFold(base, name) {
			return 100
		}
	}

	lowerRel := strings.ToLower(rel)
	switch {
	case strings.HasPrefix(lowerRel, "src/"):
		return 30
	case strings.HasPrefix(lowerRel, "app/"):
		return 30
	case strings.Contains(lowerRel, "main."):
		return 40
	case strings.Contains(lowerRel, "app."):
		return 40
	case strings.HasSuffix(lowerRel, ".py"),
		strings.HasSuffix(lowerRel, ".js"),
		strings.HasSuffix(lowerRel, ".ts"),
		strings.HasSuffix(lowerRel, ".tsx"),
		strings.HasSuffix(lowerRel, ".go"),
		strings.HasSuffix(lowerRel, ".rs"),
		strings.HasSuffix(lowerRel, ".java"),
		strings.HasSuffix(lowerRel, ".php"),
		strings.HasSuffix(lowerRel, ".rb"):
		return 10
	default:
		return 0
	}
}

func trimSnippet(s string, max int) string {
	s = strings.ReplaceAll(s, "\x00", "")
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...<truncated>..."
}

func detectHints(snippets []RepoSnippet) []string {
	var hints []string
	seen := map[string]bool{}

	add := func(v string) {
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		hints = append(hints, v)
	}

	for _, sn := range snippets {
		lp := strings.ToLower(sn.Path)
		lc := strings.ToLower(sn.Content)

		switch {
		case strings.HasSuffix(lp, "package.json"):
			add("nodejs")
			if strings.Contains(lc, "vite") {
				add("vite")
			}
			if strings.Contains(lc, "react") {
				add("react")
			}
			if strings.Contains(lc, "next") {
				add("nextjs")
			}
			if strings.Contains(lc, "\"start\"") {
				add("has_start_script")
			}
		case strings.HasSuffix(lp, "requirements.txt"),
			strings.HasSuffix(lp, "pyproject.toml"),
			strings.HasSuffix(lp, "pipfile"):
			add("python")
		case strings.HasSuffix(lp, "go.mod"):
			add("golang")
		case strings.HasSuffix(lp, "cargo.toml"):
			add("rust")
		case strings.HasSuffix(lp, "pom.xml"),
			strings.HasSuffix(lp, "build.gradle"):
			add("java")
		}

		if strings.Contains(lc, "flask") {
			add("flask")
		}
		if strings.Contains(lc, "fastapi") {
			add("fastapi")
		}
		if strings.Contains(lc, "express") {
			add("express")
		}
		if strings.Contains(lc, "listen(") || strings.Contains(lc, "app.run(") {
			add("network_service")
		}
	}

	sort.Strings(hints)
	return hints
}

var stopWalk = fmt.Errorf("stop walk")
