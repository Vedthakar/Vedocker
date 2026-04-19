package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const geminiGenerateContentURL = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"

type RepoAnalysis struct {
	AppType           string   `json:"app_type"`
	Runtime           string   `json:"runtime"`
	Framework         string   `json:"framework"`
	InstallSteps      []string `json:"install_steps"`
	BuildSteps        []string `json:"build_steps"`
	RunCommand        []string `json:"run_command"`
	LikelyExposedPort int      `json:"likely_exposed_port"`
	NeedsBuild        bool     `json:"needs_build"`
	Reasoning         string   `json:"reasoning"`
	Risks             []string `json:"risks"`
	Confidence        string   `json:"confidence"`
}

type DockerfileGeneration struct {
	DockerfileText string   `json:"dockerfile_text"`
	ExposedPort    int      `json:"exposed_port"`
	StartupCommand []string `json:"startup_command"`
	Rationale      string   `json:"rationale"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	Contents          []geminiContent         `json:"contents"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	ResponseMimeType string          `json:"responseMimeType,omitempty"`
	ResponseSchema   map[string]any  `json:"responseSchema,omitempty"`
	Temperature      float64         `json:"temperature,omitempty"`
	ThinkingConfig   *thinkingConfig `json:"thinkingConfig,omitempty"`
}

type thinkingConfig struct {
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

func AnalyzeRepoWithGemini(apiKey, repoURL string, ctx *RepoContext) (*RepoAnalysis, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("Gemini API key is required for AI fallback")
	}

	systemPrompt := strings.TrimSpace(`
You are analyzing a source repository to infer how it should be containerized.
Return JSON only.
Do not invent unsupported Docker features.
Prefer compatibility with a simple Dockerfile builder/runtime.
Infer practical install, build, and run behavior from the provided repo context.
- Never use privileged ports below 1024
- Prefer port 8080 for simple HTTP services
- Generated containers must run without requiring privileged bind permissions
- Do not use EXPOSE 80
- Do not bind servers to port 80
`)

	userPrompt := buildAnalysisPrompt(repoURL, ctx)

	var out RepoAnalysis
	if err := callGeminiJSON(apiKey, systemPrompt, userPrompt, analysisSchema(), &out); err != nil {
		return nil, err
	}

	if len(out.RunCommand) == 0 {
		return nil, fmt.Errorf("analysis did not provide a run_command")
	}
	return &out, nil
}

func GenerateDockerfileWithGemini(apiKey, repoURL string, ctx *RepoContext, analysis *RepoAnalysis) (*DockerfileGeneration, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("Gemini API key is required for AI fallback")
	}
	if analysis == nil {
		return nil, fmt.Errorf("analysis is required")
	}

	analysisJSON, _ := json.MarshalIndent(analysis, "", "  ")

	systemPrompt := strings.TrimSpace(`
You generate Dockerfiles for a lightweight local container platform.
Return JSON only.
Rules:
- Dockerfile must start with FROM
- prefer FROM, WORKDIR, COPY, RUN, EXPOSE, CMD, ENTRYPOINT
- avoid multi-stage builds
- avoid ARG-heavy logic
- avoid fancy shell tricks
- prefer JSON array CMD/ENTRYPOINT when possible
- include EXPOSE only if there is a real reason
- Dockerfile must work in normal Docker and in a simple Dockerfile-lite builder
- Never use privileged ports below 1024
- Prefer port 8080 for simple HTTP services
- Generated containers must run without requiring privileged bind permissions
- Do not use EXPOSE 80
- Do not bind servers to port 80
`)

	userPrompt := buildDockerfilePrompt(repoURL, ctx, string(analysisJSON))

	var out DockerfileGeneration
	if err := callGeminiJSON(apiKey, systemPrompt, userPrompt, dockerfileSchema(), &out); err != nil {
		return nil, err
	}

	if staticText, ok := maybeUseStaticHTMLDockerfile(ctx); ok {
		out.DockerfileText = staticText
	} else {
		out.DockerfileText = normalizeGeneratedDockerfile(out.DockerfileText)
	}

	if err := ValidateGeneratedDockerfile(out.DockerfileText); err != nil {
		return nil, err
	}
	return &out, nil
}

