package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/elastic/go-elasticsearch/v9"
)

const (
	IdxConversations = "mobius_conversations"
	IdxMessages      = "mobius_messages"
)

type ESClient struct {
	client *elasticsearch.Client
}

func NewESClient(url string) (*ESClient, error) {
	if url == "" {
		url = "http://localhost:9200"
	}

	cfg := elasticsearch.Config{
		Addresses: []string{url},
	}
	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create ES client: %w", err)
	}

	info, err := client.Info()
	if err != nil {
		return nil, fmt.Errorf("ES connection failed: %w", err)
	}
	defer info.Body.Close()

	if info.IsError() {
		return nil, fmt.Errorf("ES returned error: %s", info.String())
	}

	slog.Info("Elasticsearch connected", "url", url)
	return &ESClient{client: client}, nil
}

func (es *ESClient) IndexConversation(ctx context.Context, conv *Conversation) error {
	doc := map[string]any{
		"id":         conv.ID,
		"title":      conv.Title,
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

func (es *ESClient) IndexMessage(ctx context.Context, convID string, msg *Message, turnNumber int) error {
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

func (es *ESClient) DeleteConversation(ctx context.Context, convID string) error {
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

func (es *ESClient) UpdateConversationTitle(ctx context.Context, convID, title string) error {
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
	return nil
}

func timeNowMillis() int64 {
	return time.Now().UnixMilli()
}
