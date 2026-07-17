// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configdoc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

func ParseJSON(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, documentError(CodeMalformedJSON, "$", fmt.Errorf("document is not valid UTF-8"))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, documentError(CodeMalformedJSON, "$", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON documents")
		}
		return nil, documentError(CodeMalformedJSON, "$", err)
	}
	return document, nil
}

func EncodeJSON(document any) ([]byte, error) {
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, documentError(CodeInvalidJSONValue, "$", err)
	}
	return append(encoded, '\n'), nil
}

func JSONSet(document any, path string, value any) (any, error) {
	tokens, err := parseJSONPath(path)
	if err != nil {
		return nil, err
	}
	copy := cloneJSONDocument(document)
	valueCopy, err := copyJSONValue(value)
	if err != nil {
		return nil, err
	}
	return setJSONValue(copy, tokens, valueCopy, path)
}

func JSONDelete(document any, path string) (any, error) {
	tokens, err := parseJSONPath(path)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, documentError(CodeInvalidJSONPath, path, fmt.Errorf("document root cannot be deleted"))
	}
	return deleteJSONValue(cloneJSONDocument(document), tokens, path)
}

func JSONMove(document any, from, to string) (any, error) {
	fromTokens, err := parseJSONPath(from)
	if err != nil {
		return nil, err
	}
	toTokens, err := parseJSONPath(to)
	if err != nil {
		return nil, err
	}
	if len(fromTokens) == 0 || len(toTokens) == 0 {
		return nil, documentError(CodeInvalidJSONPath, from, fmt.Errorf("document root cannot be moved"))
	}
	copy := cloneJSONDocument(document)
	source, exists := lookupJSONValue(copy, fromTokens)
	if !exists {
		return nil, documentError(CodeJSONSourceMissing, from, fmt.Errorf("source does not exist"))
	}
	if _, exists := lookupJSONValue(copy, toTokens); exists {
		return nil, documentError(CodeJSONDestinationExists, to, fmt.Errorf("destination already exists"))
	}
	if err := requireCreatableJSONDestination(copy, toTokens, to); err != nil {
		return nil, err
	}
	copy, err = deleteJSONValue(copy, fromTokens, from)
	if err != nil {
		return nil, err
	}
	return setJSONValue(copy, toTokens, source, to)
}

// JSONPathExists reports whether path identifies a value in document. It uses
// the same closed JSONPath subset as the mutation operations.
func JSONPathExists(document any, path string) (bool, error) {
	tokens, err := parseJSONPath(path)
	if err != nil {
		return false, err
	}
	_, exists := lookupJSONValue(document, tokens)
	return exists, nil
}

func setJSONValue(document any, tokens []jsonPathToken, value any, path string) (any, error) {
	if len(tokens) == 0 {
		return value, nil
	}
	token := tokens[0]
	if token.isKey {
		object, ok := document.(map[string]any)
		if !ok {
			return nil, documentError(CodeJSONParentMissing, path, fmt.Errorf("object parent does not exist"))
		}
		if len(tokens) == 1 {
			object[token.key] = value
			return object, nil
		}
		child, exists := object[token.key]
		if !exists {
			return nil, documentError(CodeJSONParentMissing, path, fmt.Errorf("parent does not exist"))
		}
		updated, err := setJSONValue(child, tokens[1:], value, path)
		if err != nil {
			return nil, err
		}
		object[token.key] = updated
		return object, nil
	}
	array, ok := document.([]any)
	if !ok || token.index < 0 || token.index >= len(array) {
		return nil, documentError(CodeJSONParentMissing, path, fmt.Errorf("array parent or index does not exist"))
	}
	if len(tokens) == 1 {
		array[token.index] = value
		return array, nil
	}
	updated, err := setJSONValue(array[token.index], tokens[1:], value, path)
	if err != nil {
		return nil, err
	}
	array[token.index] = updated
	return array, nil
}

func deleteJSONValue(document any, tokens []jsonPathToken, path string) (any, error) {
	token := tokens[0]
	if token.isKey {
		object, ok := document.(map[string]any)
		if !ok {
			return nil, documentError(CodeJSONSourceMissing, path, fmt.Errorf("source does not exist"))
		}
		child, exists := object[token.key]
		if !exists {
			return nil, documentError(CodeJSONSourceMissing, path, fmt.Errorf("source does not exist"))
		}
		if len(tokens) == 1 {
			delete(object, token.key)
			return object, nil
		}
		updated, err := deleteJSONValue(child, tokens[1:], path)
		if err != nil {
			return nil, err
		}
		object[token.key] = updated
		return object, nil
	}
	array, ok := document.([]any)
	if !ok || token.index < 0 || token.index >= len(array) {
		return nil, documentError(CodeJSONSourceMissing, path, fmt.Errorf("source does not exist"))
	}
	if len(tokens) == 1 {
		return append(array[:token.index], array[token.index+1:]...), nil
	}
	updated, err := deleteJSONValue(array[token.index], tokens[1:], path)
	if err != nil {
		return nil, err
	}
	array[token.index] = updated
	return array, nil
}

func lookupJSONValue(document any, tokens []jsonPathToken) (any, bool) {
	current := document
	for _, token := range tokens {
		if token.isKey {
			object, ok := current.(map[string]any)
			if !ok {
				return nil, false
			}
			current, ok = object[token.key]
			if !ok {
				return nil, false
			}
			continue
		}
		array, ok := current.([]any)
		if !ok || token.index < 0 || token.index >= len(array) {
			return nil, false
		}
		current = array[token.index]
	}
	return current, true
}

func requireCreatableJSONDestination(document any, tokens []jsonPathToken, path string) error {
	parent, exists := lookupJSONValue(document, tokens[:len(tokens)-1])
	if !exists {
		return documentError(CodeJSONParentMissing, path, fmt.Errorf("destination parent does not exist"))
	}
	final := tokens[len(tokens)-1]
	if final.isKey {
		object, ok := parent.(map[string]any)
		if !ok {
			return documentError(CodeJSONParentMissing, path, fmt.Errorf("destination parent is not an object"))
		}
		if _, exists := object[final.key]; exists {
			return documentError(CodeJSONDestinationExists, path, fmt.Errorf("destination already exists"))
		}
		return nil
	}
	array, ok := parent.([]any)
	if ok && final.index >= 0 && final.index < len(array) {
		return documentError(CodeJSONDestinationExists, path, fmt.Errorf("destination already exists"))
	}
	return documentError(CodeJSONParentMissing, path, fmt.Errorf("new array positions are unsupported"))
}

func cloneJSONDocument(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		copy := make(map[string]any, len(typed))
		for key, child := range typed {
			copy[key] = cloneJSONDocument(child)
		}
		return copy
	case []any:
		copy := make([]any, len(typed))
		for index, child := range typed {
			copy[index] = cloneJSONDocument(child)
		}
		return copy
	default:
		return typed
	}
}

func copyJSONValue(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, documentError(CodeInvalidJSONValue, "$", err)
	}
	copy, err := ParseJSON(encoded)
	if err != nil {
		return nil, documentError(CodeInvalidJSONValue, "$", err)
	}
	return copy, nil
}
