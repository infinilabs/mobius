package domain

type Message struct {
	ID         string    `json:"id"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	Timestamp  int64     `json:"timestamp"`
	TokenCount int       `json:"token_count,omitempty"`
	Files      []FileRef `json:"files,omitempty"`
}

type FileRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	GCSURI   string `json:"gcs_uri,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	ProjectID *string   `json:"project_id,omitempty"`
	Messages  []Message `json:"messages"`
	CreatedAt int64     `json:"created_at"`
	UpdatedAt int64     `json:"updated_at"`
}

type ConversationSummary struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	ProjectID *string `json:"project_id,omitempty"`
	UpdatedAt int64   `json:"updated_at"`
}
