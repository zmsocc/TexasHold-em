package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

type WebGUIGame struct {
	game             *Game
	mu               sync.Mutex
	actionChan       chan ActionData
	firstGame        bool
	currentPhase     string
	currentPlayerName string
	waitingForInput  bool
	canCheck         bool
	canCall          bool
	canRaise         bool
	minRaise         int
	maxRaise         int
	callAmount       int
	message          string
	gameOver         bool
	winnerText       string
	askContinue      bool
	askShuffle       bool
}

type GameState struct {
	Players        []PlayerState `json:"players"`
	CommunityCards []string      `json:"communityCards"`
	Pot            int           `json:"pot"`
	Phase          string        `json:"phase"`
	CurrentPlayer  string        `json:"currentPlayer"`
	Message        string        `json:"message"`
	GameOver       bool          `json:"gameOver"`
	WinnerText     string        `json:"winnerText"`
	CanCheck       bool          `json:"canCheck"`
	CanCall        bool          `json:"canCall"`
	CanRaise       bool          `json:"canRaise"`
	MinRaise       int           `json:"minRaise"`
	MaxRaise       int           `json:"maxRaise"`
	CallAmount     int           `json:"callAmount"`
	WaitingForInput bool         `json:"waitingForInput"`
	AskContinue    bool          `json:"askContinue"`
	AskShuffle     bool          `json:"askShuffle"`
}

type ActionData struct {
	Action Action
	Amount int
}

type PlayerState struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Chips     int      `json:"chips"`
	Cards     []string `json:"cards"`
	Bet       int      `json:"bet"`
	Folded    bool     `json:"folded"`
	AllIn     bool     `json:"allIn"`
	IsHuman   bool     `json:"isHuman"`
	IsCurrent bool     `json:"isCurrent"`
}

func NewWebGUIGame() *WebGUIGame {
	return &WebGUIGame{
		actionChan: make(chan ActionData, 1),
		firstGame:  true,
	}
}

func (g *WebGUIGame) getGameState() *GameState {
	state := &GameState{
		Players:         make([]PlayerState, 0, len(g.game.Players)),
		CommunityCards:  make([]string, 0, len(g.game.CommunityCards)),
		Pot:             g.game.Pot,
		Phase:           g.currentPhase,
		CurrentPlayer:   g.currentPlayerName,
		Message:         g.message,
		GameOver:        g.gameOver,
		WinnerText:      g.winnerText,
		CanCheck:        g.canCheck,
		CanCall:         g.canCall,
		CanRaise:        g.canRaise,
		MinRaise:        g.minRaise,
		MaxRaise:        g.maxRaise,
		CallAmount:      g.callAmount,
		WaitingForInput: g.waitingForInput,
		AskContinue:     g.askContinue,
		AskShuffle:      g.askShuffle,
	}

	for _, p := range g.game.Players {
		ps := PlayerState{
			ID:        p.ID,
			Name:      p.Name,
			Chips:     p.Chips,
			Bet:       p.Bet,
			Folded:    p.Folded,
			AllIn:     p.AllIn,
			IsHuman:   p.Name == "你",
			IsCurrent: p.Name == g.currentPlayerName,
			Cards:     make([]string, 0, 2),
		}
		
		if p.Name == "你" {
			for _, c := range p.HoleCards {
				ps.Cards = append(ps.Cards, c.ImageFileName())
			}
		} else if (!p.Folded && g.currentPhase == "摊牌") {
			for _, c := range p.HoleCards {
				ps.Cards = append(ps.Cards, c.ImageFileName())
			}
		} else {
			ps.Cards = append(ps.Cards, "bm.png", "bm.png")
		}
		state.Players = append(state.Players, ps)
	}

	for _, c := range g.game.CommunityCards {
		state.CommunityCards = append(state.CommunityCards, c.ImageFileName())
	}

	for len(state.CommunityCards) < 5 {
		state.CommunityCards = append(state.CommunityCards, "bm.png")
	}

	return state
}

func (g *WebGUIGame) Run() {
	http.HandleFunc("/", g.handleIndex)
	http.HandleFunc("/state", g.handleState)
	http.HandleFunc("/action", g.handleAction)
	http.Handle("/puke-img/", http.StripPrefix("/puke-img/", http.FileServer(http.Dir("puke-img"))))
	http.HandleFunc("/bm.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "bm.png")
	})
	http.HandleFunc("/zhuomian.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "zhuomian.png")
	})

	fmt.Println("=====================================")
	fmt.Println("   德州扑克 Web GUI 已启动！")
	fmt.Println("=====================================")
	fmt.Println("请在浏览器中打开: http://localhost:8080")
	fmt.Println("=====================================")

	http.ListenAndServe(":8080", nil)
}

