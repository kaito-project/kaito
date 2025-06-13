package ragengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(config RAGEngineConfig) *Client {
	return &Client{
		baseURL:    config.URL,
		httpClient: &http.Client{},
	}
}

// IndexDocuments adds documents to an index or creates a new index.
func (c *Client) IndexDocuments(req *IndexDocumentRequest) ([]*RAGDocument, error) {
	endpoint := c.baseURL + "/index"
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("index failed: %s", resp.Status)
	}
	var docs []*RAGDocument
	if err := json.NewDecoder(resp.Body).Decode(&docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (c *Client) Query(req *QueryRequest) (*QueryResponse, error) {
	endpoint := c.baseURL + "/query"
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query failed: %s", resp.Status)
	}
	var qr QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return nil, err
	}
	return &qr, nil
}

// ListIndexes returns all index names
func (c *Client) ListIndexes() ([]string, error) {
	endpoint := c.baseURL + "/indexes"
	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list indexes failed: %s", resp.Status)
	}
	var indexes []string
	if err := json.NewDecoder(resp.Body).Decode(&indexes); err != nil {
		return nil, err
	}
	return indexes, nil
}

func (c *Client) ListDocumentsInIndex(indexName string, limit, offset, maxTextLength int, metadataFilter map[string]interface{}) (*ListDocumentsResponse, error) {
	endpoint := fmt.Sprintf("%s/indexes/%s/documents?limit=%d&offset=%d&max_text_length=%d", c.baseURL, url.PathEscape(indexName), limit, offset, maxTextLength)
	if metadataFilter != nil {
		filterBytes, _ := json.Marshal(metadataFilter)
		endpoint += "&metadata_filter=" + url.QueryEscape(string(filterBytes))
	}
	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list documents failed: %s", resp.Status)
	}
	var out ListDocumentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateDocumentsInIndex(indexName string, req *UpdateDocumentRequest) (*UpdateDocumentResponse, error) {
	endpoint := fmt.Sprintf("%s/indexes/%s/documents", c.baseURL, url.PathEscape(indexName))
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update documents failed: %s", resp.Status)
	}
	var out UpdateDocumentResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteDocumentsInIndex(indexName string, req *DeleteDocumentRequest) (*DeleteDocumentResponse, error) {
	endpoint := fmt.Sprintf("%s/indexes/%s/documents/delete", c.baseURL, url.PathEscape(indexName))
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("delete documents failed: %s", resp.Status)
	}
	var out DeleteDocumentResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PersistIndex persists index data to disk
func (c *Client) PersistIndex(indexName, path string) error {
	endpoint := fmt.Sprintf("%s/persist/%s?path=%s", c.baseURL, url.PathEscape(indexName), url.QueryEscape(path))
	resp, err := c.httpClient.Post(endpoint, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("persist index failed: %s: %s", resp.Status, string(body))
	}
	return nil
}

// LoadIndex loads index data from disk
func (c *Client) LoadIndex(indexName, path string, overwrite bool) error {
	endpoint := fmt.Sprintf("%s/load/%s?path=%s&overwrite=%v", c.baseURL, url.PathEscape(indexName), url.QueryEscape(path), overwrite)
	resp, err := c.httpClient.Post(endpoint, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("load index failed: %s: %s", resp.Status, string(body))
	}
	return nil
}

// DeleteIndex deletes an entire index
func (c *Client) DeleteIndex(indexName string) error {
	endpoint := fmt.Sprintf("%s/indexes/%s", c.baseURL, url.PathEscape(indexName))
	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete index failed: %s: %s", resp.Status, string(body))
	}
	return nil
}
