package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// PlayerAction 玩家操作
type PlayerAction struct {
	UserID int
	Action string
	Amount int
}

// MultiplayerGame 多人游戏实例
type MultiplayerGame struct {
	RoomID         string
	Players        map[int]*MultiplayerPlayer // user_id -> player
	PlayerOrder    []int                      // 玩家顺序（user_id列表）
	Game           *Game
	Status         string // waiting, dealing, preflop, flop, turn, river, showdown, finished
	CurrentPos     int    // 当前操作玩家索引
	DealerPos      int    // 庄家位置
	Pot            int
	CurrentBet     int
	LastRaisePos   int
	CommunityCards []Card
	HoleCards      map[int][]Card // user_id -> 手牌
	ActionLog      []GameAction
	ActionChan     chan PlayerAction
	BroadcastChan  chan GameEvent
	mu             sync.RWMutex
	timer          *time.Timer
}

// MultiplayerPlayer 多人游戏玩家
type MultiplayerPlayer struct {
	UserID   int
	Nickname string
	SeatIndex int
	Chips    int
	Bet      int
	Folded   bool
	AllIn    bool
	IsOnline bool
	Conn     *Client
}

// GameAction 游戏动作记录
type GameAction struct {
	UserID    int       `json:"user_id"`
	Nickname  string    `json:"nickname"`
	Action    string    `json:"action"`
	Amount    int       `json:"amount"`
	Timestamp time.Time `json:"timestamp"`
}

// GameEvent 游戏事件
type GameEvent struct {
	Type   string      `json:"type"`
	Data   interface{} `json:"data"`
}

// MultiplayerGameManager 多人游戏管理器
type MultiplayerGameManager struct {
	games map[string]*MultiplayerGame
	mu    sync.RWMutex
}

var multiplayerManager = &MultiplayerGameManager{
	games: make(map[string]*MultiplayerGame),
}

// CreateGame 创建多人游戏
func (mgm *MultiplayerGameManager) CreateGame(roomID string, roomPlayers []*RoomPlayer) (*MultiplayerGame, error) {
	mgm.mu.Lock()
	defer mgm.mu.Unlock()

	if _, exists := mgm.games[roomID]; exists {
		return nil, fmt.Errorf("游戏已存在")
	}

	game := &MultiplayerGame{
		RoomID:        roomID,
		Players:       make(map[int]*MultiplayerPlayer),
		PlayerOrder:   make([]int, 0, len(roomPlayers)),
		Status:        "waiting",
		HoleCards:     make(map[int][]Card),
		ActionLog:     make([]GameAction, 0),
		ActionChan:    make(chan PlayerAction, 100),
		BroadcastChan: make(chan GameEvent, 100),
	}

	// 按座位顺序添加玩家
	for _, rp := range roomPlayers {
		game.Players[rp.UserID] = &MultiplayerPlayer{
			UserID:    rp.UserID,
			Nickname:  rp.Nickname,
			SeatIndex: rp.SeatIndex,
			Chips:     1000, // 初始筹码
			IsOnline:  true,
		}
		game.PlayerOrder = append(game.PlayerOrder, rp.UserID)
	}

	mgm.games[roomID] = game

	// 启动游戏协程
	go game.run()
	go game.broadcastLoop()

	return game, nil
}

// GetGame 获取游戏
func (mgm *MultiplayerGameManager) GetGame(roomID string) *MultiplayerGame {
	mgm.mu.RLock()
	defer mgm.mu.RUnlock()
	return mgm.games[roomID]
}

// RemoveGame 移除游戏
func (mgm *MultiplayerGameManager) RemoveGame(roomID string) {
	mgm.mu.Lock()
	defer mgm.mu.Unlock()

	if game, exists := mgm.games[roomID]; exists {
		game.Stop()
		delete(mgm.games, roomID)
	}
}

// Join 玩家加入游戏
func (mg *MultiplayerGame) Join(userID int, client *Client) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	player, exists := mg.Players[userID]
	if !exists {
		return fmt.Errorf("玩家不在游戏中")
	}

	player.Conn = client
	player.IsOnline = true

	// 发送当前游戏状态
	mg.sendGameState(userID)

	return nil
}

