package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"proxyctl/internal/client"
	"proxyctl/internal/model"
	"proxyctl/internal/version"
)

type Options struct {
	Controller string
	Token      string
	TokenFile  string
	Insecure   bool
	Stdout     io.Writer
	Stderr     io.Writer
}

func Run(args []string, opt Options) error {
	if opt.Stdout == nil {
		opt.Stdout = os.Stdout
	}
	if opt.Stderr == nil {
		opt.Stderr = os.Stderr
	}
	args, opt = parseGlobals(args, opt)
	if len(args) == 0 {
		return fmt.Errorf("usage: proxctl [--controller URL] [--token-file PATH] [--insecure] <command>")
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintln(opt.Stdout, version.Line("proxctl"))
		return nil
	}
	opt, err := applyEnv(opt)
	if err != nil {
		return err
	}
	c := client.New(opt.Controller, opt.Token, opt.Insecure)
	ctx := context.Background()
	switch args[0] {
	case "node":
		return nodeCmd(ctx, c, args[1:], opt)
	case "backend":
		return backendCmd(ctx, c, args[1:], opt)
	case "mapping":
		return mappingCmd(ctx, c, args[1:], opt)
	case "tunnel":
		return tunnelCmd(ctx, c, args[1:], opt)
	case "sni":
		return sniCmd(ctx, c, args[1:], opt)
	case "apply":
		return applyCmd(ctx, c, args[1:], opt)
	case "failback":
		return failbackCmd(ctx, c, args[1:], opt)
	case "token":
		return tokenCmd(ctx, c, args[1:], opt)
	case "whoami":
		return whoamiCmd(ctx, c, opt)
	case "help", "-h", "--help":
		fmt.Fprint(opt.Stdout, helpText)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

const helpText = `proxctl — proxyctl operator CLI

Commands:
  node add --name NAME [--public-ip IP]
  node list
  backend add --name NAME --node NAME --address IP
  backend list
  mapping add --node NAME --backend NAME --protocol TCP|UDP --public-port N --backend-port N
  mapping list
  mapping delete ID
  tunnel add --node NAME --backend NAME --type WIREGUARD|SSH_TUN ...
  tunnel list
  tunnel status ID
  sni add --node NAME --listen :443 --match HOST=BACKEND --default BACKEND
  apply --node NAME [--dry-run]
  failback --node NAME
  failback status --node NAME
  token add --name NAME --role operator|readonly|agent [--out-file PATH] [--output json|token]
  whoami
  version
`

func parseGlobals(args []string, opt Options) ([]string, Options) {
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--controller":
			i++
			if i < len(args) {
				opt.Controller = args[i]
			}
		case "--token":
			i++
			if i < len(args) {
				opt.Token = args[i]
			}
		case "--token-file":
			i++
			if i < len(args) {
				opt.TokenFile = args[i]
			}
		case "--insecure":
			opt.Insecure = true
		default:
			rest = append(rest, args[i])
		}
	}
	return rest, opt
}

func nodeCmd(ctx context.Context, c *client.Client, args []string, opt Options) error {
	if len(args) == 0 {
		return fmt.Errorf("node: expected add|list")
	}
	switch args[0] {
	case "list":
		out, err := c.ListNodes(ctx)
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	case "add":
		fs := parseFlags(args[1:])
		n := model.Node{Name: fs["name"], PublicIP: fs["public-ip"]}
		out, err := c.CreateNode(ctx, n)
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	default:
		return fmt.Errorf("node: unknown subcommand %q", args[0])
	}
}

func backendCmd(ctx context.Context, c *client.Client, args []string, opt Options) error {
	if len(args) == 0 {
		return fmt.Errorf("backend: expected add|list")
	}
	switch args[0] {
	case "list":
		out, err := c.ListBackends(ctx)
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	case "add":
		fs := parseFlags(args[1:])
		node, err := resolveNode(ctx, c, fs["node"])
		if err != nil {
			return err
		}
		b := model.Backend{Name: fs["name"], NodeID: node.ID, Address: fs["address"]}
		out, err := c.CreateBackend(ctx, b)
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	default:
		return fmt.Errorf("backend: unknown subcommand %q", args[0])
	}
}

