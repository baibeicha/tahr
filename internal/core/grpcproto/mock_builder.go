package grpcproto

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MockBuilder dynamically generates type-aware sample JSON payloads from Protobuf message definitions.
// It follows the Weftloom pattern to synthesize realistic mock data without requiring protoc.
type MockBuilder struct {
	workspace *ProtoWorkspace
	maxDepth  int
}

// NewMockBuilder creates a mock payload generator connected to a proto workspace.
func NewMockBuilder(ws *ProtoWorkspace) *MockBuilder {
	return &MockBuilder{
		workspace: ws,
		maxDepth:  6,
	}
}

// BuildRPCRequestMock generates an editable, indented JSON request template for the given RPC method.
func (mb *MockBuilder) BuildRPCRequestMock(rpc *RPCMethod) (string, error) {
	if rpc == nil {
		return "{}", fmt.Errorf("rpc method cannot be nil")
	}

	reqType := rpc.RequestType
	if reqType == "" || reqType == "google.protobuf.Empty" || reqType == "Empty" {
		return "{\n}", nil
	}

	return mb.BuildMessageMockJSON(reqType)
}

// BuildMessageMockJSON synthesizes a formatted JSON sample string for a named message.
func (mb *MockBuilder) BuildMessageMockJSON(messageName string) (string, error) {
	visited := make(map[string]int)
	data := mb.synthesizeMessage(messageName, visited, 0)
	if data == nil {
		return "{}", nil
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "{}", fmt.Errorf("marshal mock json: %w", err)
	}

	return string(bytes), nil
}

// BuildMessageMockMap synthesizes a map[string]interface{} representing the mock payload.
func (mb *MockBuilder) BuildMessageMockMap(messageName string) (map[string]interface{}, error) {
	visited := make(map[string]int)
	res := mb.synthesizeMessage(messageName, visited, 0)
	if m, ok := res.(map[string]interface{}); ok {
		return m, nil
	}
	if res != nil {
		return map[string]interface{}{"value": res}, nil
	}
	return make(map[string]interface{}), nil
}

func (mb *MockBuilder) synthesizeMessage(messageName string, visited map[string]int, depth int) interface{} {
	trimmedName := strings.TrimPrefix(messageName, ".")

	// 1. Handle Google Well-Known Types
	if wk, handled := mb.handleWellKnownType(trimmedName); handled {
		return wk
	}

	// 2. Prevent infinite cyclic recursion
	if depth > mb.maxDepth || visited[trimmedName] >= 2 {
		return map[string]interface{}{}
	}

	if mb.workspace == nil {
		return map[string]interface{}{}
	}

	msg := mb.workspace.FindMessage(trimmedName)
	if msg == nil {
		// If message is not found, check if it's an enum
		if en := mb.workspace.FindEnum(trimmedName); en != nil {
			return mb.synthesizeEnum(en)
		}
		return map[string]interface{}{}
	}

	visited[trimmedName]++
	defer func() {
		visited[trimmedName]--
	}()

	result := make(map[string]interface{})
	processedOneOfs := make(map[string]bool)

	for _, field := range msg.Fields {
		// If field is part of a oneof, only include the first field in the oneof group
		if field.OneOfName != "" {
			if processedOneOfs[field.OneOfName] {
				continue
			}
			processedOneOfs[field.OneOfName] = true
		}

		fieldName := field.Name
		if jsonName, ok := field.Options["json_name"]; ok && jsonName != "" {
			fieldName = jsonName
		}

		val := mb.synthesizeField(&field, visited, depth+1)
		result[fieldName] = val
	}

	return result
}

func (mb *MockBuilder) synthesizeField(field *Field, visited map[string]int, depth int) interface{} {
	// 1. Map fields: map<K, V>
	if field.IsMap {
		keySample := mb.synthesizePrimitive("key", field.MapKeyType)
		keyStr := fmt.Sprintf("%v", keySample)
		valSample := mb.synthesizeType(field.MapValueType, field.Name+"_val", visited, depth)
		return map[string]interface{}{
			keyStr: valSample,
		}
	}

	// 2. Repeated fields
	if field.IsRepeated {
		singleVal := mb.synthesizeType(field.Type, field.Name, visited, depth)
		return []interface{}{singleVal}
	}

	// 3. Singular field
	return mb.synthesizeType(field.Type, field.Name, visited, depth)
}

func (mb *MockBuilder) synthesizeType(typeName, fieldName string, visited map[string]int, depth int) interface{} {
	// Check primitive types
	if isPrimitive(typeName) {
		return mb.synthesizePrimitive(fieldName, typeName)
	}

	// Check Google Well-Known Types
	if wk, ok := mb.handleWellKnownType(typeName); ok {
		return wk
	}

	// Check Enum in workspace
	if mb.workspace != nil {
		if en := mb.workspace.FindEnum(typeName); en != nil {
			return mb.synthesizeEnum(en)
		}
	}

	// Complex nested Message
	return mb.synthesizeMessage(typeName, visited, depth)
}

