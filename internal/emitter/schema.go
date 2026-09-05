package emitter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/wheelsmif/geovisor/internal/tir"
)

// JSONSchemaDialect is the schema dialect used by emitter payloads.
const JSONSchemaDialect = "https://json-schema.org/draft/2020-12/schema"

type schemaProperty struct {
	name   string
	schema *jsonSchema
}

type jsonSchema struct {
	types                []string
	description          string
	enum                 []string
	enumNullable         bool
	items                *jsonSchema
	properties           []schemaProperty
	required             []string
	additionalProperties *bool
}

func (schema *jsonSchema) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	first := true
	write := func(name string, value any) error {
		if !first {
			buffer.WriteByte(',')
		}
		first = false
		nameJSON, err := json.Marshal(name)
		if err != nil {
			return err
		}
		valueJSON, err := json.Marshal(value)
		if err != nil {
			return err
		}
		buffer.Write(nameJSON)
		buffer.WriteByte(':')
		buffer.Write(valueJSON)
		return nil
	}

	if len(schema.types) == 1 {
		if err := write("type", schema.types[0]); err != nil {
			return nil, err
		}
	} else {
		if err := write("type", schema.types); err != nil {
			return nil, err
		}
	}
	if schema.description != "" {
		if err := write("description", schema.description); err != nil {
			return nil, err
		}
	}
	if len(schema.enum) != 0 {
		values := make([]any, 0, len(schema.enum)+1)
		for _, value := range schema.enum {
			values = append(values, value)
		}
		if schema.enumNullable {
			values = append(values, nil)
		}
		if err := write("enum", values); err != nil {
			return nil, err
		}
	}
	if schema.items != nil {
		if err := write("items", schema.items); err != nil {
			return nil, err
		}
	}
	if schema.properties != nil {
		if !first {
			buffer.WriteByte(',')
		}
		first = false
		nameJSON, _ := json.Marshal("properties")
		buffer.Write(nameJSON)
		buffer.WriteString(":{")
		for i, property := range schema.properties {
			if i != 0 {
				buffer.WriteByte(',')
			}
			propertyName, err := json.Marshal(property.name)
			if err != nil {
				return nil, err
			}
			propertySchema, err := json.Marshal(property.schema)
			if err != nil {
				return nil, err
			}
			buffer.Write(propertyName)
			buffer.WriteByte(':')
			buffer.Write(propertySchema)
		}
		buffer.WriteByte('}')
	}
	if len(schema.required) != 0 {
		if err := write("required", schema.required); err != nil {
			return nil, err
		}
	}
	if schema.additionalProperties != nil {
		if err := write("additionalProperties", *schema.additionalProperties); err != nil {
			return nil, err
		}
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func toolInputSchema(
	ctx context.Context,
	format Format,
	tool *tir.Tool,
	strict bool,
) (*jsonSchema, error) {
	denyAdditional := false
	root := &jsonSchema{
		types:                []string{"object"},
		properties:           make([]schemaProperty, 0, len(tool.Parameters)),
		required:             make([]string, 0, len(tool.Parameters)),
		additionalProperties: &denyAdditional,
	}
	seen := make(map[string]struct{}, len(tool.Parameters))
	for i := range tool.Parameters {
		if err := canceled(ctx, format); err != nil {
			return nil, err
		}
		parameter := &tool.Parameters[i]
		field := fmt.Sprintf("tools[%q].parameters[%d]", tool.ID, i)
		if _, exists := seen[parameter.Name]; exists {
			return nil, failure(format, CodeUnsupportedShape, field+".name", "duplicate property name")
		}
		seen[parameter.Name] = struct{}{}
		converted, err := convertShape(
			ctx,
			format,
			parameter.Type,
			parameter.Enum,
			parameter.Items,
			parameter.Properties,
			parameter.Description,
			strict,
			strict && !parameter.Required,
			field,
		)
		if err != nil {
			return nil, err
		}
		root.properties = append(root.properties, schemaProperty{name: parameter.Name, schema: converted})
		if strict || parameter.Required {
			root.required = append(root.required, parameter.Name)
		}
	}
	return root, nil
}

func convertParameterShape(
	ctx context.Context,
	format Format,
	shape *tir.ParameterShape,
	description string,
	strict bool,
	nullable bool,
	field string,
) (*jsonSchema, error) {
	return convertShape(
		ctx,
		format,
		shape.Type,
		shape.Enum,
		shape.Items,
		shape.Properties,
		description,
		strict,
		nullable,
		field,
	)
}

func convertShape(
	ctx context.Context,
	format Format,
	valueType tir.ValueType,
	enum []string,
	items *tir.ParameterShape,
	properties []tir.ParameterProperty,
	description string,
	strict bool,
	nullable bool,
	field string,
) (*jsonSchema, error) {
	if err := canceled(ctx, format); err != nil {
		return nil, err
	}
	converted := &jsonSchema{
		types:       []string{string(valueType)},
		description: description,
		enum:        append([]string(nil), enum...),
	}
	if nullable {
		converted.types = append(converted.types, "null")
		converted.enumNullable = len(enum) != 0
	}

	switch valueType {
	case tir.ValueObject:
		if items != nil {
			return nil, failure(format, CodeUnsupportedShape, field+".items", "object schemas cannot define items")
		}
		if len(enum) != 0 {
			return nil, failure(format, CodeUnsupportedShape, field+".enum", "object schemas cannot use string enums")
		}
		denyAdditional := false
		converted.additionalProperties = &denyAdditional
		converted.properties = make([]schemaProperty, 0, len(properties))
		converted.required = make([]string, 0, len(properties))
		seen := make(map[string]struct{}, len(properties))
		for i := range properties {
			property := &properties[i]
			propertyField := fmt.Sprintf("%s.properties[%d]", field, i)
			if _, exists := seen[property.Name]; exists {
				return nil, failure(format, CodeUnsupportedShape, propertyField+".name", "duplicate property name")
			}
			seen[property.Name] = struct{}{}
			child, err := convertParameterShape(
				ctx,
				format,
				&property.Shape,
				property.Description,
				strict,
				strict && !property.Required,
				propertyField+".shape",
			)
			if err != nil {
				return nil, err
			}
			if strict || property.Required {
				converted.required = append(converted.required, property.Name)
			}
			converted.properties = append(converted.properties, schemaProperty{
				name:   property.Name,
				schema: child,
			})
		}
	case tir.ValueArray:
		if items == nil {
			return nil, failure(format, CodeUnsupportedShape, field+".items", "array schemas require items")
		}
		if len(properties) != 0 {
			return nil, failure(format, CodeUnsupportedShape, field+".properties", "array schemas cannot define properties")
		}
		if len(enum) != 0 {
			return nil, failure(format, CodeUnsupportedShape, field+".enum", "array schemas cannot use string enums")
		}
		child, err := convertParameterShape(ctx, format, items, "", strict, false, field+".items")
		if err != nil {
			return nil, err
		}
		converted.items = child
	case tir.ValueString:
		if items != nil || len(properties) != 0 {
			return nil, failure(format, CodeUnsupportedShape, field, "string schemas cannot define items or properties")
		}
	case tir.ValueNumber, tir.ValueInteger, tir.ValueBoolean:
		if items != nil || len(properties) != 0 || len(enum) != 0 {
			return nil, failure(format, CodeUnsupportedShape, field, "primitive schema contains incompatible items, properties, or string enum")
		}
	default:
		return nil, failure(format, CodeUnsupportedShape, field+".type", "unsupported value type")
	}
	return converted, nil
}
