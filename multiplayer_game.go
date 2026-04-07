package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// PlayerAction 玩家操作
type PlayerAction struct {
	UserID int
	Action string
	Amount int
}

// Pot 底池结构
type Pot struct {
	Amount  int   // 底池金额
	UserIDs []int // 参与该底池的玩家ID
	IsMain  bool  // 是否为主底池
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
	SmallBlind     int
	BigBlind       int
	Pots           []Pot // 主底池和边池
}

// MultiplayerPlayer 多人游戏玩家
type MultiplayerPlayer struct {
	UserID    int
	Nickname  string
	SeatIndex int
	Chips     int
	Bet       int
	Folded    bool
	AllIn     bool
	IsOnline  bool
	Conn      *Client
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
	Type string      `json:"type"`
	Data interface{} `json:"data"`
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

	fmt.Printf("CreateGame: 被调用，roomID=%s\n", roomID)

	if _, exists := mgm.games[roomID]; exists {
		fmt.Printf("CreateGame: 游戏已存在，roomID=%s\n", roomID)
		return nil, fmt.Errorf("游戏已存在")
	}

	fmt.Printf("CreateGame: 创建新游戏，roomID=%s, players=%d\n", roomID, len(roomPlayers))

	game := &MultiplayerGame{
		RoomID:        roomID,
		Players:       make(map[int]*MultiplayerPlayer),
		PlayerOrder:   make([]int, 0, len(roomPlayers)),
		Status:        "waiting",
		HoleCards:     make(map[int][]Card),
		ActionLog:     make([]GameAction, 0),
		ActionChan:    make(chan PlayerAction, 100),
		BroadcastChan: make(chan GameEvent, 100),
		SmallBlind:    10,
		BigBlind:      20,
	}

	// 按座位顺序添加玩家
	for _, rp := range roomPlayers {
		fmt.Printf("CreateGame: adding player user_id=%d, nickname=%s\n", rp.UserID, rp.Nickname)
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

	fmt.Printf("CreateGame: game created with %d players\n", len(game.PlayerOrder))

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

	fmt.Printf("Join: user_id=%d, room_id=%s, status=%s\n", userID, mg.RoomID, mg.Status)

	// 如果游戏已经开始，发送当前游戏状态
	if mg.Status != "waiting" {
		fmt.Printf("Join: 发送游戏状态给 user_id=%d\n", userID)
		mg.sendGameStateLocked(userID)
	} else {
		// 游戏还没开始，发送等待消息
		fmt.Printf("Join: 游戏未开始，发送等待消息给 user_id=%d\n", userID)
		mg.sendToPlayerLocked(userID, GameEvent{
			Type: "waiting_for_game",
			Data: map[string]interface{}{
				"message": "等待其他玩家...",
			},
		})
	}

	return nil
}

// SubmitAction 提交操作
func (mg *MultiplayerGame) SubmitAction(userID int, action string, amount int) error {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	// 检查是否是当前玩家
	if len(mg.PlayerOrder) == 0 {
		return fmt.Errorf("没有玩家")
	}
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
		if mg.isGameOver() {
			fmt.Printf("run: 游戏结束，只剩一个玩家，直接进入摊牌\n")
			mg.Status = "showdown"
		}

		switch mg.Status {
		case "preflop":
			mg.runBettingRound("preflop")
			if mg.isGameOver() {
				break
			}
			// 检查是否所有未弃牌玩家都全下
			if mg.allActivePlayersAllIn() {
				fmt.Printf("run: 所有活跃玩家都全下，跳过后续下注轮，直接发完公共牌\n")
				mg.dealRemainingCommunityCards()
				mg.Status = "showdown"
				break
			}
			mg.dealFlop()
			// 翻牌后：从小盲开始（DealerPos+1）
			mg.CurrentPos = mg.getNextActivePosition(mg.DealerPos, 1)
			mg.Status = "flop"
		case "flop":
			mg.runBettingRound("flop")
			if mg.isGameOver() {
				break
			}
			// 检查是否所有未弃牌玩家都全下
			if mg.allActivePlayersAllIn() {
				fmt.Printf("run: 所有活跃玩家都全下，跳过后续下注轮，直接发完公共牌\n")
				mg.dealRemainingCommunityCards()
				mg.Status = "showdown"
				break
			}
			mg.dealTurn()
			// 转牌后：从小盲开始
			mg.CurrentPos = mg.getNextActivePosition(mg.DealerPos, 1)
			mg.Status = "turn"
		case "turn":
			mg.runBettingRound("turn")
			if mg.isGameOver() {
				break
			}
			// 检查是否所有未弃牌玩家都全下
			if mg.allActivePlayersAllIn() {
				fmt.Printf("run: 所有活跃玩家都全下，跳过后续下注轮，直接发完公共牌\n")
				mg.dealRemainingCommunityCards()
				mg.Status = "showdown"
				break
			}
			mg.dealRiver()
			// 河牌后：从小盲开始
			mg.CurrentPos = mg.getNextActivePosition(mg.DealerPos, 1)
			mg.Status = "river"
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

	fmt.Printf("startNewHand: roomID=%s, players=%d\n", mg.RoomID, len(mg.PlayerOrder))

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

	// 确定庄家位置（每局结束后会更新）
	// 收取盲注（同时确定小盲、大盲位置）
	mg.postBlinds()

	// 翻牌前：从大盲左侧（UTG）开始
	// 小盲=DealerPos+1, 大盲=DealerPos+2, UTG=DealerPos+3
	mg.CurrentPos = mg.getNextActivePosition(mg.DealerPos, 3)

	// 广播游戏开始
	mg.broadcast(GameEvent{
		Type: "hand_started",
		Data: map[string]interface{}{
			"dealer_pos": mg.DealerPos,
			"pot":        mg.Pot,
		},
	})

	// 给每个在线玩家发送自己的手牌和完整游戏状态（使用锁安全版本）
	for userID, cards := range mg.HoleCards {
		player := mg.Players[userID]
		if !player.IsOnline {
			continue
		}

		// 发送手牌
		mg.sendToPlayerLocked(userID, GameEvent{
			Type: "hole_cards",
			Data: map[string]interface{}{
				"cards": []string{cards[0].ImageFileName(), cards[1].ImageFileName()},
			},
		})

		// 发送完整游戏状态
		mg.sendGameStateLocked(userID)
	}

	fmt.Printf("startNewHand: 完成，已发送游戏状态给所有在线玩家\n")
}

// postBlinds 收取盲注
func (mg *MultiplayerGame) postBlinds() {
	_ = len(mg.PlayerOrder)
	sbPos := mg.getNextActivePosition(mg.DealerPos, 1)
	bbPos := mg.getNextActivePosition(sbPos, 1)

	sbUserID := mg.PlayerOrder[sbPos]
	bbUserID := mg.PlayerOrder[bbPos]

	// 小盲注
	sbPlayer := mg.Players[sbUserID]
	sbAmount := mg.SmallBlind
	if sbPlayer.Chips < sbAmount {
		sbAmount = sbPlayer.Chips
		sbPlayer.AllIn = true
	}
	sbPlayer.Chips -= sbAmount
	sbPlayer.Bet = sbAmount
	mg.Pot += sbAmount

	// 大盲注
	bbPlayer := mg.Players[bbUserID]
	bbAmount := mg.BigBlind
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
	fmt.Printf("runBettingRound: roomID=%s, phase=%s\n", mg.RoomID, phase)

	// 记录最后加注的玩家ID，-1表示本轮无人加注
	var lastRaiserID int = -1
	// 记录本轮第一个行动的玩家（用于检测绕了一圈）
	var firstActorID int
	// 记录本轮已经行动过的玩家
	actionedPlayers := make(map[int]bool)

	mg.mu.RLock()
	firstActorID = mg.PlayerOrder[mg.CurrentPos]
	mg.mu.RUnlock()

	for {
		mg.mu.RLock()
		if mg.CurrentPos < 0 || mg.CurrentPos >= len(mg.PlayerOrder) {
			mg.mu.RUnlock()
			break
		}
		currentUserID := mg.PlayerOrder[mg.CurrentPos]
		player := mg.Players[currentUserID]
		mg.mu.RUnlock()

		fmt.Printf("runBettingRound: 当前玩家 user_id=%d, nickname=%s\n", currentUserID, player.Nickname)

		// 跳过已弃牌或全下的玩家
		if player.Folded || player.AllIn {
			fmt.Printf("runBettingRound: 跳过玩家 user_id=%d (folded=%v, allin=%v)\n", currentUserID, player.Folded, player.AllIn)
			mg.moveToNextPlayer()
			continue
		}

		// 检查轮次结束条件
		mg.mu.RLock()
		shouldEnd := false

		if lastRaiserID != -1 {
			// 有人加注的情况
			// 条件：所有未弃牌玩家都跟注到了相同金额，且轮到了加注者
			allMatched := true
			for _, uid := range mg.PlayerOrder {
				p := mg.Players[uid]
				if p.Folded {
					continue
				}
				// 未弃牌且未全下的玩家必须跟注到当前下注额
				if !p.AllIn && p.Bet != mg.CurrentBet {
					allMatched = false
					break
				}
			}
			// 加注者已经行动过，且所有人都跟注了
			if allMatched && actionedPlayers[lastRaiserID] && currentUserID == lastRaiserID {
				shouldEnd = true
			}
		} else {
			// 无人加注的情况
			// 条件：所有人都过牌（或弃牌/全下），且绕了一圈回到第一个行动者
			if actionedPlayers[currentUserID] && currentUserID == firstActorID {
				shouldEnd = true
			}
		}
		mg.mu.RUnlock()

		if shouldEnd {
			fmt.Printf("runBettingRound: 轮次结束\n")
			break
		}

		fmt.Printf("runBettingRound: 广播 player_turn 给 user_id=%d\n", currentUserID)

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
		fmt.Printf("runBettingRound: 等待 user_id=%d 的操作...\n", currentUserID)
		action, amount := mg.waitForAction(currentUserID, 60)
		fmt.Printf("runBettingRound: 收到操作 action=%s, amount=%d\n", action, amount)

		// 标记该玩家已行动
		actionedPlayers[currentUserID] = true

		// 执行操作
		mg.executeAction(currentUserID, action, amount)

		// 发送更新后的游戏状态给所有在线玩家
		mg.mu.RLock()
		mg.sendGameStateToAllLocked()
		mg.mu.RUnlock()

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

		// 更新最后加注位置
		if action == "raise" || action == "allin" {
			mg.mu.RLock()
			lastRaiserID = currentUserID
			// 加注后，重置已行动标记（加注者自己已经行动了）
			actionedPlayers = make(map[int]bool)
			actionedPlayers[currentUserID] = true
			mg.mu.RUnlock()
		}

		mg.moveToNextPlayer()
	}

	// 重置下注
	mg.resetBets()
}

// canEndRound 检查是否可以结束当前轮次（条件A）
func (mg *MultiplayerGame) canEndRound(lastRaisePos int) bool {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	if lastRaisePos == -1 {
		return false
	}

	// 检查所有未弃牌玩家是否都跟注到当前下注额
	for _, userID := range mg.PlayerOrder {
		player := mg.Players[userID]
		if player.Folded {
			continue
		}
		if player.Bet != mg.CurrentBet && !player.AllIn {
			return false
		}
	}
	return true
}

// allPlayersCheckedSince 检查从startPos开始所有未弃牌玩家是否都过牌
func (mg *MultiplayerGame) allPlayersCheckedSince(startPos int) bool {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	// 遍历所有未弃牌玩家，检查是否都过牌
	for _, userID := range mg.PlayerOrder {
		player := mg.Players[userID]
		if player.Folded || player.AllIn {
			continue
		}
		// 如果有玩家下注了，就不是全过牌
		if player.Bet > 0 {
			return false
		}
	}
	return true
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
		// 计算最小加注额
		minRaise := mg.BigBlind
		if mg.CurrentBet > 0 {
			minRaise = mg.CurrentBet
		}
		// 如果玩家输入的加注额小于最小加注额，使用最小加注额
		if amount < minRaise {
			amount = minRaise
		}
		totalBet := mg.CurrentBet + amount
		callAmount := totalBet - player.Bet

		if player.Chips <= callAmount {
			callAmount = player.Chips
			player.AllIn = true
		}
		player.Chips -= callAmount
		player.Bet += callAmount
		mg.CurrentBet = player.Bet
		mg.Pot += callAmount

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

	// 先烧一张牌
	mg.Game.Deck.Deal()

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

	// 翻牌后从庄家后一位开始
	mg.CurrentPos = mg.getNextActivePosition(mg.DealerPos, 1)
}

// dealTurn 发转牌
func (mg *MultiplayerGame) dealTurn() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	// 先烧一张牌
	mg.Game.Deck.Deal()

	mg.CommunityCards = append(mg.CommunityCards, mg.Game.Deck.Deal())

	mg.broadcast(GameEvent{
		Type: "community_cards",
		Data: map[string]interface{}{
			"cards": mg.getCommunityCardImages(),
			"phase": "turn",
		},
	})

	// 转牌后从庄家后一位开始
	mg.CurrentPos = mg.getNextActivePosition(mg.DealerPos, 1)
}

// dealRiver 发河牌
func (mg *MultiplayerGame) dealRiver() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	// 先烧一张牌
	mg.Game.Deck.Deal()

	mg.CommunityCards = append(mg.CommunityCards, mg.Game.Deck.Deal())

	mg.broadcast(GameEvent{
		Type: "community_cards",
		Data: map[string]interface{}{
			"cards": mg.getCommunityCardImages(),
			"phase": "river",
		},
	})

	// 河牌后从庄家后一位开始
	mg.CurrentPos = mg.getNextActivePosition(mg.DealerPos, 1)
}