func (mb *MockBuilder) synthesizeEnum(en *Enum) interface{} {
	if len(en.Values) > 0 {
		// Prefer the first non-zero enum value or the first value
		for _, v := range en.Values {
			if v.Number != 0 {
				return v.Name
			}
		}
		return en.Values[0].Name
	}
	return "UNKNOWN"
}

func (mb *MockBuilder) synthesizePrimitive(fieldName, protoType string) interface{} {
	lowerName := strings.ToLower(fieldName)

	switch protoType {
	case "string":
		switch {
		case strings.Contains(lowerName, "email"):
			return "user@example.com"
		case strings.Contains(lowerName, "uuid") || strings.Contains(lowerName, "guid"):
			return "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
		case strings.HasSuffix(lowerName, "id") || lowerName == "id":
			return "usr_1001"
		case strings.Contains(lowerName, "url") || strings.Contains(lowerName, "uri"):
			return "https://api.example.com/v1"
		case strings.Contains(lowerName, "phone"):
			return "+1-555-0199"
		case strings.Contains(lowerName, "ip") || strings.Contains(lowerName, "host"):
			return "127.0.0.1"
		case strings.Contains(lowerName, "token") || strings.Contains(lowerName, "secret"):
			return "sample_token_xyz"
		case strings.Contains(lowerName, "title") || strings.Contains(lowerName, "name"):
			return "Sample " + strings.Title(strings.ReplaceAll(fieldName, "_", " "))
		default:
			return "sample_" + fieldName
		}

	case "bool":
		return true

	case "int32", "sint32", "sfixed32":
		switch {
		case strings.Contains(lowerName, "port"):
			return 50051
		case strings.Contains(lowerName, "page"):
			return 1
		case strings.Contains(lowerName, "limit") || strings.Contains(lowerName, "size"):
			return 20
		case strings.Contains(lowerName, "status") || strings.Contains(lowerName, "code"):
			return 200
		default:
			return 1
		}

	case "int64", "sint64", "sfixed64":
		switch {
		case strings.Contains(lowerName, "timestamp"):
			return 1700000000000
		default:
			return 1001
		}

	case "uint32", "fixed32":
		return 1

	case "uint64", "fixed64":
		return 1001

	case "float", "double":
		if strings.Contains(lowerName, "rate") || strings.Contains(lowerName, "ratio") {
			return 0.95
		}
		return 3.14

	case "bytes":
		// Base64 encoded "hello world"
		return "aGVsbG8gd29ybGQ="

	default:
		return "sample_" + fieldName
	}
}

func (mb *MockBuilder) handleWellKnownType(name string) (interface{}, bool) {
	trimmed := strings.TrimPrefix(name, ".")
	switch trimmed {
	case "google.protobuf.Timestamp", "Timestamp":
		return "2026-10-06T12:00:00Z", true
	case "google.protobuf.Duration", "Duration":
		return "10s", true
	case "google.protobuf.Empty", "Empty":
		return map[string]interface{}{}, true
	case "google.protobuf.Struct", "Struct":
		return map[string]interface{}{"metadata": "value"}, true
	case "google.protobuf.Value", "Value":
		return "sample_value", true
	case "google.protobuf.ListValue", "ListValue":
		return []interface{}{"sample_item_1", "sample_item_2"}, true
	case "google.protobuf.Any", "Any":
		return map[string]interface{}{
			"@type": "type.googleapis.com/google.protobuf.StringValue",
			"value": "sample_any_payload",
		}, true
	case "google.protobuf.FieldMask", "FieldMask":
		return "id,name,status", true
	case "google.protobuf.StringValue", "StringValue":
		return "sample_string", true
	case "google.protobuf.Int32Value", "Int32Value", "google.protobuf.UInt32Value", "UInt32Value":
		return 42, true
	case "google.protobuf.Int64Value", "Int64Value", "google.protobuf.UInt64Value", "UInt64Value":
		return 1001, true
	case "google.protobuf.BoolValue", "BoolValue":
		return true, true
	case "google.protobuf.FloatValue", "FloatValue", "google.protobuf.DoubleValue", "DoubleValue":
		return 3.14, true
	case "google.protobuf.BytesValue", "BytesValue":
		return "aGVsbG8=", true
	default:
		return nil, false
	}
}

func isPrimitive(t string) bool {
	switch t {
	case "double", "float", "int32", "int64", "uint32", "uint64",
		"sint32", "sint64", "fixed32", "fixed64", "sfixed32", "sfixed64",
		"bool", "string", "bytes":
		return true
	default:
		return false
	}
}
