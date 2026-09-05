package api_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"transitforge/internal/cli"
	"transitforge/internal/client"
	"transitforge/internal/model"
)

func TestTokenRevocation(t *testing.T) {
	c, url, tokenFile := setup(t)
	ctx := context.Background()
	created, err := c.CreateToken(ctx, model.TokenCreateRequest{Name: "edge-token", Role: model.RoleAgent})
	if err != nil {
		t.Fatal(err)
	}
	agent := client.New(url, created.Token, true)
	if _, err := agent.Whoami(ctx); err != nil {
		t.Fatal(err)
	}
	checkStatus := func(err error, status int) {
		t.Helper()
		var coded *model.CodedError
		if !errors.As(err, &coded) || coded.HTTP != status {
			t.Fatalf("wanted HTTP %d, got %v", status, err)
		}
	}
	checkStatus(agent.RevokeToken(ctx, string(created.ID)), 403)
	readonly, err := c.CreateToken(ctx, model.TokenCreateRequest{Name: "reader", Role: model.RoleReadonly})
	if err != nil {
		t.Fatal(err)
	}
	checkStatus(client.New(url, readonly.Token, true).RevokeToken(ctx, string(created.ID)), 403)
	// Denied attempts must leave the target usable.
	if _, err := agent.Whoami(ctx); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	opt := cli.Options{Controller: url, TokenFile: tokenFile, Insecure: true, Stdout: &output, Stderr: &output}
	if err := cli.Run([]string{"token", "revoke", "--id", string(created.ID)}, opt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"revoked": true`) {
		t.Fatal(output.String())
	}
	_, err = agent.Whoami(ctx)
	checkStatus(err, 401)
	if err := c.RevokeToken(ctx, string(created.ID)); err != nil {
		t.Fatalf("repeat revoke: %v", err)
	}
	checkStatus(c.RevokeToken(ctx, "unknown"), 404)
	output.Reset()
	if err := cli.Run([]string{"token", "list"}, opt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"revoked": true`) || strings.Contains(output.String(), created.Token) || strings.Contains(output.String(), `"hash"`) {
		t.Fatal("token listing must retain revoked metadata without secrets")
	}
	events, err := c.ListEvents(ctx, "action=revoke")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Resource == "token" && event.ResourceID == string(created.ID) && event.Success {
			found = true
		}
		if strings.Contains(event.Detail, created.Token) {
			t.Fatal("audit leaked token")
		}
	}
	if !found {
		t.Fatal("missing successful revocation audit")
	}
	if err := cli.Run([]string{"token", "revoke"}, opt); err == nil {
		t.Fatal("missing ID accepted")
	}
}
