package skywalking

import (
	"fmt"
	"strings"

	"github.com/graphql-go/graphql/language/ast"
)

func documentDepth(document string) int {
	depth, maximum := 0, 0
	for _, char := range document {
		if char == '{' {
			depth++
			if depth > maximum {
				maximum = depth
			}
		}
		if char == '}' {
			depth--
		}
	}
	return maximum
}

func fieldCount(document string) int {
	count := 0
	for _, char := range document {
		if char == '{' || char == '}' || char == ':' || char == '(' || char == ')' {
			continue
		}
		if char == '_' || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') {
			count++
		}
	}
	return count / 2
}

func validateDocument(document *ast.Document) (int, int, error) {
	if document == nil {
		return 0, 0, fmt.Errorf("graphql document is empty")
	}
	fragments := make(map[string]*ast.FragmentDefinition)
	operations := make([]*ast.OperationDefinition, 0, len(document.Definitions))
	for _, definition := range document.Definitions {
		switch typed := definition.(type) {
		case *ast.OperationDefinition:
			if typed.Operation != ast.OperationTypeQuery {
				return 0, 0, fmt.Errorf("graphql operation is read-only")
			}
			operations = append(operations, typed)
		case *ast.FragmentDefinition:
			if typed.Name == nil || typed.Name.Value == "" {
				return 0, 0, fmt.Errorf("graphql fragment name is required")
			}
			if _, exists := fragments[typed.Name.Value]; exists {
				return 0, 0, fmt.Errorf("graphql fragment %q is duplicated", typed.Name.Value)
			}
			fragments[typed.Name.Value] = typed
		default:
			return 0, 0, fmt.Errorf("graphql schema definitions are not supported")
		}
	}
	if len(operations) == 0 {
		return 0, 0, fmt.Errorf("graphql document has no query operation")
	}
	maxDepth, fields := 0, 0
	for _, operation := range operations {
		depth, count, err := validateSelectionSet(operation.SelectionSet, 1, fragments, map[string]bool{})
		if err != nil {
			return 0, 0, err
		}
		if depth > maxDepth {
			maxDepth = depth
		}
		fields += count
	}
	for name, fragment := range fragments {
		if _, _, err := validateSelectionSet(fragment.SelectionSet, 1, fragments, map[string]bool{name: true}); err != nil {
			return 0, 0, err
		}
	}
	return maxDepth, fields, nil
}

func validateSelectionSet(selectionSet *ast.SelectionSet, depth int, fragments map[string]*ast.FragmentDefinition, stack map[string]bool) (int, int, error) {
	if selectionSet == nil {
		return depth, 0, nil
	}
	maxDepth, fields := depth, 0
	for _, selection := range selectionSet.Selections {
		var childDepth, childFields int
		var err error
		switch typed := selection.(type) {
		case *ast.Field:
			if typed.Name == nil || typed.Name.Value == "" {
				return 0, 0, fmt.Errorf("graphql field name is required")
			}
			if strings.HasPrefix(typed.Name.Value, "__") {
				return 0, 0, fmt.Errorf("graphql introspection disabled")
			}
			fields++
			childDepth, childFields, err = validateSelectionSet(typed.SelectionSet, depth+1, fragments, stack)
		case *ast.InlineFragment:
			childDepth, childFields, err = validateSelectionSet(typed.SelectionSet, depth, fragments, stack)
		case *ast.FragmentSpread:
			if typed.Name == nil || typed.Name.Value == "" {
				return 0, 0, fmt.Errorf("graphql fragment spread name is required")
			}
			name := typed.Name.Value
			fragment := fragments[name]
			if fragment == nil {
				return 0, 0, fmt.Errorf("graphql fragment %q is undefined", name)
			}
			if stack[name] {
				return 0, 0, fmt.Errorf("graphql fragment cycle detected")
			}
			nextStack := make(map[string]bool, len(stack)+1)
			for key, value := range stack {
				nextStack[key] = value
			}
			nextStack[name] = true
			childDepth, childFields, err = validateSelectionSet(fragment.SelectionSet, depth, fragments, nextStack)
		default:
			return 0, 0, fmt.Errorf("graphql selection is unsupported")
		}
		if err != nil {
			return 0, 0, err
		}
		if childDepth > maxDepth {
			maxDepth = childDepth
		}
		fields += childFields
	}
	return maxDepth, fields, nil
}
