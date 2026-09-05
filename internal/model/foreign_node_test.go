package model

import "testing"

func TestForeignNodeValidation(t *testing.T) {
	n := ForeignNodeInput{Name: " CZ-01 ", PublicAddress: " HOST.Example ", Country: " cz ", OverlayAddresses: []string{"10.0.0.1", "10.0.0.1", "fd00:0::1"}}
	if err := n.Validate(); err != nil {
		t.Fatal(err)
	}
	if n.Name != "cz-01" || n.Country != "CZ" || n.ProviderType != ProviderUnmanaged || len(n.OverlayAddresses) != 2 || n.OverlayAddresses[1] != "fd00::1" {
		t.Fatal("normalization failed")
	}
	for _, tc := range []struct {
		name  string
		apply func(*ForeignNodeInput)
	}{
		{"empty name", func(n *ForeignNodeInput) { n.Name = "" }},
		{"empty address", func(n *ForeignNodeInput) { n.PublicAddress = "" }},
		{"url", func(n *ForeignNodeInput) { n.PublicAddress = "https://example.net" }},
		{"port", func(n *ForeignNodeInput) { n.ManagementAddress = "example.net:443" }},
		{"country", func(n *ForeignNodeInput) { n.Country = "123" }},
		{"provider", func(n *ForeignNodeInput) { n.ProviderType = "OTHER" }},
		{"cidr", func(n *ForeignNodeInput) { n.OverlayAddresses = []string{"10.0.0.1/24"} }},
		{"multicast", func(n *ForeignNodeInput) { n.OverlayAddresses = []string{"ff02::1"} }},
		{"zone", func(n *ForeignNodeInput) { n.PublicAddress = "fe80::1%eth0" }},
		{"label", func(n *ForeignNodeInput) { n.Labels = map[string]string{"bad key": "value"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := ForeignNodeInput{Name: "node", PublicAddress: "example.net"}
			tc.apply(&n)
			if n.Validate() == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}
