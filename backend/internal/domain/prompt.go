package domain

type Prompt struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}
