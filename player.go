package main

import "fmt"

type Action int

const (
	Fold Action = iota
	Check
	Call
	Raise
	AllIn
)

func (a Action) String() string {
	return []string{"弃牌", "过牌", "跟注", "加注", "全下"}[a]
}

type Player struct {
	ID       int
	Name     string
	Chips    int
	HoleCards []Card
	Bet      int
	Folded   bool
	AllIn    bool
}

func NewPlayer(id int, name string, chips int) *Player {
	return &Player{
		ID:        id,
		Name:      name,
		Chips:     chips,
		HoleCards: make([]Card, 0, 2),
		Bet:       0,
		Folded:    false,
		AllIn:     false,
	}
}

func (p *Player) DealCard(card Card) {
	p.HoleCards = append(p.HoleCards, card)
}

func (p *Player) Reset() {
	p.HoleCards = p.HoleCards[:0]
	p.Bet = 0
	p.Folded = false
	p.AllIn = false
}

func (p *Player) Fold() {
	p.Folded = true
}

func (p *Player) Call(amount int) {
	callAmount := amount - p.Bet
	if p.Chips <= callAmount {
		p.AllIn = true
		callAmount = p.Chips
	}
	p.Chips -= callAmount
	p.Bet += callAmount
}

func (p *Player) Raise(currentBet, raiseAmount int) {
	totalBet := currentBet + raiseAmount
	p.Call(totalBet)
}

func (p *Player) GoAllIn() {
	if p.Chips > 0 {
		p.Bet += p.Chips
		p.Chips = 0
		p.AllIn = true
	}
}

func (p Player) String() string {
	if p.Folded {
		return fmt.Sprintf("玩家%d (%s): 已弃牌", p.ID, p.Name)
	}
	if p.AllIn {
		return fmt.Sprintf("玩家%d (%s): ALL-IN | 下注: %d | 底牌: %v", p.ID, p.Name, p.Bet, p.HoleCards)
	}
	return fmt.Sprintf("玩家%d (%s): 筹码: %d | 下注: %d | 底牌: %v", p.ID, p.Name, p.Chips, p.Bet, p.HoleCards)
}

func (p Player) SimpleString() string {
	if p.Folded {
		return fmt.Sprintf("玩家%d (%s): 已弃牌", p.ID, p.Name)
	}
	return fmt.Sprintf("玩家%d (%s): 筹码: %d | 底牌: %v", p.ID, p.Name, p.Chips, p.HoleCards)
}
