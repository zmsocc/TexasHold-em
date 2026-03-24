package main

import (
	"fmt"
	"sort"
)

type HandRank int

const (
	HighCard HandRank = iota
	OnePair
	TwoPair
	ThreeOfKind
	Straight
	Flush
	FullHouse
	FourOfKind
	StraightFlush
	RoyalFlush
)

func (h HandRank) String() string {
	return []string{
		"高牌", "一对", "两对", "三条", "顺子", "同花",
		"满堂红", "四条", "同花顺", "皇家同花顺",
	}[h]
}

type Hand struct {
	Cards  []Card
	Rank   HandRank
	Kickers []Rank
}

func (h Hand) String() string {
	return fmt.Sprintf("%v (%v)", h.Cards, h.Rank)
}

func EvaluateHand(cards []Card) Hand {
	combinations := combinations(cards, 5)
	bestHand := Hand{}
	for _, combo := range combinations {
		hand := evaluateCombo(combo)
		if hand.Rank > bestHand.Rank || (hand.Rank == bestHand.Rank && compareKickers(hand.Kickers, bestHand.Kickers) > 0) {
			bestHand = hand
		}
	}
	return bestHand
}

func combinations(cards []Card, k int) [][]Card {
	var result [][]Card
	var backtrack func(start int, current []Card)
	backtrack = func(start int, current []Card) {
		if len(current) == k {
			temp := make([]Card, k)
			copy(temp, current)
			result = append(result, temp)
			return
		}
		for i := start; i < len(cards); i++ {
			current = append(current, cards[i])
			backtrack(i+1, current)
			current = current[:len(current)-1]
		}
	}
	backtrack(0, []Card{})
	return result
}

func evaluateCombo(cards []Card) Hand {
	rankCounts := make(map[Rank]int)
	suitCounts := make(map[Suit]int)
	ranks := make([]Rank, 0, 5)
	
	for _, c := range cards {
		rankCounts[c.Rank]++
		suitCounts[c.Suit]++
		ranks = append(ranks, c.Rank)
	}
	
	sort.Slice(ranks, func(i, j int) bool { return ranks[i] > ranks[j] })
	
	isFlush := len(suitCounts) == 1
	isStraight := checkStraight(ranks)
	isRoyal := isStraight && ranks[0] == Ace && ranks[1] == King
	
	countGroups := make(map[int][]Rank)
	for r, cnt := range rankCounts {
		countGroups[cnt] = append(countGroups[cnt], r)
	}
	
	for _, rs := range countGroups {
		sort.Slice(rs, func(i, j int) bool { return rs[i] > rs[j] })
	}
	
	var handRank HandRank
	var kickers []Rank
	
	switch {
	case isRoyal && isFlush:
		handRank = RoyalFlush
	case isStraight && isFlush:
		handRank = StraightFlush
		kickers = []Rank{ranks[0]}
	case len(countGroups[4]) > 0:
		handRank = FourOfKind
		four := countGroups[4][0]
		kickers = append(countGroups[1], four)
	case len(countGroups[3]) > 0 && len(countGroups[2]) > 0:
		handRank = FullHouse
		three := countGroups[3][0]
		pair := countGroups[2][0]
		kickers = []Rank{three, pair}
	case isFlush:
		handRank = Flush
		kickers = ranks
	case isStraight:
		handRank = Straight
		kickers = []Rank{ranks[0]}
	case len(countGroups[3]) > 0:
		handRank = ThreeOfKind
		three := countGroups[3][0]
		kickers = append(countGroups[1], three)
	case len(countGroups[2]) == 2:
		handRank = TwoPair
		pairs := countGroups[2]
		kickers = append(countGroups[1], pairs[0], pairs[1])
	case len(countGroups[2]) == 1:
		handRank = OnePair
		pair := countGroups[2][0]
		kickers = append(countGroups[1], pair)
	default:
		handRank = HighCard
		kickers = ranks
	}
	
	return Hand{Cards: cards, Rank: handRank, Kickers: kickers}
}

func checkStraight(ranks []Rank) bool {
	sortedRanks := make([]Rank, len(ranks))
	copy(sortedRanks, ranks)
	sort.Slice(sortedRanks, func(i, j int) bool { return sortedRanks[i] > sortedRanks[j] })
	
	normal := true
	for i := 0; i < 4; i++ {
		if sortedRanks[i]-sortedRanks[i+1] != 1 {
			normal = false
			break
		}
	}
	if normal {
		return true
	}
	
	if sortedRanks[0] == Ace && sortedRanks[1] == Five && sortedRanks[2] == Four && sortedRanks[3] == Three && sortedRanks[4] == Two {
		return true
	}
	
	return false
}

func compareKickers(a, b []Rank) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if a[i] > b[i] {
			return 1
		} else if a[i] < b[i] {
			return -1
		}
	}
	return 0
}

func CompareHands(h1, h2 Hand) int {
	if h1.Rank > h2.Rank {
		return 1
	} else if h1.Rank < h2.Rank {
		return -1
	}
	return compareKickers(h1.Kickers, h2.Kickers)
}
