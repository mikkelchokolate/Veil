package schema

// FieldType enumerates the primitive input kinds a plugin can request in the UI.
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldPassword FieldType = "password"
	FieldSelect   FieldType = "select"
	FieldCheckbox FieldType = "checkbox"
	FieldNumber   FieldType = "number"
)

// FieldOption is one choice for a select field.
type FieldOption struct {
	Label      string            `json:"label"`
	Value      string            `json:"value"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// FieldSchema describes one dynamic protocol field in the Panel.
type FieldSchema struct {
	Key               string        `json:"key"`
	Label             string        `json:"label"`
	Type              FieldType     `json:"type"`
	Default           any           `json:"default,omitempty"`
	Options           []FieldOption `json:"options,omitempty"`
	Placeholder       string        `json:"placeholder,omitempty"`
	GenerateAction    string        `json:"generateAction,omitempty"`
	GenerateActionField string      `json:"generateActionField,omitempty"`
	Required          bool          `json:"required,omitempty"`
	Scope             string        `json:"scope,omitempty"` // "inbound" | "settings"
}
