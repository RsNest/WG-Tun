package client

import (
	"context"
	"net/http"
	"net/url"

	"transitforge/internal/model"
)

func (c *Client) ListForeignNodes(ctx context.Context) ([]model.ForeignNode, error) {
	var out []model.ForeignNode
	err := c.Do(ctx, http.MethodGet, "/api/v1/foreign-nodes", nil, &out)
	return out, err
}

func (c *Client) GetForeignNode(ctx context.Context, id string) (*model.ForeignNode, error) {
	var out model.ForeignNode
	err := c.Do(ctx, http.MethodGet, "/api/v1/foreign-nodes/"+url.PathEscape(id), nil, &out)
	return &out, err
}

func (c *Client) CreateForeignNode(ctx context.Context, n model.ForeignNodeInput) (*model.ForeignNode, error) {
	var out model.ForeignNode
	err := c.Do(ctx, http.MethodPost, "/api/v1/foreign-nodes", n, &out)
	return &out, err
}

func (c *Client) PatchForeignNode(ctx context.Context, id string, patch model.ForeignNodePatch) (*model.ForeignNode, error) {
	var out model.ForeignNode
	err := c.Do(ctx, http.MethodPatch, "/api/v1/foreign-nodes/"+url.PathEscape(id), patch, &out)
	return &out, err
}
