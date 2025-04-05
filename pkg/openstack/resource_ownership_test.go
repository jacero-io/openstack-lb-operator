package openstack

import (
	"testing"
)

func TestIsResourceOwnedByOperator(t *testing.T) {
	// Test cases
	tests := []struct {
		name     string
		tags     []string
		expected bool
	}{
		{
			name:     "Empty tags",
			tags:     []string{},
			expected: false,
		},
		{
			name:     "Has ownership tag",
			tags:     []string{TagManagedBy + "=" + TagManagedByValue},
			expected: true,
		},
		{
			name:     "Has ownership tag and other tags",
			tags:     []string{TagManagedBy + "=" + TagManagedByValue, "some-other-tag=value"},
			expected: true,
		},
		{
			name:     "Has similar but incorrect tag",
			tags:     []string{TagManagedBy + "=some-other-value"},
			expected: false,
		},
		{
			name:     "Has only other tags",
			tags:     []string{"tag1=value1", "tag2=value2"},
			expected: false,
		},
	}

	// Run tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isResourceOwnedByOperator(tt.tags)
			if result != tt.expected {
				t.Errorf("isResourceOwnedByOperator() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestConvertMapToTags(t *testing.T) {
	// Test cases
	tests := []struct {
		name     string
		input    map[string]string
		expected []string
	}{
		{
			name:     "Empty map",
			input:    map[string]string{},
			expected: []string{},
		},
		{
			name: "Single tag",
			input: map[string]string{
				"key1": "value1",
			},
			expected: []string{"key1=value1"},
		},
		{
			name: "Multiple tags",
			input: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			expected: []string{"key1=value1", "key2=value2"},
		},
	}

	// Run tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMapToTags(tt.input)

			// Check if result has the same length as expected
			if len(result) != len(tt.expected) {
				t.Errorf("convertMapToTags() returned %d tags, want %d", len(result), len(tt.expected))
				return
			}

			// Check if all expected tags are in the result
			// Note: Since map iteration is not deterministic, we can't check order
			for _, expectedTag := range tt.expected {
				found := false
				for _, resultTag := range result {
					if resultTag == expectedTag {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("convertMapToTags() result does not contain %s", expectedTag)
				}
			}
		})
	}
}
