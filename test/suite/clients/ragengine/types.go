package ragengine

type RAGEngineConfig struct {
	URL string `json:"url"`
}

type RAGDocument struct {
	DocID       string         `json:"doc_id"`
	Text        string         `json:"text"`
	Metadata    map[string]any `json:"metadata"`
	HashValue   string         `json:"hash_value"`
	IsTruncated bool           `json:"is_truncated"`
}

type IndexDocumentRequest struct {
	IndexName string         `json:"index_name"`
	Documents []*RAGDocument `json:"documents"`
}

type QueryRequest struct {
	IndexName    string            `json:"index_name"`
	Query        string            `json:"query"`
	TopK         int               `json:"top_k,omitempty"`
	LLMParams    QueryLLMParams    `json:"llm_params,omitempty"`
	RerankParams QueryRerankParams `json:"rerank_params,omitempty"`
}

type QueryLLMParams struct {
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
}

type QueryRerankParams struct {
	TopN int `json:"top_n,omitempty"`
}

type SourceNode struct {
	DocID    string         `json:"doc_id"`
	NodeID   string         `json:"node_id"`
	Text     string         `json:"text"`
	Score    float64        `json:"score"`
	Metadata map[string]any `json:"metadata"`
}

type QueryResponse struct {
	Response    string         `json:"response"`
	SourceNodes []SourceNode   `json:"source_nodes"`
	Metadata    map[string]any `json:"metadata"`
}

type ListDocumentsResponse struct {
	Documents []*RAGDocument `json:"documents"`
	Count     int            `json:"count"`
}

type UpdateDocumentRequest struct {
	Documents []*RAGDocument `json:"documents"`
}

type UpdateDocumentResponse struct {
	UpdatedDocuments   []*RAGDocument `json:"updated_documents"`
	UnchangedDocuments []*RAGDocument `json:"unchanged_documents"`
	NotFoundDocuments  []*RAGDocument `json:"not_found_documents"`
}

type DeleteDocumentRequest struct {
	DocIDs []string `json:"doc_ids"`
}

type DeleteDocumentResponse struct {
	DeletedDocIDs  []string `json:"deleted_doc_ids"`
	NotFoundDocIDs []string `json:"not_found_doc_ids"`
}