func mappingCmd(ctx context.Context, c *client.Client, args []string, opt Options) error {
	if len(args) == 0 {
		return fmt.Errorf("mapping: expected add|list|delete")
	}
	switch args[0] {
	case "list":
		out, err := c.ListMappings(ctx)
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("mapping delete requires an id")
		}
		return c.DeleteMapping(ctx, args[1])
	case "add":
		fs := parseFlags(args[1:])
		node, err := resolveNode(ctx, c, fs["node"])
		if err != nil {
			return err
		}
		be, err := resolveBackend(ctx, c, fs["backend"])
		if err != nil {
			return err
		}
		pub, err := atoi(fs["public-port"])
		if err != nil {
			return fmt.Errorf("public-port: %w", err)
		}
		bp, err := atoi(fs["backend-port"])
		if err != nil {
			return fmt.Errorf("backend-port: %w", err)
		}
		m := model.PortMapping{
			NodeID:      node.ID,
			BackendID:   be.ID,
			Protocol:    model.Protocol(strings.ToUpper(fs["protocol"])),
			PublicPort:  pub,
			BackendPort: bp,
		}
		out, err := c.CreateMapping(ctx, m)
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	default:
		return fmt.Errorf("mapping: unknown subcommand %q", args[0])
	}
}

func tunnelCmd(ctx context.Context, c *client.Client, args []string, opt Options) error {
	if len(args) == 0 {
		return fmt.Errorf("tunnel: expected add|list|status")
	}
	switch args[0] {
	case "list":
		out, err := c.ListTunnels(ctx)
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	case "status":
		if len(args) < 2 {
			return fmt.Errorf("tunnel status requires an id")
		}
		out, err := c.TunnelStatus(ctx, args[1])
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	case "add":
		fs := parseFlags(args[1:])
		node, err := resolveNode(ctx, c, fs["node"])
		if err != nil {
			return err
		}
		be, err := resolveBackend(ctx, c, fs["backend"])
		if err != nil {
			return err
		}
		port, _ := atoi(fs["listen-port"])
		ka, _ := atoi(fs["keepalive"])
		pri, _ := atoi(fs["priority"])
		t := model.Tunnel{
			NodeID:              node.ID,
			BackendID:           be.ID,
			Type:                model.TunnelType(strings.ToUpper(fs["type"])),
			InterfaceName:       fs["interface"],
			LocalOverlayIP:      fs["local"],
			RemoteOverlayIP:     fs["remote"],
			ListenPort:          port,
			Endpoint:            fs["endpoint"],
			PersistentKeepalive: ka,
			Priority:            pri,
			PrivateKeyPath:      fs["private-key-path"],
			PublicKey:           fs["public-key"],
			ServiceName:         fs["service-name"],
		}
		if cidrs := fs["allowed-ips"]; cidrs != "" {
			t.AllowedIPs = splitComma(cidrs)
		}
		out, err := c.CreateTunnel(ctx, t)
		if err != nil {
			return err
		}
		return printJSON(opt.Stdout, out)
	default:
		return fmt.Errorf("tunnel: unknown subcommand %q", args[0])
	}
}

func sniCmd(ctx context.Context, c *client.Client, args []string, opt Options) error {
	if len(args) == 0 || args[0] != "add" {
		return fmt.Errorf("sni: expected add")
	}
	fs := parseFlags(args[1:])
	node, err := resolveNode(ctx, c, fs["node"])
	if err != nil {
		return err
	}
	route := model.SniRoute{NodeID: node.ID, Listen: fs["listen"]}
	if m := fs["match"]; m != "" {
		host, backend, ok := strings.Cut(m, "=")
		if !ok {
			return fmt.Errorf("--match HOST=BACKEND")
		}
		route.Matches = append(route.Matches, model.SniMatch{Match: host, Backend: backend})
	}
	if d := fs["default"]; d != "" {
		route.Matches = append(route.Matches, model.SniMatch{Default: true, Backend: d})
	}
	out, err := c.CreateSniRoute(ctx, route)
	if err != nil {
		return err
	}
	return printJSON(opt.Stdout, out)
}

func whoamiCmd(ctx context.Context, c *client.Client, opt Options) error {
	out, err := c.Whoami(ctx)
	if err != nil {
		return err
	}
	return printJSON(opt.Stdout, out)
}

