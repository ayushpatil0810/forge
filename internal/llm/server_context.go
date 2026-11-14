package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	serverContextCache sync.Map // map[string]int
	ollamaNumCtxRegex  = regexp.MustCompile(`(?m)^\s*num_ctx\s+(\d+)`)
)

func isLoopbackAddress(addr string) bool {
	raw := strings.TrimSpace(addr)
	if raw == "" {
		return true // Default local server address is loopback
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	hostname := u.Hostname()
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

// FetchServerContextLength performs a best-effort lookup of context length
// for local model servers (Ollama and LM Studio) running on loopback (SPEC Amendment C).
// It enforces loopback only, short timeout (500ms), and caches results per model per run.
// Any failure returns (0, false); it never errors out or blocks startup.
func FetchServerContextLength(ctx context.Context, provider, model, baseURL string) (int, bool) {
	prov := strings.ToLower(strings.TrimSpace(provider))
	if prov != "ollama" && prov != "lmstudio" {
		return 0, false
	}

	model = strings.TrimSpace(model)
	if model == "" {
		return 0, false
	}

	cacheKey := prov + "/" + strings.ToLower(model)
	if val, ok := serverContextCache.Load(cacheKey); ok {
		if n, ok := val.(int); ok && n > 0 {
			return n, true
		}
	}

	// Loopback enforcement
	if !isLoopbackAddress(baseURL) {
		return 0, false
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	var (
		detected int
		ok       bool
	)

	switch prov {
	case "ollama":
		detected, ok = fetchOllamaContextLength(fetchCtx, model, baseURL)
	case "lmstudio":
		detected, ok = fetchLMStudioContextLength(fetchCtx, model, baseURL)
	}

	if ok && detected > 0 {
		serverContextCache.Store(cacheKey, detected)
		return detected, true
	}

	return 0, false
}

// fetchOllamaContextLength queries Ollama's /api/show endpoint for model metadata.
// Docs: https://github.com/ollama/ollama/blob/main/docs/api.md#show-model-information
func fetchOllamaContextLength(ctx context.Context, model, baseURL string) (int, bool) {
	endpoint := baseURL
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434"
	}
	endpoint = strings.TrimRight(endpoint, "/") + "/api/show"

	reqBody, _ := json.Marshal(map[string]string{"model": model})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return 0, false
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return 0, false
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, false
	}

	var showResp struct {
		Parameters string         `json:"parameters"`
		ModelInfo  map[string]any `json:"model_info"`
	}
	if err := json.Unmarshal(bodyBytes, &showResp); err != nil {
		return 0, false
	}

	// 1. Check parameters string for num_ctx (loaded / configured context)
	if showResp.Parameters != "" {
		if matches := ollamaNumCtxRegex.FindStringSubmatch(showResp.Parameters); len(matches) > 1 {
			if n, err := strconv.Atoi(matches[1]); err == nil && n > 0 {
				return n, true
			}
		}
	}

	// 2. Check model_info for *.context_length (architectural limit)
	for k, v := range showResp.ModelInfo {
		if strings.HasSuffix(k, ".context_length") || k == "context_length" {
			switch val := v.(type) {
			case float64:
				if int(val) > 0 {
					return int(val), true
				}
			case int:
				if val > 0 {
					return val, true
				}
			}
		}
	}

	return 0, false
}

// fetchLMStudioContextLength queries LM Studio's /api/v0/models endpoint.
// A local model's loaded context can be smaller than its maximum; prefer the loaded value.
// Docs: https://lmstudio.ai/docs/api/rest
func fetchLMStudioContextLength(ctx context.Context, model, baseURL string) (int, bool) {
	endpoint := baseURL
	if endpoint == "" {
		endpoint = "http://127.0.0.1:1234"
	}
	endpoint = strings.TrimRight(endpoint, "/") + "/api/v0/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, false
	}

	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return 0, false
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, false
	}

	var modelsResp struct {
		Data []struct {
			ID                  string `json:"id"`
			MaxContextLength    int    `json:"max_context_length"`
			LoadedContextLength int    `json:"loaded_context_length"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &modelsResp); err != nil {
		return 0, false
	}

	modelLower := strings.ToLower(model)
	for _, m := range modelsResp.Data {
		if strings.ToLower(m.ID) == modelLower || modelLower == "model" {
			// Prefer loaded_context_length when reported (Amendment C)
			if m.LoadedContextLength > 0 {
				return m.LoadedContextLength, true
			}
			if m.MaxContextLength > 0 {
				return m.MaxContextLength, true
			}
		}
	}

	return 0, false
}
