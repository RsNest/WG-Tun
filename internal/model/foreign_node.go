package model

import (
	"net/netip"
	"strings"
	"time"

	"transitforge/internal/validate"
)

type ProviderType string

const (
	ProviderUnmanaged ProviderType = "UNMANAGED"
	Provider3XUI      ProviderType = "3X_UI"
	ProviderSharX     ProviderType = "SHARX"
)

// ForeignNode is management inventory, independent of the existing Backend
// data-plane resources. ProviderType records software, not a verified connection.
type ForeignNode struct {
	ID ID `json:"id"`
	ForeignNodeInput
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ForeignNodeInput struct {
	Name              string            `json:"name"`
	PublicAddress     string            `json:"public_address"`
	ManagementAddress string            `json:"management_address"`
	Country           string            `json:"country"`
	OverlayAddresses  []string          `json:"overlay_addresses"`
	ProviderType      ProviderType      `json:"provider_type"`
	Labels            map[string]string `json:"labels"`
}

// Pointers distinguish omitted values from deliberate clears.
type ForeignNodePatch struct {
	Name              *string            `json:"name,omitempty"`
	PublicAddress     *string            `json:"public_address,omitempty"`
	ManagementAddress *string            `json:"management_address,omitempty"`
	Country           *string            `json:"country,omitempty"`
	OverlayAddresses  *[]string          `json:"overlay_addresses,omitempty"`
	ProviderType      *ProviderType      `json:"provider_type,omitempty"`
	Labels            *map[string]string `json:"labels,omitempty"`
}

func (p ForeignNodePatch) Apply(n *ForeignNodeInput) {
	if p.Name != nil {
		n.Name = *p.Name
	}
	if p.PublicAddress != nil {
		n.PublicAddress = *p.PublicAddress
	}
	if p.ManagementAddress != nil {
		n.ManagementAddress = *p.ManagementAddress
	}
	if p.Country != nil {
		n.Country = *p.Country
	}
	if p.OverlayAddresses != nil {
		n.OverlayAddresses = *p.OverlayAddresses
	}
	if p.ProviderType != nil {
		n.ProviderType = *p.ProviderType
	}
	if p.Labels != nil {
		n.Labels = *p.Labels
	}
}

func (n *ForeignNodeInput) Validate() error {
	n.Name = strings.ToLower(strings.TrimSpace(n.Name))
	if err := validate.NodeName(n.Name); err != nil {
		return wrapVal(err)
	}
	n.PublicAddress = strings.ToLower(strings.TrimSpace(n.PublicAddress))
	n.ManagementAddress = strings.ToLower(strings.TrimSpace(n.ManagementAddress))
	if err := foreignAddress(n.PublicAddress); err != nil {
		return Validation("public_address: " + err.Error())
	}
	if n.ManagementAddress != "" {
		if err := foreignAddress(n.ManagementAddress); err != nil {
			return Validation("management_address: " + err.Error())
		}
	}
	n.Country = strings.ToUpper(strings.TrimSpace(n.Country))
	if n.Country != "" && (len(n.Country) != 2 || n.Country[0] < 'A' || n.Country[0] > 'Z' || n.Country[1] < 'A' || n.Country[1] > 'Z') {
		return Validation("country must be a two-letter country code")
	}
	n.ProviderType = ProviderType(strings.ToUpper(strings.TrimSpace(string(n.ProviderType))))
	if n.ProviderType == "" {
		n.ProviderType = ProviderUnmanaged
	}
	switch n.ProviderType {
	case ProviderUnmanaged, Provider3XUI, ProviderSharX:
	default:
		return Validation("provider_type must be UNMANAGED, 3X_UI, or SHARX")
	}
	if len(n.OverlayAddresses) > 32 {
		return Validation("at most 32 overlay addresses are allowed")
	}
	addresses := []string{}
	seen := map[netip.Addr]bool{}
	for _, raw := range n.OverlayAddresses {
		ip, err := netip.ParseAddr(strings.TrimSpace(raw))
		if err != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
			return Validation("overlay_addresses must contain usable IP addresses without CIDR prefixes")
		}
		ip = ip.Unmap()
		if !seen[ip] {
			addresses = append(addresses, ip.String())
			seen[ip] = true
		}
	}
	n.OverlayAddresses = addresses
	if n.Labels == nil {
		n.Labels = map[string]string{}
	}
	if len(n.Labels) > 32 {
		return Validation("at most 32 labels are allowed")
	}
	for key, value := range n.Labels {
		if err := validate.TokenName(key); err != nil {
			return Validation("label keys must be alphanumeric with ._- (max 63)")
		}
		if len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
			return Validation("label values must be single-line text (max 256 bytes)")
		}
	}
	return nil
}

func foreignAddress(address string) error {
	if ip, err := netip.ParseAddr(address); err == nil {
		if ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
			return Validation("unusable IP address")
		}
		return nil
	}
	return validate.Hostname(address)
}
