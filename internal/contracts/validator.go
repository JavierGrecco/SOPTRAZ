package contracts

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schemas/*.schema.json
var schemaFS embed.FS

const MaxDocumentBytes = 1 << 20

var schemaFiles = map[string]string{
	"manifest":     "schemas/module-manifest.schema.json",
	"action":       "schemas/action-definition.schema.json",
	"intervention": "schemas/intervention.schema.json",
	"event":        "schemas/intervention-event.schema.json",
}

var (
	schemaOnce      sync.Once
	compiledSchemas map[string]*jsonschema.Schema
	schemaInitErr   error
)

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
		return fmt.Errorf("tipo de contrato desconocido %q (válidos: manifest, action, intervention, event)", kind)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		if err == io.EOF {
			return fmt.Errorf("el documento está vacío")
		}
		return fmt.Errorf("JSON inválido: %w", err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("el archivo contiene más de un documento JSON")
		}
		return fmt.Errorf("contenido adicional después del documento JSON: %w", err)
	}

	if err := schema.Validate(document); err != nil {
		return fmt.Errorf("contrato %s inválido: %w", kind, err)
	}
	return nil
}
