package domain

// ProductSchema is the versioned, persisted definition of a product's fixed
// attributes and variation axes. Option identity never depends on its label.
type ProductSchema struct {
	Version    int                          `json:"version"`
	Type       string                       `json:"type"`
	Attributes []ProductAttributeDefinition `json:"attributes"`
}

type ProductAttributeDefinition struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Role       string                   `json:"role,omitempty"`
	Source     string                   `json:"source"`
	RegistryID *int64                   `json:"registryId,omitempty"`
	Values     []ProductAttributeOption `json:"values"`
}

type ProductAttributeOption struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	HexCode string `json:"hexCode,omitempty"`
}