func ValidateGeneratedDockerfile(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("generated Dockerfile is empty")
	}

	lines := strings.Split(text, "\n")
	allowed := map[string]bool{
		"FROM":       true,
		"WORKDIR":    true,
		"COPY":       true,
		"RUN":        true,
		"ENV":        true,
		"EXPOSE":     true,
		"CMD":        true,
		"ENTRYPOINT": true,
		"MAINTAINER": true,
	}

	foundFrom := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		inst := strings.ToUpper(parts[0])
		if !allowed[inst] {
			return fmt.Errorf("generated Dockerfile contains unsupported instruction: %s", inst)
		}
		if !foundFrom {
			if inst != "FROM" {
				return fmt.Errorf("generated Dockerfile must start with FROM")
			}
			foundFrom = true
		}
		if inst == "EXPOSE" {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				p, err := strconv.Atoi(strings.TrimSpace(fields[1]))
				if err == nil && p > 0 && p < 1024 {
					return fmt.Errorf("generated Dockerfile uses privileged EXPOSE port: %d", p)
				}
			}
		}
	}

	if !foundFrom {
		return fmt.Errorf("generated Dockerfile missing FROM")
	}

	return nil
}

func callGeminiJSON(apiKey, systemPrompt, userPrompt string, schema map[string]any, out any) error {
	model := strings.TrimSpace(os.Getenv("MINICONTAINER_GEMINI_MODEL"))
	if model == "" {
		model = "gemini-3-flash-preview"
	}

	reqBody := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{
				Parts: []geminiPart{{Text: userPrompt}},
			},
		},
		GenerationConfig: &geminiGenerationConfig{
			ResponseMimeType: "application/json",
			ResponseSchema:   schema,
			Temperature:      0.1,
			ThinkingConfig: &thinkingConfig{
				ThinkingLevel: "low",
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal Gemini request: %w", err)
	}

	url := fmt.Sprintf(geminiGenerateContentURL, model)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("build Gemini request: %w", err)
	}
	req.Header.Set("x-goog-api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Gemini request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read Gemini response: %w", err)
	}

	var parsed geminiResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("parse Gemini response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return fmt.Errorf("Gemini API error: %s", parsed.Error.Message)
		}
		return fmt.Errorf("Gemini API error: status %d", resp.StatusCode)
	}

	if len(parsed.Candidates) == 0 {
		return fmt.Errorf("Gemini returned no candidates")
	}
	if len(parsed.Candidates[0].Content.Parts) == 0 {
		return fmt.Errorf("Gemini returned no content parts")
	}

	content := strings.TrimSpace(parsed.Candidates[0].Content.Parts[0].Text)
	content = stripCodeFence(content)
	if content == "" {
		return fmt.Errorf("Gemini returned empty content")
	}

	if err := json.Unmarshal([]byte(content), out); err != nil {
		return fmt.Errorf("parse structured Gemini JSON output: %w", err)
	}

	return nil
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}

func buildAnalysisPrompt(repoURL string, ctx *RepoContext) string {
	var b strings.Builder

	b.WriteString("Analyze this repo for containerization.\n")
	b.WriteString("Repo URL: " + repoURL + "\n\n")
	b.WriteString("Repo tree:\n")
	for _, item := range ctx.Tree {
		b.WriteString("- " + item + "\n")
	}

	if len(ctx.DetectedHints) > 0 {
		b.WriteString("\nDetected hints:\n")
		for _, h := range ctx.DetectedHints {
			b.WriteString("- " + h + "\n")
		}
	}

	b.WriteString("\nImportant file excerpts:\n")
	for _, sn := range ctx.KeyFiles {
		b.WriteString("\n=== FILE: " + sn.Path + " ===\n")
		b.WriteString(sn.Content)
		b.WriteString("\n")
	}

	return b.String()
}

