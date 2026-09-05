package cli

import (
	"context"
	"fmt"
	"strings"

	"transitforge/internal/client"
	"transitforge/internal/model"
)

func foreignNodeCmd(ctx context.Context, c *client.Client, args []string, opt Options) error {
	if len(args) == 0 {
		return fmt.Errorf("foreign-node: expected add, list, get, or update")
	}
	switch args[0] {
	case "list":
		out, err := c.ListForeignNodes(ctx)
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	case "get":
		if len(args) != 2 {
			return fmt.Errorf("foreign-node get requires ID")
		}
		out, err := c.GetForeignNode(ctx, args[1])
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	case "add":
		fs := parseFlags(args[1:])
		n := model.ForeignNodeInput{Name: fs["name"], PublicAddress: fs["public-address"], ManagementAddress: fs["management-address"], Country: fs["country"], ProviderType: model.ProviderType(fs["provider"]), OverlayAddresses: foreignOverlays(fs["overlay-addresses"])}
		if err := n.Validate(); err != nil {
			return err
		}
		out, err := c.CreateForeignNode(ctx, n)
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	case "update":
		if len(args) < 3 || strings.HasPrefix(args[1], "--") {
			return fmt.Errorf("foreign-node update requires ID and fields")
		}
		fs := parseFlags(args[2:])
		var patch model.ForeignNodePatch
		if v, ok := fs["name"]; ok {
			patch.Name = &v
		}
		if v, ok := fs["public-address"]; ok {
			patch.PublicAddress = &v
		}
		if v, ok := fs["management-address"]; ok {
			patch.ManagementAddress = &v
		}
		if v, ok := fs["country"]; ok {
			patch.Country = &v
		}
		if v, ok := fs["provider"]; ok {
			p := model.ProviderType(v)
			patch.ProviderType = &p
		}
		if v, ok := fs["overlay-addresses"]; ok {
			overlays := foreignOverlays(v)
			patch.OverlayAddresses = &overlays
		}
		out, err := c.PatchForeignNode(ctx, args[1], patch)
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	default:
		return fmt.Errorf("foreign-node: expected add, list, get, or update")
	}
}

func foreignOverlays(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}