// SubmitAction 提交操作
func (mg *MultiplayerGame) SubmitAction(userID int, action string, amount int) error {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	// 检查是否是当前玩家
	currentUserID := mg.PlayerOrder[mg.CurrentPos]
	if currentUserID != userID {
		return fmt.Errorf("不是您的回合")
	}

	// 提交到操作通道
	select {
	case mg.ActionChan <- PlayerAction{UserID: userID, Action: action, Amount: amount}:
		return nil
	default:
		return fmt.Errorf("操作通道已满")
	}
}

// run 游戏主循环
func (mg *MultiplayerGame) run() {
	// 等待所有玩家加入
	mg.waitForPlayers()

	// 开始新一局
	mg.startNewHand()

	// 游戏主循环
	for mg.Status != "finished" {
		switch mg.Status {
		case "preflop":
			mg.runBettingRound("preflop")
			if !mg.isGameOver() {
				mg.dealFlop()
				mg.Status = "flop"
			}
		case "flop":
			mg.runBettingRound("flop")
			if !mg.isGameOver() {
				mg.dealTurn()
				mg.Status = "turn"
			}
		case "turn":
			mg.runBettingRound("turn")
			if !mg.isGameOver() {
				mg.dealRiver()
				mg.Status = "river"
			}
		case "river":
			mg.runBettingRound("river")
			mg.Status = "showdown"
		case "showdown":
			mg.runShowdown()
			mg.Status = "finished"
		}
	}

	// 游戏结束，广播结果
	mg.broadcast(GameEvent{
		Type: "game_over",
		Data: mg.getGameResult(),
	})
}

// waitForPlayers 等待玩家加入
func (mg *MultiplayerGame) waitForPlayers() {
	// 给玩家5秒时间加入
	time.Sleep(5 * time.Second)
}

// startNewHand 开始新一局
func (mg *MultiplayerGame) startNewHand() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	// 重置状态
	mg.Status = "preflop"
	mg.Pot = 0
	mg.CurrentBet = 0
	mg.LastRaisePos = -1
	mg.CommunityCards = make([]Card, 0, 5)
	mg.ActionLog = make([]GameAction, 0)

	// 重置玩家状态
	for _, p := range mg.Players {
		p.Bet = 0
		p.Folded = false
		p.AllIn = false
	}

	// 创建牌组并发牌
	deck := NewDeck()
	deck.Shuffle()

	for userID := range mg.Players {
		mg.HoleCards[userID] = []Card{deck.Deal(), deck.Deal()}
	}

	// 保存剩余牌用于发公共牌
	mg.Game = &Game{Deck: deck}

	// 确定庄家位置
	mg.DealerPos = 0
	mg.CurrentPos = mg.getNextActivePosition(mg.DealerPos, 3) // 从大盲注后一位开始

	// 收取盲注
	mg.postBlinds()

	// 广播游戏开始
	mg.broadcast(GameEvent{
		Type: "hand_started",
		Data: map[string]interface{}{
			"dealer_pos": mg.DealerPos,
			"pot":        mg.Pot,
		},
	})

	// 给每个玩家发送自己的手牌
	for userID, cards := range mg.HoleCards {
		mg.sendToPlayer(userID, GameEvent{
			Type: "hole_cards",
			Data: map[string]interface{}{
				"cards": []string{cards[0].ImageFileName(), cards[1].ImageFileName()},
			},
		})
	}
}

// postBlinds 收取盲注
func (mg *MultiplayerGame) postBlinds() {
	numPlayers := len(mg.PlayerOrder)
	sbPos := (mg.DealerPos + 1) % numPlayers
	bbPos := (mg.DealerPos + 2) % numPlayers

	sbUserID := mg.PlayerOrder[sbPos]
	bbUserID := mg.PlayerOrder[bbPos]

	// 小盲注 10
	sbPlayer := mg.Players[sbUserID]
	sbAmount := 10
	if sbPlayer.Chips < sbAmount {
		sbAmount = sbPlayer.Chips
		sbPlayer.AllIn = true
	}
	sbPlayer.Chips -= sbAmount
	sbPlayer.Bet = sbAmount
	mg.Pot += sbAmount

	// 大盲注 20
	bbPlayer := mg.Players[bbUserID]
	bbAmount := 20
	if bbPlayer.Chips < bbAmount {
		bbAmount = bbPlayer.Chips
		bbPlayer.AllIn = true
	}
	bbPlayer.Chips -= bbAmount
	bbPlayer.Bet = bbAmount
	mg.Pot += bbAmount

	mg.CurrentBet = bbAmount

	// 记录动作
	mg.ActionLog = append(mg.ActionLog, GameAction{
		UserID:    sbUserID,
		Nickname:  sbPlayer.Nickname,
		Action:    "small_blind",
		Amount:    sbAmount,
		Timestamp: time.Now(),
	})
	mg.ActionLog = append(mg.ActionLog, GameAction{
		UserID:    bbUserID,
		Nickname:  bbPlayer.Nickname,
		Action:    "big_blind",
		Amount:    bbAmount,
		Timestamp: time.Now(),
	})
}

