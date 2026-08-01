package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mobius/internal/domain"
)

// domain.Conversation + message documents.
// Split from es.go (plan 6.5).

func (es *Client) IndexConversation(ctx context.Context, conv *domain.Conversation) error {
	doc := map[string]any{
		"id":         conv.ID,
		"title":      conv.Title,
		"project_id": conv.ProjectID,
		"turn_count": len(conv.Messages),
		"created_at": conv.CreatedAt,
		"updated_at": conv.UpdatedAt,
	}

	body, _ := json.Marshal(doc)
	res, err := es.client.Index(IdxConversations, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(conv.ID),
	)
	if err != nil {
		return fmt.Errorf("ES index conversation failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("ES index conversation error: %s", res.String())
	}
	return nil
}

func (es *Client) IndexMessage(ctx context.Context, convID string, msg *domain.Message, turnNumber int) error {
	doc := map[string]any{
		"id":              msg.ID,
		"conversation_id": convID,
		"role":            msg.Role,
		"content":         msg.Content,
		"timestamp":       msg.Timestamp,
		"turn_number":     turnNumber,
		"token_count":     msg.TokenCount,
		"summarized":      false,
	}
	if len(msg.Files) > 0 {
		doc["files"] = msg.Files
	}

	body, _ := json.Marshal(doc)
	res, err := es.client.Index(IdxMessages, bytes.NewReader(body),
		es.client.Index.WithContext(ctx),
		es.client.Index.WithDocumentID(msg.ID),
	)
	if err != nil {
		return fmt.Errorf("ES index message failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("ES index message error: %s", res.String())
	}
	return nil
}

func (es *Client) DeleteConversation(ctx context.Context, convID string) error {
	// Delete the conversation document
	res, err := es.client.Delete(IdxConversations, convID,
		es.client.Delete.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete conversation failed: %w", err)
	}
	defer res.Body.Close()

	// Delete all messages belonging to this conversation
	query := map[string]any{
		"query": map[string]any{
			"term": map[string]any{
				"conversation_id": convID,
			},
		},
	}
	qBody, _ := json.Marshal(query)
	delRes, err := es.client.DeleteByQuery(
		[]string{IdxMessages},
		bytes.NewReader(qBody),
		es.client.DeleteByQuery.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete messages failed: %w", err)
	}
	defer delRes.Body.Close()

	if delRes.IsError() {
		return fmt.Errorf("ES delete messages error: %s", delRes.String())
	}

	slog.Info("ES conversation deleted", "id", convID)
	return nil
}

func (es *Client) ListAllConversations(ctx context.Context) ([]domain.Conversation, error) {
	query := map[string]any{
		"query": map[string]any{"match_all": map[string]any{}},
		"sort":  []any{map[string]any{"updated_at": "desc"}},
		"size":  10000,
	}

	buf, _ := json.Marshal(query)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxConversations),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES list conversations failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("ES list conversations error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					ID        string  `json:"id"`
					Title     string  `json:"title"`
					ProjectID *string `json:"project_id"`
					CreatedAt int64   `json:"created_at"`
					UpdatedAt int64   `json:"updated_at"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode conversations failed: %w", err)
	}

	convs := make([]domain.Conversation, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		convs = append(convs, domain.Conversation{
			ID:        hit.Source.ID,
			Title:     hit.Source.Title,
			ProjectID: hit.Source.ProjectID,
			Messages:  []domain.Message{},
			CreatedAt: hit.Source.CreatedAt,
			UpdatedAt: hit.Source.UpdatedAt,
		})
	}
	return convs, nil
}

func (es *Client) ListMessages(ctx context.Context, convID string) ([]domain.Message, error) {
	query := map[string]any{
		"query": map[string]any{
			"term": map[string]any{"conversation_id": convID},
		},
		"sort": []any{map[string]any{"turn_number": "asc"}},
		"size": 10000,
	}

	buf, _ := json.Marshal(query)
	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(IdxMessages),
		es.client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, fmt.Errorf("ES list messages failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("ES list messages error: %s", res.String())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					ID         string           `json:"id"`
					Role       string           `json:"role"`
					Content    string           `json:"content"`
					Timestamp  int64            `json:"timestamp"`
					TokenCount int              `json:"token_count"`
					Files      []domain.FileRef `json:"files"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ES decode messages failed: %w", err)
	}

	msgs := make([]domain.Message, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		msgs = append(msgs, domain.Message{
			ID:         hit.Source.ID,
			Role:       hit.Source.Role,
			Content:    hit.Source.Content,
			Timestamp:  hit.Source.Timestamp,
			TokenCount: hit.Source.TokenCount,
			Files:      hit.Source.Files,
		})
	}
	return msgs, nil
}

func (es *Client) DeleteMessagesBeyond(ctx context.Context, convID string, keepCount int) error {
	query := map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"must": []any{
					map[string]any{"term": map[string]any{"conversation_id": convID}},
					map[string]any{"range": map[string]any{"turn_number": map[string]any{"gt": keepCount}}},
				},
			},
		},
	}

	buf, _ := json.Marshal(query)
	res, err := es.client.DeleteByQuery(
		[]string{IdxMessages},
		bytes.NewReader(buf),
		es.client.DeleteByQuery.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES delete messages beyond failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("ES delete messages beyond error: %s", res.String())
	}
	return nil
}

func (es *Client) UpdateConversationTitle(ctx context.Context, convID, title string) error {
	doc := map[string]any{
		"doc": map[string]any{
			"title":      title,
			"updated_at": timeNowMillis(),
		},
	}
	body, _ := json.Marshal(doc)
	res, err := es.client.Update(IdxConversations, convID, bytes.NewReader(body),
		es.client.Update.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("ES update conversation failed: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ES update conversation error: %s", res.Status())
	}
	return nil
}
