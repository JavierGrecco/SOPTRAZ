package contracts

import (
	"bytes"
	"embed"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/JavierGrecco/SOPTRAZ/internal/strictjson"
)

//go:embed schemas/*.schema.json
var schemaFS embed.FS

const MaxDocumentBytes = 1 << 20

var schemaFiles = map[string]string{
	"manifest":     "schemas/module-manifest.schema.json",
	"action":       "schemas/action-definition.schema.json",
	"intervention": "schemas/intervention.schema.json",
	"event":        "schemas/intervention-event.schema.json",
	"checkpoint":   "schemas/audit-checkpoint.schema.json",
}

var (
	schemaOnce      sync.Once
	compiledSchemas map[string]*jsonschema.Schema
	schemaInitErr   error
)

type denyExternalSchemaLoader struct{}

func (denyExternalSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("referencia de esquema externa deshabilitada: %s", url)
}

func loadSchemas() {
	schemaOnce.Do(func() {
		compiledSchemas = make(map[string]*jsonschema.Schema, len(schemaFiles))
		for kind, path := range schemaFiles {
			raw, err := schemaFS.ReadFile(path)
			if err != nil {
				schemaInitErr = fmt.Errorf("leer esquema embebido %q: %w", path, err)
				return
			}

			document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				schemaInitErr = fmt.Errorf("analizar esquema embebido %q: %w", path, err)
				return
			}

			compiler := jsonschema.NewCompiler()
			compiler.DefaultDraft(jsonschema.Draft2020)
			compiler.AssertFormat()

			location := "urn:soptraz:schema:" + kind
			if err := compiler.AddResource(location, document); err != nil {
				schemaInitErr = fmt.Errorf("registrar esquema embebido %q: %w", path, err)
				return
			}
			schema, err := compiler.Compile(location)
			if err != nil {
				schemaInitErr = fmt.Errorf("compilar esquema embebido %q: %w", path, err)
				return
			}
			compiledSchemas[kind] = schema
		}
	})
}

// ValidateDocument validates one JSON document against an embedded, versioned
// JSON Schema. It never fetches remote schemas or follows external references.
func ValidateDocument(kind string, raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("el documento está vacío")
	}
	if len(raw) > MaxDocumentBytes {
		return fmt.Errorf("el documento supera el límite de %d bytes", MaxDocumentBytes)
	}

	loadSchemas()
	if schemaInitErr != nil {
		return fmt.Errorf("el validador de contratos no pudo inicializarse: %w", schemaInitErr)
	}
	schema, ok := compiledSchemas[kind]
	if !ok {
		return fmt.Errorf("tipo de contrato desconocido %q (válidos: manifest, action, intervention, event, checkpoint)", kind)
	}

	document, err := strictjson.Decode(raw)
	if err != nil {
		return fmt.Errorf("JSON inválido: %w", err)
	}

	if err := schema.Validate(document); err != nil {
		return fmt.Errorf("contrato %s inválido: %w", kind, err)
	}
	if kind == "action" {
		if err := validateActionParameterSchemas(document); err != nil {
			return fmt.Errorf("esquema de parámetros/respuesta inválido: %w", err)
		}
	}
	return nil
}

func validateActionParameterSchemas(document any) error {
	action, ok := document.(map[string]any)
	if !ok {
		return fmt.Errorf("la definición de acción debe ser un objeto")
	}

	for _, field := range []string{"inputSchema", "outputSchema"} {
		schemaDocument, ok := action[field].(map[string]any)
		if !ok {
			return fmt.Errorf("%s debe ser un objeto JSON Schema", field)
		}

		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		compiler.AssertFormat()
		compiler.UseLoader(denyExternalSchemaLoader{})

		location := "urn:soptraz:action:" + field
		if err := compiler.AddResource(location, schemaDocument); err != nil {
			return fmt.Errorf("registrar %s: %w", field, err)
		}
		if _, err := compiler.Compile(location); err != nil {
			return fmt.Errorf("compilar %s: %w", field, err)
		}
	}
	return nil
}