// runBettingRound 运行下注轮
func (mg *MultiplayerGame) runBettingRound(phase string) {
	lastRaisePos := -1
	betCount := 0

	for {
		mg.mu.RLock()
		currentUserID := mg.PlayerOrder[mg.CurrentPos]
		player := mg.Players[currentUserID]
		mg.mu.RUnlock()

		// 跳过已弃牌或全下的玩家
		if player.Folded || player.AllIn {
			mg.moveToNextPlayer()
			continue
		}

		// 检查是否完成一轮
		if lastRaisePos == mg.CurrentPos && betCount > 0 {
			break
		}

		// 广播轮到该玩家
		mg.broadcast(GameEvent{
			Type: "player_turn",
			Data: map[string]interface{}{
				"user_id":   currentUserID,
				"nickname":  player.Nickname,
				"time_left": 60,
				"phase":     phase,
			},
		})

		// 等待玩家操作（60秒超时）
		action, amount := mg.waitForAction(currentUserID, 60)

		// 执行操作
		mg.executeAction(currentUserID, action, amount)

		// 广播操作结果
		mg.broadcast(GameEvent{
			Type: "action_result",
			Data: map[string]interface{}{
				"user_id":  currentUserID,
				"nickname": player.Nickname,
				"action":   action,
				"amount":   amount,
				"pot":      mg.Pot,
			},
		})

		// 检查是否只剩一个玩家
		if mg.getActivePlayerCount() <= 1 {
			return
		}

		// 更新位置
		if action == "raise" || action == "allin" {
			lastRaisePos = mg.CurrentPos
			betCount++
		}

		mg.moveToNextPlayer()
	}

	// 重置下注
	mg.resetBets()
}

// waitForAction 等待玩家操作
func (mg *MultiplayerGame) waitForAction(userID int, timeoutSeconds int) (string, int) {
	timeout := time.After(time.Duration(timeoutSeconds) * time.Second)

	for {
		select {
		case action := <-mg.ActionChan:
			if action.UserID == userID {
				return action.Action, action.Amount
			}
			// 不是当前玩家的操作，继续等待
		case <-timeout:
			return "fold", 0
		}
	}
}

// executeAction 执行操作
func (mg *MultiplayerGame) executeAction(userID int, action string, amount int) {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	player := mg.Players[userID]
	callAmount := mg.CurrentBet - player.Bet

	switch action {
	case "fold":
		player.Folded = true

	case "check":
		// 过牌，不需要操作

	case "call":
		if callAmount > 0 {
			if player.Chips <= callAmount {
				// 全下跟注
				mg.Pot += player.Chips
				player.Bet += player.Chips
				player.AllIn = true
				player.Chips = 0
			} else {
				mg.Pot += callAmount
				player.Chips -= callAmount
				player.Bet += callAmount
			}
		}

	case "raise":
		// 跟注+加注
		if callAmount > 0 {
			if player.Chips <= callAmount {
				// 筹码不够跟注，全下
				mg.Pot += player.Chips
				player.Bet += player.Chips
				player.AllIn = true
				player.Chips = 0
			} else {
				player.Chips -= callAmount
				player.Bet += callAmount
			}
		}

		// 额外加注
		if amount > 0 && player.Chips > 0 {
			if amount >= player.Chips {
				// 全下
				mg.Pot += player.Chips
				player.Bet += player.Chips
				player.AllIn = true
				player.Chips = 0
			} else {
				mg.Pot += amount
				player.Chips -= amount
				player.Bet += amount
			}
		}
		mg.CurrentBet = player.Bet

	case "allin":
		if player.Chips > 0 {
			mg.Pot += player.Chips
			player.Bet += player.Chips
			if player.Bet > mg.CurrentBet {
				mg.CurrentBet = player.Bet
			}
			player.AllIn = true
			player.Chips = 0
		}
	}

	// 记录动作
	mg.ActionLog = append(mg.ActionLog, GameAction{
		UserID:    userID,
		Nickname:  player.Nickname,
		Action:    action,
		Amount:    amount,
		Timestamp: time.Now(),
	})
}

