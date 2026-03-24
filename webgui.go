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
	game              *Game
	mu                sync.Mutex
	actionChan        chan ActionData
	firstGame         bool
	currentPhase      string
	currentPlayerName string
	waitingForInput   bool
	canCheck          bool
	canCall           bool
	canRaise          bool
	minRaise          int
	maxRaise          int
	callAmount        int
	message           string
	gameOver          bool
	winnerText        string
	askContinue       bool
	askShuffle        bool
	showdownCards     []string
	winnerHandRank    string
	winnerName        string
	currentUser       *User
}

type GameState struct {
	Players         []PlayerState `json:"players"`
	CommunityCards  []string      `json:"communityCards"`
	Pot             int           `json:"pot"`
	Phase           string        `json:"phase"`
	CurrentPlayer   string        `json:"currentPlayer"`
	Message         string        `json:"message"`
	GameOver        bool          `json:"gameOver"`
	WinnerText      string        `json:"winnerText"`
	CanCheck        bool          `json:"canCheck"`
	CanCall         bool          `json:"canCall"`
	CanRaise        bool          `json:"canRaise"`
	MinRaise        int           `json:"minRaise"`
	MaxRaise        int           `json:"maxRaise"`
	CallAmount      int           `json:"callAmount"`
	WaitingForInput bool          `json:"waitingForInput"`
	AskContinue     bool          `json:"askContinue"`
	AskShuffle      bool          `json:"askShuffle"`
	ShowdownCards   []string      `json:"showdownCards"`
	WinnerHandRank  string        `json:"winnerHandRank"`
	WinnerName      string        `json:"winnerName"`
}

type ActionData struct {
	Action Action
	Amount int
}

type PlayerState struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Chips        int      `json:"chips"`
	Cards        []string `json:"cards"`
	Bet          int      `json:"bet"`
	Folded       bool     `json:"folded"`
	AllIn        bool     `json:"allIn"`
	IsHuman      bool     `json:"isHuman"`
	IsCurrent    bool     `json:"isCurrent"`
	IsDealer     bool     `json:"isDealer"`
	IsSmallBlind bool     `json:"isSmallBlind"`
	IsBigBlind   bool     `json:"isBigBlind"`
}

func NewWebGUIGame() *WebGUIGame {
	return &WebGUIGame{
		actionChan: make(chan ActionData, 1),
		firstGame:  true,
	}
}

func getRankOrder(rank Rank) int {
	switch rank {
	case Ace:
		return 14
	case King:
		return 13
	case Queen:
		return 12
	case Jack:
		return 11
	case Ten:
		return 10
	case Nine:
		return 9
	case Eight:
		return 8
	case Seven:
		return 7
	case Six:
		return 6
	case Five:
		return 5
	case Four:
		return 4
	case Three:
		return 3
	case Two:
		return 2
	default:
		return 0
	}
}

