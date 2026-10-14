package apiserver

import (
	"testing"
)

func TestParseID(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		wantErr  bool
	}{
		{"123", 123, false},
		{"0", 0, false},
		{"-1", -1, false},
		{"abc", 0, true},
		{"2147483647", 2147483647, false},
		{"-2147483648", -2147483648, false},
		{"2147483648", 0, true},  // overflow int32
		{"-2147483649", 0, true}, // underflow int32
	}

	for _, tt := range tests {
		got, err := parseID(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.expected {
			t.Errorf("parseID(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}
