package api_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"transitforge/internal/cli"
	"transitforge/internal/client"
	"transitforge/internal/model"
)

func TestForeignNodeAPIAndCLI(t *testing.T) {
	c, endpoint, tokenFile := setup(t)
	ctx := context.Background()
	input := model.ForeignNodeInput{Name: "cz-01", PublicAddress: "203.0.113.20", ManagementAddress: "manage.example.net", Country: "cz", ProviderType: model.Provider3XUI, OverlayAddresses: []string{"10.200.1.2"}, Labels: map[string]string{"region": "europe"}}
	n, err := c.CreateForeignNode(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if n.ID == "" || n.Country != "CZ" || n.CreatedAt.IsZero() {
		t.Fatal("create response incomplete")
	}
	check := func(err error, status int) {
		t.Helper()
		var ce *model.CodedError
		if !errors.As(err, &ce) || ce.HTTP != status {
			t.Fatalf("want %d, got %v", status, err)
		}
	}
	_, err = c.CreateForeignNode(ctx, input)
	check(err, 409)
	empty := ""
	overlays := []string{}
	updated, err := c.PatchForeignNode(ctx, string(n.ID), model.ForeignNodePatch{ManagementAddress: &empty, OverlayAddresses: &overlays})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ManagementAddress != "" || len(updated.OverlayAddresses) != 0 || updated.Labels["region"] != "europe" || !updated.CreatedAt.Equal(n.CreatedAt) {
		t.Fatal("patch clear/omission semantics broken")
	}
	bad := "https://invalid.example"
	_, err = c.PatchForeignNode(ctx, string(n.ID), model.ForeignNodePatch{PublicAddress: &bad})
	check(err, 400)
	check(c.Do(ctx, http.MethodPatch, "/api/v1/foreign-nodes/"+string(n.ID), map[string]string{"id": "forged"}, nil), 400)
	_, err = c.GetForeignNode(ctx, "missing")
	check(err, 404)
	for _, role := range []model.Role{model.RoleReadonly, model.RoleAgent} {
		token, err := c.CreateToken(ctx, model.TokenCreateRequest{Name: string(role), Role: role})
		if err != nil {
			t.Fatal(err)
		}
		other := client.New(endpoint, token.Token, true)
		_, err = other.ListForeignNodes(ctx)
		if role == model.RoleAgent {
			check(err, 403)
		} else if err != nil {
			t.Fatal(err)
		}
		_, err = other.GetForeignNode(ctx, string(n.ID))
		if role == model.RoleAgent {
			check(err, 403)
		} else if err != nil {
			t.Fatal(err)
		}
		_, err = other.CreateForeignNode(ctx, input)
		check(err, 403)
		_, err = other.PatchForeignNode(ctx, string(n.ID), model.ForeignNodePatch{Country: &empty})
		check(err, 403)
	}
	var output strings.Builder
	opt := cli.Options{Controller: endpoint, TokenFile: tokenFile, Insecure: true, Stdout: &output, Stderr: &output}
	if err := cli.Run([]string{"foreign-node", "add", "--name", "de-01", "--public-address", "host.example", "--provider", "SHARX"}, opt); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"foreign-node", "list"}, {"foreign-node", "get", string(n.ID)}, {"foreign-node", "update", string(n.ID), "--country", "DE"}} {
		output.Reset()
		if err := cli.Run(args, opt); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "cz-01") {
			t.Fatal("missing CLI output")
		}
	}
	events, err := c.ListEvents(ctx, "action=create")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Resource == "foreign-node" && e.ResourceID == string(n.ID) && e.Success {
			found = true
		}
	}
	if !found {
		t.Fatal("missing creation audit")
	}
}
