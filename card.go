package main

import (
	"fmt"
	"math/rand"
	"time"
)

type Suit int

const (
	Spades Suit = iota
	Hearts
	Diamonds
	Clubs
)

type Rank int

const (
	Two Rank = iota + 2
	Three
	Four
	Five
	Six
	Seven
	Eight
	Nine
	Ten
	Jack
	Queen
	King
	Ace
)

type Card struct {
	Suit Suit
	Rank Rank
}

func (c Card) String() string {
	suits := []string{"♠", "♥", "♦", "♣"}
	ranks := []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}
	return fmt.Sprintf("%s%s", suits[c.Suit], ranks[c.Rank-2])
}

func (c Card) ImageFileName() string {
	suitPrefix := []string{"ht", "hx", "fk", "mh"}
	rankStr := []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}
	return fmt.Sprintf("puke-img/%s-%s.png", suitPrefix[c.Suit], rankStr[c.Rank-2])
}

type Deck struct {
	Cards []Card
}

func NewDeck() *Deck {
	deck := &Deck{Cards: make([]Card, 0, 52)}
	for suit := Spades; suit <= Clubs; suit++ {
		for rank := Two; rank <= Ace; rank++ {
			deck.Cards = append(deck.Cards, Card{Suit: suit, Rank: rank})
		}
	}
	return deck
}

func (d *Deck) Shuffle() {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(d.Cards), func(i, j int) {
		d.Cards[i], d.Cards[j] = d.Cards[j], d.Cards[i]
	})
}

func (d *Deck) Deal() Card {
	card := d.Cards[0]
	d.Cards = d.Cards[1:]
	return card
}
