package database

import (
	"testing"

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
			results, err := calculateRankedResult(test.votes)
			assert.Equal(t, test.results, results)
			assert.Equal(t, test.err, err)
		})
	}
}
