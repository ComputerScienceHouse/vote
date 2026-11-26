package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func makeVotes() []RankedVote {
	// so inpyt for this, we want to have option, then a list of ranks.
	// am tempted to have some shorthand for generating test cases more easily
	return []RankedVote{}
}

func TestResultCalcs(t *testing.T) {
	// for votes, we only need to define options, we don't currently rely on IDs
	tests := []struct {
		name    string
		votes   []RankedVote
		results []map[string]int
		err     error
	}{
		{
			name: "Empty Votes",
			votes: []RankedVote{
				{
					Options: map[string]int{},
				},
			},
			results: []map[string]int{
				{},
			},
		},
		{
			name: "1 vote",
			votes: []RankedVote{
				{
					Options: map[string]int{
						"first":  1,
						"second": 2,
						"third":  3,
					},
				},
			},
			results: []map[string]int{
				{
					"first": 1,
				},
			},
		},
		{
			name: "Tie vote",
			votes: []RankedVote{
				{
					Options: map[string]int{
						"first":  1,
						"second": 2,
					},
				},
				{
					Options: map[string]int{
						"first":  2,
						"second": 1,
					},
				},
			},
			results: []map[string]int{
				{
					"first":  1,
					"second": 1,
				},
			},
		},
		{
			name: "Several Rounds",
			votes: []RankedVote{
				{
					Options: map[string]int{
						"a": 1,
						"b": 2,
						"c": 3,
					},
				},
				{
					Options: map[string]int{
						"a": 2,
						"b": 1,
						"c": 3,
					},
				},
				{
					Options: map[string]int{
						"a": 1,
						"b": 2,
						"c": 3,
					},
				},
				{
					Options: map[string]int{
						"a": 2,
						"b": 1,
						"c": 3,
					},
				},
				{
					Options: map[string]int{
						"a": 2,
						"b": 3,
						"c": 1,
					},
				},
			},
			results: []map[string]int{
				{
					"a": 2,
					"b": 2,
					"c": 1,
				},
				{
					"a": 3,
					"b": 2,
				},
				{
					"a": 3,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := calculateRankedResult(context.Background(), test.votes)
			assert.Equal(t, test.results, results)
			assert.Equal(t, test.err, err)
		})
	}
}

func TestOrderOptions(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]int
		output []string
	}{
		{
			name: "SimpleOrder",
			input: map[string]int{
				"one":   1,
				"two":   2,
				"three": 3,
				"four":  4,
			},
			output: []string{"one", "two", "three", "four"},
		},
		{
			name: "Reversed",
			input: map[string]int{
				"one":   4,
				"two":   3,
				"three": 2,
				"four":  1,
			},
			output: []string{"four", "three", "two", "one"},
		},
		{
			name: "HighStart",
			input: map[string]int{
				"one":   2,
				"two":   3,
				"three": 4,
				"four":  5,
			},
			output: []string{"one", "two", "three", "four"},
		},
		{
			name: "LowStart",
			input: map[string]int{
				"one":   0,
				"two":   1,
				"three": 2,
				"four":  3,
			},
			output: []string{"one", "two", "three", "four"},
		},
		{
			name: "Negative",
			input: map[string]int{
				"one":   -1,
				"two":   1,
				"three": 2,
				"four":  3,
			},
			output: []string{"one", "two", "three", "four"},
		},
		{
			name: "duplicate, expect bad output",
			input: map[string]int{
				"one":   0,
				"two":   1,
				"three": 1,
				"four":  2,
			},
			output: []string{"one", "three", "three", "four"},
		},
		{
			name: "Gap",
			input: map[string]int{
				"one":   1,
				"two":   2,
				"three": 4,
				"four":  5,
			},
			output: []string{"one", "two", "three", "four"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			assert.Equal(t, test.output, orderOptions(ctx, test.input))
		})
	}
}

// ballot builds a RankedVote from options listed in order of preference
func ballot(options ...string) RankedVote {
	ranks := make(map[string]int, len(options))
	for i, option := range options {
		ranks[option] = i + 1
	}
	return RankedVote{Options: ranks}
}

// ballots repeats the same ballot count times
func ballots(count int, options ...string) []RankedVote {
	votes := make([]RankedVote, 0, count)
	for i := 0; i < count; i++ {
		votes = append(votes, ballot(options...))
	}
	return votes
}

