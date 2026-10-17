package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ModelInfo struct {
	ID      string `json:"id"`
	Created int64  `json:"created,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

// ListModels queries the provider's /models endpoint.
// It handles both standard OpenAI {"data": [...]} and bare array responses.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	endpoint := c.baseURL + "/models"

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating /models request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("GET %s returned %d: %s", endpoint, resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// 1. Try standard OpenAI format: {"data": [...]}
	var openAIResp struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &openAIResp); err == nil && len(openAIResp.Data) > 0 {
		var ids []string
		for _, m := range openAIResp.Data {
			cleanID := strings.TrimPrefix(m.ID, "models/")
			ids = append(ids, cleanID)
		}
		return ids, nil
	}

	// 2. Try bare array of objects: [{"id": "..."}]
	var bareObjs []ModelInfo
	if err := json.Unmarshal(bodyBytes, &bareObjs); err == nil && len(bareObjs) > 0 {
		var ids []string
		for _, m := range bareObjs {
			cleanID := strings.TrimPrefix(m.ID, "models/")
			ids = append(ids, cleanID)
		}
		return ids, nil
	}

	// 3. Try bare array of strings: ["id1", "id2"]
	var bareStrings []string
	if err := json.Unmarshal(bodyBytes, &bareStrings); err == nil && len(bareStrings) > 0 {
		var ids []string
		for _, id := range bareStrings {
			ids = append(ids, strings.TrimPrefix(id, "models/"))
		}
		return ids, nil
	}

	return nil, fmt.Errorf("unable to parse models response from %s", endpoint)
}
