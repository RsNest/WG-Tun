package store

import (
	"context"
	"encoding/json"

	"transitforge/internal/model"
)

const foreignNodeColumns = `id,name,public_address,management_address,country,overlay_addresses_json,provider_type,labels_json,created_at,updated_at`

func (s *SQLite) CreateForeignNode(ctx context.Context, n *model.ForeignNode) error {
	if err := n.Validate(); err != nil {
		return err
	}
	overlays, _ := json.Marshal(n.OverlayAddresses)
	labels, _ := json.Marshal(n.Labels)
	_, err := s.db.ExecContext(ctx, `INSERT INTO foreign_nodes (`+foreignNodeColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		n.ID, n.Name, n.PublicAddress, n.ManagementAddress, n.Country, string(overlays), n.ProviderType, string(labels), nowRFC3339(n.CreatedAt), nowRFC3339(n.UpdatedAt))
	if isUnique(err) {
		return model.ErrConflict("foreign node name already exists")
	}
	return wrap("CreateForeignNode", err)
}

func (s *SQLite) GetForeignNode(ctx context.Context, id model.ID) (*model.ForeignNode, error) {
	return scanForeignNode(s.db.QueryRowContext(ctx, `SELECT `+foreignNodeColumns+` FROM foreign_nodes WHERE id=?`, id))
}

func (s *SQLite) ListForeignNodes(ctx context.Context) ([]model.ForeignNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+foreignNodeColumns+` FROM foreign_nodes ORDER BY name,id`)
	if err != nil {
		return nil, wrap("ListForeignNodes", err)
	}
	defer rows.Close()
	out := []model.ForeignNode{}
	for rows.Next() {
		n, err := scanForeignNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func (s *SQLite) UpdateForeignNode(ctx context.Context, n *model.ForeignNode) error {
	if err := n.Validate(); err != nil {
		return err
	}
	overlays, _ := json.Marshal(n.OverlayAddresses)
	labels, _ := json.Marshal(n.Labels)
	result, err := s.db.ExecContext(ctx, `UPDATE foreign_nodes SET name=?,public_address=?,management_address=?,country=?,overlay_addresses_json=?,provider_type=?,labels_json=?,updated_at=? WHERE id=?`,
		n.Name, n.PublicAddress, n.ManagementAddress, n.Country, string(overlays), n.ProviderType, string(labels), nowRFC3339(n.UpdatedAt), n.ID)
	if isUnique(err) {
		return model.ErrConflict("foreign node name already exists")
	}
	if err != nil {
		return wrap("UpdateForeignNode", err)
	}
	num, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if num == 0 {
		return ErrNotFound
	}
	return nil
}

func scanForeignNode(row scanner) (*model.ForeignNode, error) {
	var n model.ForeignNode
	var overlays, labels, created, updated string
	if err := row.Scan(&n.ID, &n.Name, &n.PublicAddress, &n.ManagementAddress, &n.Country, &overlays, &n.ProviderType, &labels, &created, &updated); err != nil {
		return nil, wrap("GetForeignNode", err)
	}
	if err := json.Unmarshal([]byte(overlays), &n.OverlayAddresses); err != nil {
		return nil, wrap("foreign node overlays", err)
	}
	if err := json.Unmarshal([]byte(labels), &n.Labels); err != nil {
		return nil, wrap("foreign node labels", err)
	}
	n.CreatedAt, n.UpdatedAt = parseTime(created), parseTime(updated)
	return &n, nil
}