func getHandRankName(rank HandRank) string {
	switch rank {
	case RoyalFlush:
		return "皇家同花顺"
	case StraightFlush:
		return "同花顺"
	case FourOfKind:
		return "四条"
	case FullHouse:
		return "满堂红"
	case Flush:
		return "同花"
	case Straight:
		return "顺子"
	case ThreeOfKind:
		return "三条"
	case TwoPair:
		return "两对"
	case OnePair:
		return "一对"
	case HighCard:
		return "高牌"
	default:
		return ""
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
		ShowdownCards:   g.showdownCards,
		WinnerHandRank:  g.winnerHandRank,
		WinnerName:      g.winnerName,
	}

	numPlayers := len(g.game.Players)
	dealerPos := g.game.ButtonPos
	sbPos := (dealerPos + 1) % numPlayers
	bbPos := (dealerPos + 2) % numPlayers

	humanPlayerName := "你"
	if g.currentUser != nil {
		humanPlayerName = g.currentUser.Nickname
	}

	for i, p := range g.game.Players {
		ps := PlayerState{
			ID:           p.ID,
			Name:         p.Name,
			Chips:        p.Chips,
			Bet:          p.Bet,
			Folded:       p.Folded,
			AllIn:        p.AllIn,
			IsHuman:      p.Name == humanPlayerName,
			IsCurrent:    p.Name == g.currentPlayerName,
			IsDealer:     i == dealerPos,
			IsSmallBlind: i == sbPos,
			IsBigBlind:   i == bbPos,
			Cards:        make([]string, 0, 2),
		}

		if p.Name == humanPlayerName {
			for _, c := range p.HoleCards {
				ps.Cards = append(ps.Cards, c.ImageFileName())
			}
		} else if !p.Folded && g.currentPhase == "摊牌" {
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
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if g.checkAuth(r) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		g.handleIndex(w, r)
	})
	http.HandleFunc("/login", g.handleLoginPage)
	http.HandleFunc("/register", g.handleRegisterPage)
	http.HandleFunc("/api/login", g.handleLoginAPI)
	http.HandleFunc("/api/register", g.handleRegisterAPI)
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
	user := g.checkAuth(r)
	if user != nil {
		g.mu.Lock()
		g.currentUser = user
		g.mu.Unlock()
	}
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func (g *WebGUIGame) handleState(w http.ResponseWriter, r *http.Request) {
	user := g.checkAuth(r)
	if user != nil {
		g.mu.Lock()
		g.currentUser = user
		g.mu.Unlock()
	}

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

	user := g.checkAuth(r)
	if user != nil {
		g.mu.Lock()
		g.currentUser = user
		g.mu.Unlock()
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
			humanName := ""
			if g.currentUser != nil {
				humanName = g.currentUser.Nickname
			}
			g.game = NewGame(numPlayers, 100, humanName)
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

func (g *WebGUIGame) shouldSkipBettingRounds() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	activePlayers := g.game.getActivePlayers()
	for _, p := range activePlayers {
		if !p.AllIn {
			return false
		}
	}
	return len(activePlayers) > 1
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
	g.showdownCards = nil
	g.winnerHandRank = ""
	g.winnerName = ""

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

	if g.shouldSkipBettingRounds() {
		g.mu.Lock()
		g.message = "所有玩家全下，直接发牌至河牌！"
		g.mu.Unlock()
		time.Sleep(1000 * time.Millisecond)
	} else {
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

		if g.shouldSkipBettingRounds() {
			g.mu.Lock()
			g.message = "所有玩家全下，直接发牌至河牌！"
			g.mu.Unlock()
			time.Sleep(1000 * time.Millisecond)
		} else {
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

			if !g.shouldSkipBettingRounds() {
				g.mu.Lock()
				g.game.DealRiver()
				g.currentPhase = "河牌圈"
				g.message = ""
				g.mu.Unlock()

				g.runBettingRound("河牌圈")
			}
		}
	}

	g.mu.Lock()
	if len(g.game.CommunityCards) < 3 {
		g.game.DealFlop()
	}
	if len(g.game.CommunityCards) < 4 {
		g.game.DealTurn()
	}
	if len(g.game.CommunityCards) < 5 {
		g.game.DealRiver()
	}
	g.currentPhase = "河牌圈"
	g.mu.Unlock()

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
	firstRoundComplete := false
	checkCount := 0
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

		humanPlayerName := "你"
		if g.currentUser != nil {
			humanPlayerName = g.currentUser.Nickname
		}

		if player.Name == humanPlayerName {
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
			checkCount = 0
			firstRoundComplete = true
		} else if action == Check {
			checkCount++
		} else if action == Fold {
			activePlayers = g.game.getActivePlayers()
			if len(activePlayers) <= 1 {
				g.mu.Unlock()
				break
			}
		} else if action == Call {
			firstRoundComplete = true
		}

		activePlayers = g.game.getActivePlayers()
		activeNonAllInCount := 0
		for _, p := range activePlayers {
			if !p.AllIn {
				activeNonAllInCount++
			}
		}

		if activeNonAllInCount <= 1 {
			g.mu.Unlock()
			break
		}

		if firstRoundComplete && lastRaisePos == -1 && checkCount >= activeNonAllInCount {
			g.mu.Unlock()
			break
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
	var bestHand Hand
	var winners []*Player
	var winnerCards []Card

	if len(activePlayers) == 1 {
		winner := activePlayers[0]
		winner.Chips += g.game.Pot
		winnerText = fmt.Sprintf("%s 获胜！赢得 %d 筹码！", winner.Name, g.game.Pot)
		winners = []*Player{winner}
		winnerCards = winner.HoleCards
		allCards := append(winner.HoleCards, g.game.CommunityCards...)
		if len(allCards) >= 5 {
			bestHand = EvaluateHand(allCards)
		} else {
			bestHand = Hand{Cards: winner.HoleCards, Rank: HighCard}
		}
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

		bestHand = hands[0].hand
		winners = []*Player{hands[0].player}
		winnerCards = hands[0].hand.Cards

		for _, ph := range hands {
			cmp := CompareHands(ph.hand, bestHand)
			if cmp > 0 {
				bestHand = ph.hand
				winners = []*Player{ph.player}
				winnerCards = ph.hand.Cards
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

	sortedCards := make([]Card, len(winnerCards))
	copy(sortedCards, winnerCards)
	for i := 0; i < len(sortedCards); i++ {
		for j := i + 1; j < len(sortedCards); j++ {
			if getRankOrder(sortedCards[i].Rank) < getRankOrder(sortedCards[j].Rank) {
				sortedCards[i], sortedCards[j] = sortedCards[j], sortedCards[i]
			}
		}
	}

	g.showdownCards = make([]string, 0, 5)
	for _, c := range sortedCards {
		g.showdownCards = append(g.showdownCards, c.ImageFileName())
	}

	g.winnerHandRank = getHandRankName(bestHand.Rank)
	g.winnerText = winnerText
	g.gameOver = true
	g.askContinue = false
	g.currentPlayerName = ""
	if len(winners) > 0 {
		g.winnerName = winners[0].Name
		for i := 1; i < len(winners); i++ {
			g.winnerName += "、" + winners[i].Name
		}
	} else {
		g.winnerName = ""
	}

	g.game.ButtonPos = (g.game.ButtonPos + 1) % len(g.game.Players)
	g.mu.Unlock()

	time.Sleep(3000 * time.Millisecond)

	g.mu.Lock()
	g.askContinue = true
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

type User struct {
	ID       int
	Nickname string
	Email    string
	Password string
}

var (
	users      = make(map[string]*User)
	usersByID  = make(map[int]*User)
	nextUserID = 1
	sessions   = make(map[string]*User)
	sessionMu  sync.Mutex
)

func (g *WebGUIGame) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/login.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func (g *WebGUIGame) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/register.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func (g *WebGUIGame) handleLoginAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求",
		})
		return
	}

	var user *User
	var err error

	if db != nil {
		user, err = GetUserByNickname(req.Username)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "数据库错误",
			})
			return
		}
		if user == nil {
			user, err = GetUserByEmail(req.Username)
			if err != nil {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "数据库错误",
				})
				return
			}
		}
	} else {
		sessionMu.Lock()
		defer sessionMu.Unlock()

		if u, ok := users[req.Username]; ok && u.Password == req.Password {
			user = u
		} else {
			for _, u := range users {
				if u.Email == req.Username && u.Password == req.Password {
					user = u
					break
				}
			}
		}
	}

	if user == nil || user.Password != req.Password {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "账号或密码错误",
		})
		return
	}

	sessionMu.Lock()
	sessionID := fmt.Sprintf("%d", time.Now().UnixNano())
	sessions[sessionID] = user
	sessionMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400,
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