func (g *WebGUIGame) handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func (g *WebGUIGame) handleState(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()

	var state *GameState
	if g.game == nil {
		state = &GameState{
			Message: "点击开始游戏",
		}
	} else {
		state = g.getGameState()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (g *WebGUIGame) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var actionReq struct {
		Action string `json:"action"`
		Amount int    `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&actionReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var action Action
	switch actionReq.Action {
	case "fold":
		action = Fold
	case "check":
		action = Check
	case "call":
		action = Call
	case "raise":
		action = Raise
	case "allin":
		action = AllIn
	case "start":
		g.mu.Lock()
		if g.game == nil {
			rand.Seed(time.Now().UnixNano())
			numPlayers := rand.Intn(9) + 2
			g.game = NewGame(numPlayers, 100)
			if g.firstGame {
				g.shufflePlayers()
				g.firstGame = false
			}
			go g.playHand()
		}
		g.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	case "continue":
		go func() {
			g.actionChan <- ActionData{Action: Check, Amount: -1}
		}()
		w.WriteHeader(http.StatusOK)
		return
	case "shuffle":
		g.mu.Lock()
		g.shufflePlayers()
		g.mu.Unlock()
		go func() {
			g.actionChan <- ActionData{Action: Check, Amount: -2}
		}()
		w.WriteHeader(http.StatusOK)
		return
	case "noContinue":
		w.WriteHeader(http.StatusOK)
		return
	case "timeout":
		action = Fold
	default:
		http.Error(w, "Invalid action", http.StatusBadRequest)
		return
	}

	g.actionChan <- ActionData{Action: action, Amount: actionReq.Amount}
	w.WriteHeader(http.StatusOK)
}

func (g *WebGUIGame) shufflePlayers() {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(g.game.Players), func(i, j int) {
		g.game.Players[i], g.game.Players[j] = g.game.Players[j], g.game.Players[i]
	})
	for i, p := range g.game.Players {
		p.ID = i + 1
	}
}

func (g *WebGUIGame) playHand() {
	g.mu.Lock()
	
	g.game.NewHand()
	g.currentPhase = "翻牌前"
	g.message = "游戏开始！"
	g.gameOver = false
	g.winnerText = ""
	g.askContinue = false
	g.askShuffle = false

	g.game.DealHoleCards()
	g.currentPhase = "翻牌前"
	g.mu.Unlock()

	time.Sleep(500 * time.Millisecond)

	g.mu.Lock()
	g.game.PostBlinds()
	g.currentPhase = "翻牌前"
	g.mu.Unlock()

	g.runBettingRound("翻牌前")
	
	g.mu.Lock()
	if g.game.GameOver() {
		g.mu.Unlock()
		g.showdown()
		return
	}
	g.mu.Unlock()

	g.mu.Lock()
	g.game.DealFlop()
	g.currentPhase = "翻牌圈"
	g.message = ""
	g.mu.Unlock()

	g.runBettingRound("翻牌圈")
	
	g.mu.Lock()
	if g.game.GameOver() {
		g.mu.Unlock()
		g.showdown()
		return
	}
	g.mu.Unlock()

	g.mu.Lock()
	g.game.DealTurn()
	g.currentPhase = "转牌圈"
	g.message = ""
	g.mu.Unlock()

	g.runBettingRound("转牌圈")
	
	g.mu.Lock()
	if g.game.GameOver() {
		g.mu.Unlock()
		g.showdown()
		return
	}
	g.mu.Unlock()

	g.mu.Lock()
	g.game.DealRiver()
	g.currentPhase = "河牌圈"
	g.message = ""
	g.mu.Unlock()

	g.runBettingRound("河牌圈")

	g.showdown()
}

func (g *WebGUIGame) runBettingRound(phase string) {
	g.mu.Lock()
	startPos := g.game.ButtonPos + 1
	if phase == "翻牌前" {
		startPos = g.game.ButtonPos + 3
	}

	activePlayers := g.game.getActivePlayers()
	if len(activePlayers) <= 1 {
		g.mu.Unlock()
		return
	}

	lastRaisePos := -1
	currentPos := startPos % len(g.game.Players)
	betCount := 0
	g.mu.Unlock()

	for {
		g.mu.Lock()
		player := g.game.Players[currentPos]

		if player.Folded || player.AllIn {
			currentPos = (currentPos + 1) % len(g.game.Players)
			g.mu.Unlock()
			continue
		}

		if lastRaisePos == currentPos && betCount > 0 {
			g.mu.Unlock()
			break
		}

		g.currentPhase = phase
		g.currentPlayerName = player.Name
		
		var action Action
		var raiseAmount int

		if player.Name == "你" {
			g.message = "轮到你了！"
			g.waitingForInput = true
			g.canCheck = g.game.CurrentBet == player.Bet
			g.canCall = g.game.CurrentBet > player.Bet && player.Chips > 0
			g.minRaise = g.game.BigBlind
			if g.game.CurrentBet > 0 {
				g.minRaise = g.game.CurrentBet
			}
			g.maxRaise = player.Chips - (g.game.CurrentBet - player.Bet)
			g.canRaise = player.Chips > (g.game.CurrentBet - player.Bet + g.minRaise)
			g.callAmount = g.game.CurrentBet - player.Bet

			timeoutChan := make(chan bool, 1)
			go func() {
				time.Sleep(60 * time.Second)
				timeoutChan <- true
			}()

			g.mu.Unlock()
			select {
			case actionData := <-g.actionChan:
				action = actionData.Action
				raiseAmount = actionData.Amount
			case <-timeoutChan:
				action = Fold
			}
			g.mu.Lock()
		} else {
			g.message = fmt.Sprintf("轮到 %s 思考中...", player.Name)
			g.mu.Unlock()
			time.Sleep(3000 * time.Millisecond)
			g.mu.Lock()
			action, raiseAmount = g.game.getAIAction(player)
		}

		g.game.executeAction(player, action, raiseAmount)
		g.message = fmt.Sprintf("%s 选择了 %s", player.Name, action)
		g.waitingForInput = false

		if action == Raise || action == AllIn {
			lastRaisePos = currentPos
			betCount++
		} else if action == Fold {
			activePlayers = g.game.getActivePlayers()
			if len(activePlayers) <= 1 {
				g.mu.Unlock()
				break
			}
		}

		g.mu.Unlock()
		time.Sleep(500 * time.Millisecond)
		currentPos = (currentPos + 1) % len(g.game.Players)
	}

	g.mu.Lock()
	g.game.resetBets()
	g.message = ""
	g.waitingForInput = false
	g.currentPlayerName = ""
	g.mu.Unlock()
}

func (g *WebGUIGame) showdown() {
	g.mu.Lock()
	g.currentPhase = "摊牌"

	activePlayers := g.game.getActivePlayers()

	var winnerText string
	if len(activePlayers) == 1 {
		winner := activePlayers[0]
		winner.Chips += g.game.Pot
		winnerText = fmt.Sprintf("%s 获胜！赢得 %d 筹码！", winner.Name, g.game.Pot)
	} else {
		type playerHand struct {
			player *Player
			hand   Hand
		}

		var hands []playerHand

		for _, p := range activePlayers {
			allCards := append(p.HoleCards, g.game.CommunityCards...)
			hand := EvaluateHand(allCards)
			hands = append(hands, playerHand{p, hand})
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

		winAmount := g.game.Pot / len(winners)
		for _, winner := range winners {
			winner.Chips += winAmount
		}

		winnerText = "获胜者: "
		for i, w := range winners {
			if i > 0 {
				winnerText += ", "
			}
			winnerText += w.Name
		}
		winnerText += fmt.Sprintf(" 每人赢得 %d 筹码！(牌型: %v)", winAmount, bestHand.Rank)
	}

	g.winnerText = winnerText
	g.gameOver = true
	g.askContinue = true
	g.currentPlayerName = ""

	g.game.ButtonPos = (g.game.ButtonPos + 1) % len(g.game.Players)
	g.mu.Unlock()

	actionData := <-g.actionChan
	
	g.mu.Lock()
	if actionData.Amount == -1 {
		g.askContinue = false
		g.askShuffle = true
		g.gameOver = false
		g.mu.Unlock()
		
		actionData = <-g.actionChan
		
		g.mu.Lock()
		if actionData.Amount == -2 {
		}
		go g.playHand()
	}
	g.mu.Unlock()
}
