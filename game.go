package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
)

type Game struct {
	Players        []*Player
	Deck           *Deck
	CommunityCards []Card
	Pot            int
	ButtonPos      int
	SmallBlind     int
	BigBlind       int
	CurrentBet     int
	scanner        *bufio.Scanner
}

func NewGame(numPlayers int, startingChips int, humanPlayerName string) *Game {
	game := &Game{
		Players:        make([]*Player, 0, numPlayers),
		CommunityCards: make([]Card, 0, 5),
		ButtonPos:      0,
		SmallBlind:     1,
		BigBlind:       2,
		CurrentBet:     0,
		scanner:        bufio.NewScanner(os.Stdin),
	}
	names := []string{"张三", "李四", "王五", "赵六", "钱七", "孙八", "周九", "吴十", "郑十一", "王十二"}
	for i := 0; i < numPlayers; i++ {
		name := names[i%len(names)]
		if i == 0 {
			if humanPlayerName != "" {
				name = humanPlayerName
			} else {
				name = "你"
			}
		}
		game.Players = append(game.Players, NewPlayer(i+1, name, startingChips))
	}
	return game
}

func (g *Game) NewHand() {
	g.Deck = NewDeck()
	g.Deck.Shuffle()
	g.CommunityCards = g.CommunityCards[:0]
	g.Pot = 0
	g.CurrentBet = 0
	for _, p := range g.Players {
		p.Reset()
	}
}

func (g *Game) PlayHand() {
	g.NewHand()
	g.DealHoleCards()
	g.PostBlinds()
	g.BettingRound("翻牌前")
	if g.GameOver() {
		g.Showdown()
		return
	}
	g.DealFlop()
	g.BettingRound("翻牌圈")
	if g.GameOver() {
		g.Showdown()
		return
	}
	g.DealTurn()
	g.BettingRound("转牌圈")
	if g.GameOver() {
		g.Showdown()
		return
	}
	g.DealRiver()
	g.BettingRound("河牌圈")
	g.Showdown()
}

func (g *Game) DealHoleCards() {
	for i := 0; i < 2; i++ {
		for _, p := range g.Players {
			if p.Chips > 0 {
				p.DealCard(g.Deck.Deal())
			}
		}
	}
	fmt.Println("\n=== 底牌已发放 ===")
	g.ShowPlayerStatus(false)
}

func (g *Game) PostBlinds() {
	sbPos := (g.ButtonPos + 1) % len(g.Players)
	bbPos := (g.ButtonPos + 2) % len(g.Players)

	sbPlayer := g.Players[sbPos]
	if sbPlayer.Chips > 0 {
		sbAmount := g.SmallBlind
		if sbPlayer.Chips < sbAmount {
			sbAmount = sbPlayer.Chips
			sbPlayer.AllIn = true
		}
		sbPlayer.Chips -= sbAmount
		sbPlayer.Bet = sbAmount
		g.Pot += sbAmount
	}

	bbPlayer := g.Players[bbPos]
	if bbPlayer.Chips > 0 {
		bbAmount := g.BigBlind
		if bbPlayer.Chips < bbAmount {
			bbAmount = bbPlayer.Chips
			bbPlayer.AllIn = true
		}
		bbPlayer.Chips -= bbAmount
		bbPlayer.Bet = bbAmount
		g.Pot += bbAmount
	}

	g.CurrentBet = g.BigBlind
	fmt.Printf("\n小盲注 %d 筹码由 %s 下，大盲注 %d 筹码由 %s 下\n", g.SmallBlind, sbPlayer.Name, g.BigBlind, bbPlayer.Name)
	fmt.Printf("当前底池: %d 筹码\n", g.Pot)
}

func (g *Game) BettingRound(phase string) {
	fmt.Printf("\n=== %s ===", phase)
	if len(g.CommunityCards) > 0 {
		fmt.Printf(" | 公共牌: %v", g.CommunityCards)
	}
	fmt.Println()

	startPos := g.ButtonPos + 1
	if phase == "翻牌前" {
		startPos = g.ButtonPos + 3
	}

	activePlayers := g.getActivePlayers()
	if len(activePlayers) <= 1 {
		return
	}

	lastRaisePos := -1
	currentPos := startPos % len(g.Players)
	betCount := 0

	for {
		player := g.Players[currentPos]

		if player.Folded || player.AllIn {
			currentPos = (currentPos + 1) % len(g.Players)
			continue
		}

		if lastRaisePos == currentPos && betCount > 0 {
			break
		}

		fmt.Println("\n" + strings.Repeat("-", 50))
		g.ShowPlayerStatus(true)
		fmt.Printf("底池: %d 筹码 | 当前下注: %d\n", g.Pot, g.CurrentBet)

		var action Action
		var raiseAmount int

		if player.Name == "你" {
			action, raiseAmount = g.getPlayerAction(player)
		} else {
			action, raiseAmount = g.getAIAction(player)
		}

		g.executeAction(player, action, raiseAmount)

		if action == Raise || action == AllIn {
			lastRaisePos = currentPos
			betCount++
		} else if action == Fold {
			activePlayers = g.getActivePlayers()
			if len(activePlayers) <= 1 {
				break
			}
		}

		currentPos = (currentPos + 1) % len(g.Players)
	}

	g.resetBets()
}

