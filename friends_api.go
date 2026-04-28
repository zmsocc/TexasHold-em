package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// HandleSearchFriends 处理搜索好友
func HandleSearchFriends(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	var req struct {
		Nickname string `json:"nickname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求参数",
		})
		return
	}

	users, err := SearchUsers(req.Nickname, 20)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "搜索失败",
		})
		return
	}

	// 过滤掉自己
	var results []*User
	for _, u := range users {
		if u.ID != user.ID {
			// 清除密码
			u.Password = ""
			results = append(results, u)
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    results,
	})
}

// HandleSendFriendRequest 处理发送好友请求
func HandleSendFriendRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	var req struct {
		ToUserID int `json:"to_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求参数",
		})
		return
	}

	// 检查是否已经是好友
	isFriend, err := IsFriend(user.ID, req.ToUserID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "检查好友关系失败",
		})
		return
	}
	if isFriend {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "已经是好友了",
		})
		return
	}

	// 检查是否已经发送过请求
	existingReq, err := GetFriendRequest(user.ID, req.ToUserID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "检查请求失败",
		})
		return
	}
	if existingReq != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "已经发送过好友请求",
		})
		return
	}

	// 检查对方是否已经给自己发送过请求
	reverseReq, err := GetFriendRequest(req.ToUserID, user.ID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "检查请求失败",
		})
		return
	}
	if reverseReq != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "对方已发送好友请求，请查看好友请求列表",
		})
		return
	}

	// 创建好友请求
	err = CreateFriendRequest(user.ID, req.ToUserID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "发送好友请求失败",
		})
		return
	}

	// 通过WebSocket通知对方
	if wsManager != nil {
		wsManager.SendToUser(req.ToUserID, "friend_request", map[string]interface{}{
			"from_user": map[string]interface{}{
				"id":         user.ID,
				"nickname":   user.Nickname,
				"avatar_url": user.AvatarURL,
			},
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "好友请求已发送",
	})
}

// HandleGetFriendRequests 处理获取好友请求列表
func HandleGetFriendRequests(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	requests, err := GetIncomingFriendRequests(user.ID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "获取好友请求失败",
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    requests,
	})
}

// HandleAcceptFriendRequest 处理接受好友请求
func HandleAcceptFriendRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	var req struct {
		RequestID int `json:"request_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求参数",
		})
		return
	}

	err := AcceptFriendRequest(req.RequestID, user.ID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "接受好友请求失败",
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "已添加为好友",
	})
}

// HandleRejectFriendRequest 处理拒绝好友请求
func HandleRejectFriendRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	var req struct {
		RequestID int `json:"request_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求参数",
		})
		return
	}

	err := RejectFriendRequest(req.RequestID, user.ID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "拒绝好友请求失败",
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "已拒绝好友请求",
	})
}

// HandleGetFriends 处理获取好友列表
func HandleGetFriends(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	friends, err := GetFriends(user.ID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "获取好友列表失败",
		})
		return
	}

	// 清理密码信息
	for _, friend := range friends {
		friend.User.Password = ""
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    friends,
	})
}

// HandleGetUserProfile 处理获取用户详情
func HandleGetUserProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	// 获取用户ID
	userIDStr := r.URL.Query().Get("user_id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的用户ID",
		})
		return
	}

	user, err := GetUserByID(userID)
	if err != nil || user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "用户不存在",
		})
		return
	}

	// 增加访问次数
	IncrementViewCount(userID)

	// 获取游戏历史
	gameHistory, err := GetGameHistory(userID, 20)
	if err != nil {
		gameHistory = []*GameRecord{}
	}

	// 清理密码
	user.Password = ""

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"user":         user,
			"game_history": gameHistory,
		},
	})
}

// HandleGetConversation 处理获取对话历史
func HandleGetConversation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	targetUserIDStr := r.URL.Query().Get("target_user_id")
	targetUserID, err := strconv.Atoi(targetUserIDStr)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的用户ID",
		})
		return
	}

	messages, err := GetConversation(user.ID, targetUserID, 50)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "获取消息失败",
		})
		return
	}

	// 标记为已读
	MarkMessagesAsRead(targetUserID, user.ID)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    messages,
	})
}

// HandleSendMessage 处理发送消息
func HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	var req struct {
		ToUserID int    `json:"to_user_id"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "无效的请求参数",
		})
		return
	}

	if req.Content == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "消息内容不能为空",
		})
		return
	}

	// 检查是否是好友
	isFriend, err := IsFriend(user.ID, req.ToUserID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "检查好友关系失败",
		})
		return
	}
	if !isFriend {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "只能给好友发送消息",
		})
		return
	}

	// 保存消息
	msg, err := SaveMessage(user.ID, req.ToUserID, req.Content)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "保存消息失败",
		})
		return
	}

	// 通过WebSocket发送消息给对方
	if wsManager != nil {
		wsManager.SendToUser(req.ToUserID, "private_message", map[string]interface{}{
			"from_user_id":  user.ID,
			"from_nickname": user.Nickname,
			"content":       req.Content,
			"created_at":    msg.CreatedAt,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    msg,
	})
}

// HandleGetRoomPlayersWithFriends 处理获取房间玩家及好友状态
func HandleGetRoomPlayersWithFriends(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})
		return
	}

	user := getCurrentUser(r)
	if user == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "未登录",
		})
		return
	}

	roomID := r.URL.Query().Get("room_id")
	if roomID == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "房间ID不能为空",
		})
		return
	}

	room, err := GetRoomByID(roomID)
	if err != nil || room == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "房间不存在",
		})
		return
	}

	players, err := GetRoomPlayers(roomID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "获取玩家列表失败",
		})
		return
	}

	// 获取当前用户的好友列表
	friends, err := GetFriends(user.ID)
	if err != nil {
		friends = []*FriendWithStatus{}
	}

	// 创建好友ID映射
	friendMap := make(map[int]*FriendWithStatus)
	for _, friend := range friends {
		friendMap[friend.User.ID] = friend
	}

	// 构建响应
	type RoomPlayerWithFriendStatus struct {
		RoomPlayer
		IsFriend bool `json:"is_friend"`
		IsOnline bool `json:"is_online"`
	}

	var result []RoomPlayerWithFriendStatus
	for _, player := range players {
		isFriend := false
		isOnline := false
		if friend, exists := friendMap[player.UserID]; exists {
			isFriend = true
			isOnline = friend.IsOnline
		}

		// 获取用户在线状态
		if playerUser, _ := GetUserByID(player.UserID); playerUser != nil {
			isOnline = playerUser.IsOnline
		}

		result = append(result, RoomPlayerWithFriendStatus{
			RoomPlayer: *player,
			IsFriend:   isFriend,
			IsOnline:   isOnline,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    result,
	})
}
