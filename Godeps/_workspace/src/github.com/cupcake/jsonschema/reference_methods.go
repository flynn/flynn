package jsonschema

// A temporary interface for use until code is written to access
// a validator's EmbeddedSchemas field with reflection.
type SchemaEmbedder interface {
	LinkEmbedded() map[string]*Schema
}

func (a *additionalProperties) LinkEmbedded() map[string]*Schema {
	return a.EmbeddedSchemas
}

func (a *allOf) LinkEmbedded() map[string]*Schema {
	return a.EmbeddedSchemas
}

func (a *anyOf) LinkEmbedded() map[string]*Schema {
	return a.EmbeddedSchemas
}

func (a *additionalItems) LinkEmbedded() map[string]*Schema {
	return a.EmbeddedSchemas
}

func (a *dependencies) LinkEmbedded() map[string]*Schema {
	return a.EmbeddedSchemas
}

func (a *items) LinkEmbedded() map[string]*Schema {
	return a.EmbeddedSchemas
}

func (a *not) LinkEmbedded() map[string]*Schema {
	return a.EmbeddedSchemas
}

func (a *patternProperties) LinkEmbedded() map[string]*Schema {
	return a.EmbeddedSchemas
}

func (a *properties) LinkEmbedded() map[string]*Schema {
	return a.EmbeddedSchemas
}

func (a *oneOf) LinkEmbedded() map[string]*Schema {
	return a.EmbeddedSchemas
}

func (a *other) LinkEmbedded() map[string]*Schema {
	return a.EmbeddedSchemas
}