func (g *Game) getPlayerAction(player *Player) (Action, int) {
	for {
		fmt.Printf("\n轮到你了，%s！\n", player.Name)
		fmt.Printf("你的底牌: %v | 筹码: %d | 已下注: %d\n", player.HoleCards, player.Chips, player.Bet)

		canCheck := g.CurrentBet == player.Bet
		canCall := g.CurrentBet > player.Bet && player.Chips > 0
		minRaise := g.BigBlind
		if g.CurrentBet > 0 {
			minRaise = g.CurrentBet
		}
		canRaise := player.Chips > (g.CurrentBet - player.Bet + minRaise)

		fmt.Println("可选动作:")
		if canCheck {
			fmt.Println("1. 过牌")
		}
		if canCall {
			callAmount := g.CurrentBet - player.Bet
			fmt.Printf("2. 跟注 (%d 筹码)\n", callAmount)
		}
		if canRaise {
			fmt.Printf("3. 加注 (至少 %d 筹码)\n", minRaise)
		}
		if player.Chips > 0 {
			fmt.Println("4. 全下")
		}
		fmt.Println("0. 弃牌")

		fmt.Print("请选择动作 (0-4): ")
		g.scanner.Scan()
		input := strings.TrimSpace(g.scanner.Text())

		switch input {
		case "0":
			return Fold, 0
		case "1":
			if canCheck {
				return Check, 0
			}
			fmt.Println("不能过牌！")
		case "2":
			if canCall {
				return Call, 0
			}
			fmt.Println("不能跟注！")
		case "3":
			if canRaise {
				for {
					fmt.Printf("请输入加注金额 (至少 %d，最多 %d): ", minRaise, player.Chips-(g.CurrentBet-player.Bet))
					g.scanner.Scan()
					amountInput := strings.TrimSpace(g.scanner.Text())
					amount, err := strconv.Atoi(amountInput)
					if err == nil && amount >= minRaise && amount <= (player.Chips-(g.CurrentBet-player.Bet)) {
						return Raise, amount
					}
					fmt.Println("无效的金额！")
				}
			}
			fmt.Println("不能加注！")
		case "4":
			return AllIn, 0
		default:
			fmt.Println("无效的选择！")
		}
	}
}

func (g *Game) getAIAction(player *Player) (Action, int) {
	callAmount := g.CurrentBet - player.Bet

	var action Action
	var raiseAmount int

	allCards := append(player.HoleCards, g.CommunityCards...)
	var handStrength HandRank = HighCard

	if len(allCards) >= 5 {
		hand := EvaluateHand(allCards)
		handStrength = hand.Rank
	} else if len(allCards) >= 2 {
		if player.HoleCards[0].Rank == player.HoleCards[1].Rank {
			handStrength = OnePair
			if player.HoleCards[0].Rank >= Jack {
				handStrength = ThreeOfKind
			}
		} else if player.HoleCards[0].Rank >= Ace || player.HoleCards[1].Rank >= Ace {
			handStrength = HighCard
		} else if player.HoleCards[0].Rank >= King || player.HoleCards[1].Rank >= King {
			handStrength = HighCard
		}
	}

	if callAmount == 0 {
		if handStrength >= OnePair {
			if rand.Intn(2) == 0 {
				action = Raise
				raiseAmount = g.BigBlind * 2
				if raiseAmount > player.Chips {
					raiseAmount = player.Chips
				}
			} else {
				action = Check
			}
		} else {
			action = Check
		}
	} else if handStrength >= ThreeOfKind {
		if player.Chips > callAmount+g.BigBlind*3 {
			action = Raise
			raiseAmount = g.BigBlind * 3
			if raiseAmount > player.Chips-callAmount {
				raiseAmount = player.Chips - callAmount
			}
		} else if player.Chips > callAmount {
			action = Call
		} else {
			action = AllIn
		}
	} else if handStrength >= OnePair {
		if callAmount <= player.Chips/2 {
			if rand.Intn(3) == 0 && player.Chips > callAmount+g.BigBlind*2 {
				action = Raise
				raiseAmount = g.BigBlind * 2
				if raiseAmount > player.Chips-callAmount {
					raiseAmount = player.Chips - callAmount
				}
			} else {
				action = Call
			}
		} else if callAmount <= player.Chips {
			action = Call
		} else {
			action = Fold
		}
	} else {
		if callAmount <= player.Chips/4 {
			action = Call
		} else if callAmount <= player.Chips/3 && rand.Intn(2) == 0 {
			action = Call
		} else {
			action = Fold
		}
	}

	fmt.Printf("\n%s 选择: %s (牌力: %v)\n", player.Name, action, handStrength)
	return action, raiseAmount
}

