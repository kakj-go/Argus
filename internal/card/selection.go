package card

import "sort"

type Candidate struct {
	CardID           string
	Revision         int
	Source           string
	Available        bool
	RequiredResolved bool
	StrictMatches    int
	CompatibleSlots  int
	IntentScore      int
}

func SelectCandidate(candidates []Candidate) (Candidate, bool) {
	eligible := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Available && candidate.RequiredResolved {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		return Candidate{}, false
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		left, right := eligible[i], eligible[j]
		if left.CompatibleSlots != right.CompatibleSlots {
			return left.CompatibleSlots > right.CompatibleSlots
		}
		if left.StrictMatches != right.StrictMatches {
			return left.StrictMatches > right.StrictMatches
		}
		if left.IntentScore != right.IntentScore {
			return left.IntentScore > right.IntentScore
		}
		if left.Source != right.Source {
			return left.Source == "system"
		}
		if left.Revision != right.Revision {
			return left.Revision > right.Revision
		}
		return left.CardID < right.CardID
	})
	return eligible[0], true
}