func (g *WebGUIGame) handleRegisterAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Nickname string `json:"nickname"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求",
		})
		return
	}

	if db != nil {
		exists, err := CheckNicknameExists(req.Nickname)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "数据库错误",
			})
			return
		}
		if exists {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "昵称已存在",
			})
			return
		}

		exists, err = CheckEmailExists(req.Email)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "数据库错误",
			})
			return
		}
		if exists {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "邮箱已被注册",
			})
			return
		}

		_, err = CreateUser(req.Nickname, req.Email, req.Password)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "创建用户失败",
			})
			return
		}
	} else {
		sessionMu.Lock()
		defer sessionMu.Unlock()

		if _, ok := users[req.Nickname]; ok {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "昵称已存在",
			})
			return
		}

		for _, u := range users {
			if u.Email == req.Email {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "邮箱已被注册",
				})
				return
			}
		}

		user := &User{
			ID:       nextUserID,
			Nickname: req.Nickname,
			Email:    req.Email,
			Password: req.Password,
		}
		nextUserID++
		users[req.Nickname] = user
		usersByID[user.ID] = user
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

func (g *WebGUIGame) checkAuth(r *http.Request) *User {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return nil
	}

	sessionMu.Lock()
	defer sessionMu.Unlock()

	return sessions[cookie.Value]
}