func buildDockerfilePrompt(repoURL string, ctx *RepoContext, analysisJSON string) string {
	var b strings.Builder

	b.WriteString("Generate one practical Dockerfile for this repo.\n")
	b.WriteString("Repo URL: " + repoURL + "\n\n")
	b.WriteString("Analysis JSON:\n")
	b.WriteString(analysisJSON)
	b.WriteString("\n\nRepo tree:\n")
	for _, item := range ctx.Tree {
		b.WriteString("- " + item + "\n")
	}

	b.WriteString("\nImportant file excerpts:\n")
	for _, sn := range ctx.KeyFiles {
		b.WriteString("\n=== FILE: " + sn.Path + " ===\n")
		b.WriteString(sn.Content)
		b.WriteString("\n")
	}

	return b.String()
}

func analysisSchema() map[string]any {
	return map[string]any{
		"type": "OBJECT",
		"properties": map[string]any{
			"app_type":            map[string]any{"type": "STRING"},
			"runtime":             map[string]any{"type": "STRING"},
			"framework":           map[string]any{"type": "STRING"},
			"install_steps":       arrayOfStrings(),
			"build_steps":         arrayOfStrings(),
			"run_command":         arrayOfStrings(),
			"likely_exposed_port": map[string]any{"type": "INTEGER"},
			"needs_build":         map[string]any{"type": "BOOLEAN"},
			"reasoning":           map[string]any{"type": "STRING"},
			"risks":               arrayOfStrings(),
			"confidence":          map[string]any{"type": "STRING"},
		},
		"required": []string{
			"app_type",
			"runtime",
			"framework",
			"install_steps",
			"build_steps",
			"run_command",
			"likely_exposed_port",
			"needs_build",
			"reasoning",
			"risks",
			"confidence",
		},
	}
}

func dockerfileSchema() map[string]any {
	return map[string]any{
		"type": "OBJECT",
		"properties": map[string]any{
			"dockerfile_text": map[string]any{"type": "STRING"},
			"exposed_port":    map[string]any{"type": "INTEGER"},
			"startup_command": arrayOfStrings(),
			"rationale":       map[string]any{"type": "STRING"},
		},
		"required": []string{
			"dockerfile_text",
			"exposed_port",
			"startup_command",
			"rationale",
		},
	}
}

func arrayOfStrings() map[string]any {
	return map[string]any{
		"type":  "ARRAY",
		"items": map[string]any{"type": "STRING"},
	}
}

func CompactContextSummary(ctx *RepoContext) string {
	if ctx == nil {
		return ""
	}
	var lines []string
	lines = append(lines, "tree="+strconv.Itoa(len(ctx.Tree)))
	lines = append(lines, "snippets="+strconv.Itoa(len(ctx.KeyFiles)))
	if len(ctx.DetectedHints) > 0 {
		hints := append([]string{}, ctx.DetectedHints...)
		sort.Strings(hints)
		lines = append(lines, "hints="+strings.Join(hints, ","))
	}
	return strings.Join(lines, " ")
}

func normalizeGeneratedDockerfile(text string) string {
	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)

		if strings.HasPrefix(line, "EXPOSE ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "80" {
				lines[i] = "EXPOSE 8080"
				continue
			}
		}

		if strings.HasPrefix(line, "CMD ") && strings.Contains(line, `"80"`) {
			lines[i] = strings.Replace(lines[i], `"80"`, `"8080"`, 1)
			continue
		}
	}

	return strings.Join(lines, "\n")
}

func maybeUseStaticHTMLDockerfile(ctx *RepoContext) (string, bool) {
	if ctx == nil {
		return "", false
	}

	hasIndex := false
	hasPackageJSON := false
	hasRequirements := false

	for _, sn := range ctx.KeyFiles {
		switch strings.ToLower(sn.Path) {
		case "index.html":
			hasIndex = true
		case "package.json":
			hasPackageJSON = true
		case "requirements.txt":
			hasRequirements = true
		}
	}

	if hasIndex && !hasPackageJSON && !hasRequirements {
		return `FROM busybox:latest
WORKDIR /www
COPY index.html /www/index.html
EXPOSE 8080
CMD ["busybox", "httpd", "-f", "-p", "8080", "-h", "/www"]`, true
	}

	return "", false
}
