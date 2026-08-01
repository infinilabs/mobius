package bq

import (
	"context"
	"fmt"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
)

func QueryRow[T any](ctx context.Context, bq *Client, sql string, params []bigquery.QueryParameter) (*T, error) {
	q := bq.client.Query(sql)
	q.Parameters = params
	it, err := q.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("BQ query failed: %w", err)
	}
	var row T
	if err := it.Next(&row); err != nil {
		if err == iterator.Done {
			return nil, nil
		}
		return nil, fmt.Errorf("BQ read row: %w", err)
	}
	return &row, nil
}

func QueryRows[T any](ctx context.Context, bq *Client, sql string, params []bigquery.QueryParameter) ([]T, error) {
	q := bq.client.Query(sql)
	q.Parameters = params
	it, err := q.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("BQ query failed: %w", err)
	}
	var rows []T
	for {
		var row T
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return rows, fmt.Errorf("BQ read row: %w", err)
		}
		rows = append(rows, row)
	}
	if rows == nil {
		rows = []T{}
	}
	return rows, nil
}