// dealFlop 发翻牌
func (mg *MultiplayerGame) dealFlop() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	for i := 0; i < 3; i++ {
		mg.CommunityCards = append(mg.CommunityCards, mg.Game.Deck.Deal())
	}

	mg.broadcast(GameEvent{
		Type: "community_cards",
		Data: map[string]interface{}{
			"cards": mg.getCommunityCardImages(),
			"phase": "flop",
		},
	})
}

// dealTurn 发转牌
func (mg *MultiplayerGame) dealTurn() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	mg.CommunityCards = append(mg.CommunityCards, mg.Game.Deck.Deal())

	mg.broadcast(GameEvent{
		Type: "community_cards",
		Data: map[string]interface{}{
			"cards": mg.getCommunityCardImages(),
			"phase": "turn",
		},
	})
}

// dealRiver 发河牌
func (mg *MultiplayerGame) dealRiver() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	mg.CommunityCards = append(mg.CommunityCards, mg.Game.Deck.Deal())

	mg.broadcast(GameEvent{
		Type: "community_cards",
		Data: map[string]interface{}{
			"cards": mg.getCommunityCardImages(),
			"phase": "river",
		},
	})
}

// runShowdown 运行摊牌
func (mg *MultiplayerGame) runShowdown() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	// 收集所有未弃牌玩家的牌
	type playerHand struct {
		userID int
		hand   Hand
	}

	var hands []playerHand
	var activePlayers []*MultiplayerPlayer

	for userID, player := range mg.Players {
		if !player.Folded {
			holeCards := mg.HoleCards[userID]
			allCards := append(holeCards, mg.CommunityCards...)
			hand := EvaluateHand(allCards)
			hands = append(hands, playerHand{userID, hand})
			activePlayers = append(activePlayers, player)
		}
	}

	if len(activePlayers) == 0 {
		return
	}

	if len(activePlayers) == 1 {
		// 只有一个玩家，直接获胜
		winner := activePlayers[0]
		winner.Chips += mg.Pot
		mg.broadcast(GameEvent{
			Type: "showdown_result",
			Data: map[string]interface{}{
				"winners": []int{winner.UserID},
				"pot":     mg.Pot,
				"hands":   mg.getPlayerHands(),
			},
		})
		return
	}

	// 比较牌型找出获胜者
	bestHand := hands[0].hand
	var winners []int

	for _, ph := range hands {
		cmp := CompareHands(ph.hand, bestHand)
		if cmp > 0 {
			bestHand = ph.hand
			winners = []int{ph.userID}
		} else if cmp == 0 {
			winners = append(winners, ph.userID)
		}
	}

	// 分配底池
	winAmount := mg.Pot / len(winners)
	remainder := mg.Pot % len(winners)

	for i, userID := range winners {
		amount := winAmount
		if i < remainder {
			amount++
		}
		mg.Players[userID].Chips += amount
	}

	// 广播结果
	mg.broadcast(GameEvent{
		Type: "showdown_result",
		Data: map[string]interface{}{
			"winners":    winners,
			"pot":        mg.Pot,
			"hands":      mg.getPlayerHands(),
			"best_rank":  getHandRankName(bestHand.Rank),
		},
	})
}

// 辅助方法

func (mg *MultiplayerGame) getNextActivePosition(startPos int, offset int) int {
	numPlayers := len(mg.PlayerOrder)
	count := 0
	for i := 0; i < numPlayers*2; i++ {
		pos := (startPos + i) % numPlayers
		userID := mg.PlayerOrder[pos]
		player := mg.Players[userID]
		if !player.Folded && !player.AllIn && player.Chips > 0 {
			count++
			if count == offset {
				return pos
			}
		}
	}
	return startPos
}

func (mg *MultiplayerGame) moveToNextPlayer() {
	numPlayers := len(mg.PlayerOrder)
	for i := 1; i <= numPlayers; i++ {
		mg.CurrentPos = (mg.CurrentPos + i) % numPlayers
		userID := mg.PlayerOrder[mg.CurrentPos]
		player := mg.Players[userID]
		if !player.Folded && !player.AllIn && player.Chips > 0 {
			return
		}
	}
}