// runShowdown 运行摊牌
func (mg *MultiplayerGame) runShowdown() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	// 收集所有未弃牌玩家的牌
	type playerHand struct {
		userID    int
		hand      Hand
		bestCards []Card
	}

	var hands []playerHand
	var activePlayers []*MultiplayerPlayer

	for userID, player := range mg.Players {
		if !player.Folded {
			holeCards := mg.HoleCards[userID]
			allCards := append(holeCards, mg.CommunityCards...)
			hand := EvaluateHand(allCards)
			bestCards := hand.Cards
			hands = append(hands, playerHand{userID, hand, bestCards})
			activePlayers = append(activePlayers, player)
		}
	}

	// 排序函数：按面值从大到小排序
	sortCardsDescByRank := func(cards []Card) []Card {
		sorted := make([]Card, len(cards))
		copy(sorted, cards)
		// 定义面值排序：A > K > Q > J > 10 > 9 > 8 > 7 > 6 > 5 > 4 > 3 > 2
		rankOrder := map[Rank]int{
			Ace:   14,
			King:  13,
			Queen: 12,
			Jack:  11,
			Ten:   10,
			Nine:  9,
			Eight: 8,
			Seven: 7,
			Six:   6,
			Five:  5,
			Four:  4,
			Three: 3,
			Two:   2,
		}
		sort.Slice(sorted, func(i, j int) bool {
			return rankOrder[sorted[i].Rank] > rankOrder[sorted[j].Rank]
		})
		return sorted
	}

	// 对所有玩家的最佳手牌进行排序
	for i := range hands {
		hands[i].bestCards = sortCardsDescByRank(hands[i].bestCards)
	}

	if len(activePlayers) == 0 {
		return
	}

	if len(activePlayers) == 1 {
		// 只有一个玩家，直接获胜
		winner := activePlayers[0]
		winner.Chips += mg.Pot

		// 获取胜者最佳5张牌
		winnerUserID := winner.UserID
		holeCards := mg.HoleCards[winnerUserID]
		allCards := append(holeCards, mg.CommunityCards...)
		winnerHand := EvaluateHand(allCards)
		winnerBestCards := winnerHand.Cards

		// 对最佳5张牌进行排序
		sortedWinnerCards := make([]Card, len(winnerBestCards))
		copy(sortedWinnerCards, winnerBestCards)
		// 定义面值排序：A > K > Q > J > 10 > 9 > 8 > 7 > 6 > 5 > 4 > 3 > 2
		rankOrder := map[Rank]int{
			Ace:   14,
			King:  13,
			Queen: 12,
			Jack:  11,
			Ten:   10,
			Nine:  9,
			Eight: 8,
			Seven: 7,
			Six:   6,
			Five:  5,
			Four:  4,
			Three: 3,
			Two:   2,
		}
		sort.Slice(sortedWinnerCards, func(i, j int) bool {
			return rankOrder[sortedWinnerCards[i].Rank] > rankOrder[sortedWinnerCards[j].Rank]
		})

		showdownCards := make([]string, len(sortedWinnerCards))
		for i, card := range sortedWinnerCards {
			showdownCards[i] = card.ImageFileName()
		}

		mg.broadcast(GameEvent{
			Type: "showdown_result",
			Data: map[string]interface{}{
				"winners":          []int{winner.UserID},
				"pot":              mg.Pot,
				"hands":            mg.getPlayerHands(),
				"showdown_cards":   showdownCards,
				"winner_name":      winner.Nickname,
				"winner_hand_rank": getHandRankName(winnerHand.Rank),
			},
		})
		return
	}

	// 比较牌型找出获胜者
	bestHand := hands[0].hand
	var winners []int
	var winnerBestCards []Card

	for _, ph := range hands {
		cmp := CompareHands(ph.hand, bestHand)
		if cmp > 0 {
			bestHand = ph.hand
			winners = []int{ph.userID}
			winnerBestCards = ph.bestCards
		} else if cmp == 0 {
			winners = append(winners, ph.userID)
		}
	}

	// 分配底池
	winAmount := mg.Pot / len(winners)
	remainder := mg.Pot % len(winners)
	eachWinAmount := make([]int, len(winners))

	for i, userID := range winners {
		amount := winAmount
		if i < remainder {
			amount++
		}
		mg.Players[userID].Chips += amount
		eachWinAmount[i] = amount
	}

	// 获取第一个胜者的昵称
	var winnerName string
	if len(winners) > 0 {
		winnerName = mg.Players[winners[0]].Nickname
	}

	// 获取赢得的筹码数量（取第一个胜者的数量）
	totalWinAmount := 0
	if len(eachWinAmount) > 0 {
		totalWinAmount = eachWinAmount[0]
	}

	// 准备展示胜者最佳5张牌
	showdownCards := make([]string, len(winnerBestCards))
	for i, card := range winnerBestCards {
		showdownCards[i] = card.ImageFileName()
	}

	// 广播结果
	mg.broadcast(GameEvent{
		Type: "showdown_result",
		Data: map[string]interface{}{
			"winners":          winners,
			"pot":              mg.Pot,
			"hands":            mg.getPlayerHands(),
			"best_rank":        getHandRankName(bestHand.Rank),
			"showdown_cards":   showdownCards,
			"winner_name":      winnerName,
			"winner_hand_rank": getHandRankName(bestHand.Rank),
			"win_amount":       totalWinAmount,
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
		if !player.Folded && !player.AllIn && player.Chips >= 0 {
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
		if !player.Folded && !player.AllIn && player.Chips >= 0 {
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

// allActivePlayersAllIn 检查所有未弃牌玩家是否都全下
func (mg *MultiplayerGame) allActivePlayersAllIn() bool {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	activePlayerCount := 0
	allInCount := 0

	for _, player := range mg.Players {
		if player.Folded {
			continue
		}
		activePlayerCount++
		if player.AllIn {
			allInCount++
		}
	}

	// 至少有一个活跃玩家，且所有活跃玩家都全下
	return activePlayerCount >= 1 && allInCount == activePlayerCount
}

// dealRemainingCommunityCards 发完剩余的所有公共牌
func (mg *MultiplayerGame) dealRemainingCommunityCards() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	fmt.Printf("dealRemainingCommunityCards: 开始发剩余公共牌，当前公共牌=%d\n", len(mg.CommunityCards))

	for len(mg.CommunityCards) < 5 {
		if mg.Game == nil || mg.Game.Deck == nil {
			fmt.Printf("dealRemainingCommunityCards: 牌组为空，无法发牌\n")
			break
		}
		card := mg.Game.Deck.Deal()
		if (card == Card{}) {
			fmt.Printf("dealRemainingCommunityCards: 没有更多牌了\n")
			break
		}
		mg.CommunityCards = append(mg.CommunityCards, card)
		fmt.Printf("dealRemainingCommunityCards: 发了一张牌 %s\n", card.ImageFileName())
	}

	fmt.Printf("dealRemainingCommunityCards: 发完剩余公共牌，最终公共牌=%d\n", len(mg.CommunityCards))

	// 发送更新后的游戏状态给所有在线玩家
	mg.sendGameStateToAllLocked()

	// 广播公共牌
	mg.broadcast(GameEvent{
		Type: "community_cards",
		Data: map[string]interface{}{
			"cards": mg.getCommunityCardImages(),
		},
	})
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
			result = append(result, map[string]interface{}{
				"user_id":    userID,
				"nickname":   player.Nickname,
				"hole_cards": []string{holeCards[0].ImageFileName(), holeCards[1].ImageFileName()},
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
	defer mg.mu.RUnlock()
	mg.sendToPlayerLocked(userID, event)
}

func (mg *MultiplayerGame) sendGameState(userID int) {
	mg.mu.RLock()
	defer mg.mu.RUnlock()
	mg.sendGameStateLocked(userID)
}

// sendGameStateLocked 发送游戏状态（已持有锁）
func (mg *MultiplayerGame) sendGameStateLocked(userID int) {
	fmt.Printf("sendGameStateLocked: roomID=%s, userID=%d, totalPlayers=%d\n", mg.RoomID, userID, len(mg.PlayerOrder))

	numPlayers := len(mg.PlayerOrder)
	dealerUserID := mg.PlayerOrder[mg.DealerPos]
	sbUserID := mg.PlayerOrder[(mg.DealerPos+1)%numPlayers]
	bbUserID := mg.PlayerOrder[(mg.DealerPos+2)%numPlayers]

	players := make([]map[string]interface{}, 0)
	for _, uid := range mg.PlayerOrder {
		p := mg.Players[uid]
		playerData := map[string]interface{}{
			"user_id":        uid,
			"nickname":       p.Nickname,
			"chips":          p.Chips,
			"bet":            p.Bet,
			"folded":         p.Folded,
			"all_in":         p.AllIn,
			"is_dealer":      uid == dealerUserID,
			"is_small_blind": uid == sbUserID,
			"is_big_blind":   uid == bbUserID,
		}

		if uid == userID {
			if cards, ok := mg.HoleCards[uid]; ok {
				playerData["cards"] = []string{cards[0].ImageFileName(), cards[1].ImageFileName()}
			}
			playerData["is_me"] = true
		} else {
			playerData["cards"] = []string{"bm.png", "bm.png"}
			playerData["is_me"] = false
		}

		players = append(players, playerData)
	}

	var currentPlayerID int
	if mg.CurrentPos >= 0 && mg.CurrentPos < len(mg.PlayerOrder) {
		currentPlayerID = mg.PlayerOrder[mg.CurrentPos]
	}

	mg.sendToPlayerLocked(userID, GameEvent{
		Type: "game_state",
		Data: map[string]interface{}{
			"status":          mg.Status,
			"phase":           mg.Status,
			"pot":             mg.Pot,
			"current_bet":     mg.CurrentBet,
			"community_cards": mg.getCommunityCardImages(),
			"players":         players,
			"current_player":  currentPlayerID,
			"action_log":      mg.ActionLog,
		},
	})
}

// sendGameStateToAllLocked 发送游戏状态给所有在线玩家（已持有锁）
func (mg *MultiplayerGame) sendGameStateToAllLocked() {
	for userID := range mg.Players {
		player := mg.Players[userID]
		if player.IsOnline {
			mg.sendGameStateLocked(userID)
		}
	}
}

// sendToPlayerLocked 向指定玩家发送消息（已持有锁）
func (mg *MultiplayerGame) sendToPlayerLocked(userID int, event GameEvent) {
	player, exists := mg.Players[userID]
	if !exists || player.Conn == nil || !player.IsOnline {
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

// Stop 停止游戏
func (mg *MultiplayerGame) Stop() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	mg.Status = "finished"
	close(mg.ActionChan)
	close(mg.BroadcastChan)
}
