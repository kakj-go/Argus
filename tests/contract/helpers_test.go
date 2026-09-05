package contract_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"sigs.k8s.io/yaml"
)

func readJSON(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func schemaCompiler(t *testing.T, root string) *jsonschema.Compiler {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	err := filepath.WalkDir(filepath.Join(root, "api/schemas"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".schema.json") {
			return err
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer file.Close()
		document, decodeErr := jsonschema.UnmarshalJSON(file)
		if decodeErr != nil {
			return decodeErr
		}
		rootObject, ok := document.(map[string]any)
		if !ok {
			return fmt.Errorf("schema %s is not an object", path)
		}
		id, ok := rootObject["$id"].(string)
		if !ok || id == "" {
			return fmt.Errorf("schema %s has no $id", path)
		}
		return compiler.AddResource(id, document)
	})
	if err != nil {
		t.Fatal(err)
	}
	return compiler
}

func openAPISchemaCompiler(t *testing.T, root string) *jsonschema.Compiler {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "api/openapi/generated/argus.bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource("https://argus.io/openapi/v1/argus.bundle.json", document); err != nil {
		t.Fatal(err)
	}
	return compiler
}

func readYAML(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func decodeStructured(t *testing.T, path string, data []byte, target any) {
	t.Helper()
	if strings.HasSuffix(path, ".json") {
		if err := json.Unmarshal(data, target); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := yaml.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func gitFile(root, ref, path string) ([]byte, bool) {
	command := exec.Command("git", "show", ref+":"+path)
	command.Dir = root
	data, err := command.Output()
	return data, err == nil
}

func gitFileList(root, ref string, directories ...string) []string {
	args := []string{"ls-tree", "-r", "--name-only", ref, "--"}
	args = append(args, directories...)
	command := exec.Command("git", args...)
	command.Dir = root
	data, err := command.Output()
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" && (strings.HasSuffix(line, ".json") || strings.HasSuffix(line, ".yaml")) {
			result = append(result, line)
		}
	}
	return result
}

func requiresStableMapKeys(path string) bool {
	for _, suffix := range []string{"/schemas", "/properties", "/$defs", "/codes", "/machines", "/transitions", "/paths"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func typeCompatible(oldValue, newValue any) bool {
	oldSet := scalarOrSliceSet(oldValue)
	newSet := scalarOrSliceSet(newValue)
	for value := range oldSet {
		if !newSet[value] {
			return false
		}
	}
	return true
}

func scalarOrSliceSet(value any) map[string]bool {
	if values, ok := value.([]any); ok {
		return valueSet(values)
	}
	if value == nil {
		return map[string]bool{}
	}
	return map[string]bool{fmt.Sprint(value): true}
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	default:
		return 0, false
	}
}

func valueSet(value any) map[string]bool {
	result := map[string]bool{}
	values, _ := value.([]any)
	for _, item := range values {
		result[fmt.Sprint(item)] = true
	}
	return result
}

func sliceSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}

func walkKeys(value any, visit func(string) error) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if err := visit(key); err != nil {
				return err
			}
			if err := walkKeys(child, visit); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := walkKeys(child, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func cloneJSON(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var cloned any
	if err := decoder.Decode(&cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}