func (mg *MultiplayerGame) getActivePlayerCount() int {
	count := 0
	for _, player := range mg.Players {
		if !player.Folded && !player.AllIn {
			count++
		}
	}
	return count
}

func (mg *MultiplayerGame) isGameOver() bool {
	return mg.getActivePlayerCount() <= 1
}

func (mg *MultiplayerGame) resetBets() {
	mg.CurrentBet = 0
	for _, player := range mg.Players {
		player.Bet = 0
	}
}

func (mg *MultiplayerGame) getCommunityCardImages() []string {
	images := make([]string, len(mg.CommunityCards))
	for i, card := range mg.CommunityCards {
		images[i] = card.ImageFileName()
	}
	return images
}

func (mg *MultiplayerGame) getPlayerHands() []map[string]interface{} {
	result := make([]map[string]interface{}, 0)
	for userID, player := range mg.Players {
		if !player.Folded {
			holeCards := mg.HoleCards[userID]
			allCards := append(holeCards, mg.CommunityCards...)
			hand := EvaluateHand(allCards)
			result = append(result, map[string]interface{}{
				"user_id":    userID,
				"nickname":   player.Nickname,
				"hole_cards": []string{holeCards[0].ImageFileName(), holeCards[1].ImageFileName()},
				"hand_rank":  getHandRankName(hand.Rank),
			})
		}
	}
	return result
}

func (mg *MultiplayerGame) getGameResult() map[string]interface{} {
	players := make([]map[string]interface{}, 0)
	for _, userID := range mg.PlayerOrder {
		player := mg.Players[userID]
		players = append(players, map[string]interface{}{
			"user_id":  userID,
			"nickname": player.Nickname,
			"chips":    player.Chips,
		})
	}

	return map[string]interface{}{
		"players": players,
		"pot":     mg.Pot,
	}
}

// 广播相关

func (mg *MultiplayerGame) broadcast(event GameEvent) {
	select {
	case mg.BroadcastChan <- event:
	default:
	}
}

func (mg *MultiplayerGame) broadcastLoop() {
	for event := range mg.BroadcastChan {
		mg.mu.RLock()
		data, _ := json.Marshal(WebSocketMessage{
			Event: event.Type,
			Data:  event.Data,
		})

		for _, player := range mg.Players {
			if player.Conn != nil && player.IsOnline {
				select {
				case player.Conn.Send <- data:
				default:
				}
			}
		}
		mg.mu.RUnlock()
	}
}

func (mg *MultiplayerGame) sendToPlayer(userID int, event GameEvent) {
	mg.mu.RLock()
	player, exists := mg.Players[userID]
	mg.mu.RUnlock()

	if !exists || player.Conn == nil {
		return
	}

	data, _ := json.Marshal(WebSocketMessage{
		Event: event.Type,
		Data:  event.Data,
	})

	select {
	case player.Conn.Send <- data:
	default:
	}
}

func (mg *MultiplayerGame) sendGameState(userID int) {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	players := make([]map[string]interface{}, 0)
	for _, uid := range mg.PlayerOrder {
		p := mg.Players[uid]
		playerData := map[string]interface{}{
			"user_id":  uid,
			"nickname": p.Nickname,
			"chips":    p.Chips,
			"bet":      p.Bet,
			"folded":   p.Folded,
			"all_in":   p.AllIn,
		}

		// 发送该玩家的手牌
		if uid == userID {
			if cards, ok := mg.HoleCards[uid]; ok {
				playerData["cards"] = []string{cards[0].ImageFileName(), cards[1].ImageFileName()}
			}
			playerData["is_me"] = true
		} else {
			// 其他玩家显示背面
			playerData["cards"] = []string{"bm.png", "bm.png"}
			playerData["is_me"] = false
		}

		players = append(players, playerData)
	}

	mg.sendToPlayer(userID, GameEvent{
		Type: "game_state",
		Data: map[string]interface{}{
			"status":          mg.Status,
			"phase":           mg.Status,
			"pot":             mg.Pot,
			"current_bet":     mg.CurrentBet,
			"community_cards": mg.getCommunityCardImages(),
			"players":         players,
			"current_pos":     mg.CurrentPos,
			"action_log":      mg.ActionLog,
		},
	})
}

// Stop 停止游戏
func (mg *MultiplayerGame) Stop() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	mg.Status = "finished"
	close(mg.ActionChan)
	close(mg.BroadcastChan)
}
