package project

type Info struct {
	Path           string   `json:"path"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	KindLabel      string   `json:"kindLabel"`
	DocumentRoot   string   `json:"documentRoot"`
	PHPRequirement string   `json:"phpRequirement,omitempty"`
	Ready          bool     `json:"ready"`
	Warnings       []string `json:"warnings"`
}