func tokenCmd(ctx context.Context, c *client.Client, args []string, opt Options) error {
	if len(args) == 0 || args[0] != "add" {
		return fmt.Errorf("token: expected add")
	}
	fs := parseFlags(args[1:])
	role, err := model.ParseRole(fs["role"])
	if err != nil {
		return err
	}
	if strings.TrimSpace(fs["name"]) == "" {
		return fmt.Errorf("token add requires --name")
	}
	out, err := c.CreateToken(ctx, model.TokenCreateRequest{Name: fs["name"], Role: role})
	if err != nil {
		return err
	}
	if path := strings.TrimSpace(fs["out-file"]); path != "" {
		if err := writeSecretFile(path, out.Token); err != nil {
			return err
		}
		return printJSON(opt.Stdout, map[string]any{
			"id":         out.ID,
			"name":       out.Name,
			"role":       out.Role,
			"created_at": out.CreatedAt,
			"out_file":   path,
		})
	}
	switch strings.ToLower(strings.TrimSpace(fs["output"])) {
	case "", "json":
		return printJSON(opt.Stdout, out)
	case "token":
		_, err := fmt.Fprintln(opt.Stdout, out.Token)
		return err
	default:
		return fmt.Errorf("token add --output must be json or token")
	}
}

func writeSecretFile(path, plaintext string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("token --out-file: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("token --out-file chmod: %w", err)
	}
	if _, err := f.WriteString(plaintext + "\n"); err != nil {
		return fmt.Errorf("token --out-file: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("token --out-file sync: %w", err)
	}
	return nil
}

func applyCmd(ctx context.Context, c *client.Client, args []string, opt Options) error {
	fs := parseFlags(args)
	node, err := resolveNode(ctx, c, fs["node"])
	if err != nil {
		return err
	}
	dry := flagBool(args, "dry-run")
	res, err := c.Apply(ctx, string(node.ID), dry)
	if err != nil {
		return err
	}
	fmt.Fprint(opt.Stdout, res.Plan)
	if res.Message != "" && res.Message != "NO CHANGES" {
		fmt.Fprintln(opt.Stdout, res.Message)
	}
	return nil
}

func failbackCmd(ctx context.Context, c *client.Client, args []string, opt Options) error {
	if len(args) > 0 && args[0] == "status" {
		fs := parseFlags(args[1:])
		node, err := resolveNode(ctx, c, fs["node"])
		if err != nil {
			return err
		}
		ds, err := c.DesiredState(ctx, string(node.ID))
		if err != nil {
			return err
		}
		fmt.Fprintf(opt.Stdout, "node %s (%s) backends=%d tunnels=%d\n", ds.Node.Name, ds.Node.ID, len(ds.Backends), len(ds.Tunnels))
		return nil
	}
	fs := parseFlags(args)
	node, err := resolveNode(ctx, c, fs["node"])
	if err != nil {
		return err
	}
	return c.Failback(ctx, string(node.ID), fs["backend"])
}

func resolveNode(ctx context.Context, c *client.Client, name string) (*model.Node, error) {
	if name == "" {
		return nil, fmt.Errorf("--node is required")
	}
	nodes, err := c.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	for i := range nodes {
		if nodes[i].Name == name || string(nodes[i].ID) == name {
			return &nodes[i], nil
		}
	}
	return nil, fmt.Errorf("node %q not found", name)
}

func resolveBackend(ctx context.Context, c *client.Client, name string) (*model.Backend, error) {
	if name == "" {
		return nil, fmt.Errorf("--backend is required")
	}
	list, err := c.ListBackends(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Name == name || string(list[i].ID) == name {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("backend %q not found", name)
}

func parseFlags(args []string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue
		}
		key := strings.TrimPrefix(a, "--")
		if key == "dry-run" {
			out[key] = "true"
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			i++
			out[key] = args[i]
		} else {
			out[key] = "true"
		}
	}
	return out
}

func flagBool(args []string, name string) bool {
	for _, a := range args {
		if a == "--"+name {
			return true
		}
	}
	return false
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func atoi(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func applyEnv(opt Options) (Options, error) {
	if opt.Controller == "" {
		opt.Controller = envOr("PROXYCTL_CONTROLLER", "https://127.0.0.1:8443")
	}
	if opt.TokenFile == "" {
		opt.TokenFile = os.Getenv("PROXYCTL_TOKEN_FILE")
	}
	if opt.Token == "" && opt.TokenFile != "" {
		b, err := os.ReadFile(opt.TokenFile)
		if err != nil {
			return opt, fmt.Errorf("token-file: %w", err)
		}
		opt.Token = strings.TrimSpace(string(b))
	}
	if opt.Token == "" {
		opt.Token = os.Getenv("PROXYCTL_TOKEN")
	}
	if !opt.Insecure {
		opt.Insecure = envTruthy("PROXYCTL_INSECURE")
	}
	return opt, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envTruthy(k string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(k))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
