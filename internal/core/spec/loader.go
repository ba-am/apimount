package spec

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// LoadSpec loads an OpenAPI spec from a local file path or remote URL.
func LoadSpec(pathOrURL string) ([]byte, error) {
	if isURL(pathOrURL) {
		return loadFromURL(pathOrURL)
	}
	return loadFromFile(pathOrURL)
}

func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

func loadFromFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not load spec from file %q: %w", path, err)
	}
	return data, nil
}

func loadFromURL(rawURL string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("could not load spec from URL %q: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not load spec from URL %q: HTTP %d", rawURL, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read spec response body: %w", err)
	}
	return data, nil
}