func (g *Game) executeAction(player *Player, action Action, raiseAmount int) {
	switch action {
	case Fold:
		player.Fold()
		fmt.Printf("%s 弃牌\n", player.Name)
	case Check:
		fmt.Printf("%s 过牌\n", player.Name)
	case Call:
		player.Call(g.CurrentBet)
		g.Pot += g.CurrentBet - player.Bet
		fmt.Printf("%s 跟注 %d 筹码\n", player.Name, g.CurrentBet-player.Bet)
	case Raise:
		oldBet := player.Bet
		player.Raise(g.CurrentBet, raiseAmount)
		g.CurrentBet = player.Bet
		g.Pot += player.Bet - oldBet
		fmt.Printf("%s 加注到 %d 筹码\n", player.Name, g.CurrentBet)
	case AllIn:
		oldBet := player.Bet
		player.GoAllIn()
		if player.Bet > g.CurrentBet {
			g.CurrentBet = player.Bet
		}
		g.Pot += player.Bet - oldBet
		fmt.Printf("%s 全下 %d 筹码\n", player.Name, player.Bet-oldBet)
	}
}

func (g *Game) DealFlop() {
	g.Deck.Deal()
	g.CommunityCards = append(g.CommunityCards, g.Deck.Deal(), g.Deck.Deal(), g.Deck.Deal())
	fmt.Printf("\n翻牌: %v\n", g.CommunityCards)
}

func (g *Game) DealTurn() {
	g.Deck.Deal()
	g.CommunityCards = append(g.CommunityCards, g.Deck.Deal())
	fmt.Printf("\n转牌: %v\n", g.CommunityCards)
}

func (g *Game) DealRiver() {
	g.Deck.Deal()
	g.CommunityCards = append(g.CommunityCards, g.Deck.Deal())
	fmt.Printf("\n河牌: %v\n", g.CommunityCards)
}

func (g *Game) GameOver() bool {
	activePlayers := g.getActivePlayers()
	return len(activePlayers) <= 1
}

func (g *Game) getActivePlayers() []*Player {
	var active []*Player
	for _, p := range g.Players {
		if !p.Folded {
			active = append(active, p)
		}
	}
	return active
}

func (g *Game) Showdown() {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("=== 摊牌 ===")
	fmt.Println(strings.Repeat("=", 50))

	activePlayers := g.getActivePlayers()

	if len(activePlayers) == 1 {
		winner := activePlayers[0]
		winner.Chips += g.Pot
		fmt.Printf("\n%s 获胜！赢得 %d 筹码！\n", winner.Name, g.Pot)
	} else {
		type playerHand struct {
			player *Player
			hand   Hand
		}

		var hands []playerHand

		for _, p := range activePlayers {
			allCards := append(p.HoleCards, g.CommunityCards...)
			hand := EvaluateHand(allCards)
			hands = append(hands, playerHand{p, hand})
			fmt.Printf("\n%s: %v - %v", p.Name, p.HoleCards, hand.Rank)
		}

		bestHand := hands[0].hand
		var winners []*Player

		for _, ph := range hands {
			cmp := CompareHands(ph.hand, bestHand)
			if cmp > 0 {
				bestHand = ph.hand
				winners = []*Player{ph.player}
			} else if cmp == 0 {
				winners = append(winners, ph.player)
			}
		}

		winAmount := g.Pot / len(winners)
		for _, winner := range winners {
			winner.Chips += winAmount
		}

		fmt.Printf("\n\n获胜者: ")
		for i, w := range winners {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(w.Name)
		}
		fmt.Printf(" 每人赢得 %d 筹码！(牌型: %v)\n", winAmount, bestHand.Rank)
	}

	g.ButtonPos = (g.ButtonPos + 1) % len(g.Players)
}

func (g *Game) resetBets() {
	for _, p := range g.Players {
		p.Bet = 0
	}
	g.CurrentBet = 0
}

func (g *Game) ShowPlayerStatus(showHoleCards bool) {
	fmt.Println()
	for _, p := range g.Players {
		if showHoleCards || p.Name == "你" {
			fmt.Println(p.String())
		} else {
			if p.Folded {
				fmt.Printf("玩家%d (%s): 已弃牌\n", p.ID, p.Name)
			} else {
				fmt.Printf("玩家%d (%s): 筹码: %d | 下注: %d | 底牌: [??] [??]\n", p.ID, p.Name, p.Chips, p.Bet)
			}
		}
	}
}