func concatVotes(groups ...[]RankedVote) []RankedVote {
	votes := make([]RankedVote, 0)
	for _, group := range groups {
		votes = append(votes, group...)
	}
	return votes
}

func TestNoConfidenceNeverEliminated(t *testing.T) {
	tests := []struct {
		name    string
		votes   []RankedVote
		results []map[string]int
	}{
		{
			name: "Lowest is No Confidence, next lowest eliminated instead",
			votes: concatVotes(
				ballots(3, "a"),
				ballots(2, "b"),
				ballots(1, "c", "a"),
				ballots(1, "No Confidence", "b"),
			),
			results: []map[string]int{
				{"a": 3, "b": 2, "c": 1, "No Confidence": 1},
				{"a": 4, "b": 2, "No Confidence": 1},
				{"a": 4},
			},
		},
		{
			name: "No Confidence survives multiple rounds in last place",
			votes: concatVotes(
				ballots(2, "a"),
				ballots(2, "b"),
				ballots(1, "c", "a"),
				ballots(1, "No Confidence"),
			),
			results: []map[string]int{
				{"a": 2, "b": 2, "c": 1, "No Confidence": 1},
				{"a": 3, "b": 2, "No Confidence": 1},
				{"a": 3, "No Confidence": 1},
				{"a": 3},
			},
		},
		{
			name: "No Confidence wins after other options are eliminated",
			votes: concatVotes(
				ballots(2, "No Confidence"),
				ballots(2, "a", "No Confidence"),
				ballots(1, "b", "No Confidence"),
			),
			results: []map[string]int{
				{"No Confidence": 2, "a": 2, "b": 1},
				{"No Confidence": 3, "a": 2},
				{"No Confidence": 3},
			},
		},
		{
			name: "Remaining options tied above No Confidence is a tie, not a No Confidence win",
			votes: concatVotes(
				ballots(2, "a"),
				ballots(2, "b"),
				ballots(1, "No Confidence"),
			),
			results: []map[string]int{
				{"a": 2, "b": 2, "No Confidence": 1},
			},
		},
		{
			name: "Remaining options tied below No Confidence are all eliminated",
			votes: concatVotes(
				ballots(3, "No Confidence"),
				ballots(2, "a"),
				ballots(2, "b"),
			),
			results: []map[string]int{
				{"No Confidence": 3, "a": 2, "b": 2},
				{"No Confidence": 3},
			},
		},
		{
			name: "No Confidence tied with only remaining option is a tie",
			votes: concatVotes(
				ballots(2, "No Confidence"),
				ballots(2, "a"),
			),
			results: []map[string]int{
				{"No Confidence": 2, "a": 2},
			},
		},
		{
			name:  "Only No Confidence",
			votes: ballots(1, "No Confidence"),
			results: []map[string]int{
				{"No Confidence": 1},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := calculateRankedResult(context.Background(), test.votes)
			assert.NoError(t, err)
			assert.Equal(t, test.results, results)
		})
	}
}

func TestNoConfidenceCaseInsensitive(t *testing.T) {
	for _, nc := range []string{"No Confidence", "no confidence", "NO CONFIDENCE", "nO cOnFiDeNcE", " No Confidence "} {
		t.Run(nc, func(t *testing.T) {
			votes := concatVotes(
				ballots(3, "a"),
				ballots(2, "b"),
				ballots(1, "c", "a"),
				ballots(1, nc, "b"),
			)
			results, err := calculateRankedResult(context.Background(), votes)
			assert.NoError(t, err)
			assert.Equal(t, []map[string]int{
				{"a": 3, "b": 2, "c": 1, nc: 1},
				{"a": 4, "b": 2, nc: 1},
				{"a": 4},
			}, results)
		})
	}
}

func TestIsNoConfidence(t *testing.T) {
	tests := map[string]bool{
		"No Confidence":    true,
		"no confidence":    true,
		"NO CONFIDENCE":    true,
		"  no confidence ": true,
		"No-Confidence":    false,
		"NoConfidence":     false,
		"Confidence":       false,
		"No Confidence 2":  false,
		"":                 false,
	}
	for input, expected := range tests {
		t.Run(input, func(t *testing.T) {
			assert.Equal(t, expected, isNoConfidence(input))
		})
	}
}
