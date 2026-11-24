package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func makeOptions(n int) []string {
	options := make([]string, n)
	for i := range options {
		options[i] = fmt.Sprintf("Option %d", i+1)
	}
	return options
}

func TestParseCustomOptions(t *testing.T) {
	tests := []struct {
		name    string
		raw     []string
		ranked  bool
		want    []string
		wantErr bool
	}{
		{
			name: "trims and keeps order",
			raw:  []string{" A ", "B"},
			want: []string{"A", "B", "Abstain"},
		},
		{
			name: "commas kept",
			raw:  []string{"Smith, John", "Doe, Jane"},
			want: []string{"Smith, John", "Doe, Jane", "Abstain"},
		},
		{
			name: "blanks dropped",
			raw:  []string{"A", "", "  ", "B"},
			want: []string{"A", "B", "Abstain"},
		},
		{
			name: "user abstain kept without a second one",
			raw:  []string{"Yes", "abstain"},
			want: []string{"Yes", "abstain"},
		},
		{
			name:   "ranked gets no abstain",
			raw:    []string{"A", "B", "C"},
			ranked: true,
			want:   []string{"A", "B", "C"},
		},
		{
			name:   "single option",
			raw:    []string{"Only"},
			ranked: true,
			want:   []string{"Only"},
		},
		{
			name:    "no options",
			raw:     []string{},
			wantErr: true,
		},
		{
			name:    "only blank options",
			raw:     []string{"", " "},
			ranked:  true,
			wantErr: true,
		},
		{
			name:    "case insensitive duplicate",
			raw:     []string{"Alice", " alice"},
			wantErr: true,
		},
		{
			name:   "50 options allowed",
			raw:    makeOptions(50),
			ranked: true,
			want:   makeOptions(50),
		},
		{
			name:    "51 options rejected",
			raw:     makeOptions(51),
			ranked:  true,
			wantErr: true,
		},
		{
			name: "limit applies before abstain",
			raw:  makeOptions(50),
			want: append(makeOptions(50), "Abstain"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseCustomOptions(test.raw, test.ranked)
			if test.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}
