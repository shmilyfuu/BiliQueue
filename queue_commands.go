package main

import (
	"strings"
	"time"
)

func (a *App) addManualUser(username string) (bool, string) {
	username = strings.TrimSpace(username)
	if username == "" {
		return false, "用户名不能为空"
	}
	manualSequence := a.messageSeq.Add(1) % 1000
	uid := -(time.Now().UnixMilli()*1000 + int64(manualSequence))
	a.mu.RLock()
	joinCommand := a.config.JoinCommand
	a.mu.RUnlock()
	return a.addUser(ChatMessage{UID: uid, Username: username, Text: joinCommand, Manual: true})
}

func (a *App) removeQueueUser(id string) bool {
	a.mu.Lock()
	removed := false
	for i, user := range a.queue {
		if user.ID == id {
			a.queue = append(a.queue[:i], a.queue[i+1:]...)
			removed = true
			break
		}
	}
	a.mu.Unlock()
	if removed {
		a.saveQueue()
		a.broadcast()
	}
	return removed
}

func (a *App) reorderQueue(ids []string) {
	a.mu.Lock()
	byID := make(map[string]QueueUser, len(a.queue))
	for _, user := range a.queue {
		byID[user.ID] = user
	}
	ordered := make([]QueueUser, 0, len(a.queue))
	for _, id := range ids {
		if user, ok := byID[id]; ok {
			ordered = append(ordered, user)
			delete(byID, id)
		}
	}
	for _, user := range a.queue {
		if _, ok := byID[user.ID]; ok {
			ordered = append(ordered, user)
		}
	}
	a.queue = ordered
	a.mu.Unlock()
	a.saveQueue()
	a.broadcast()
}

func (a *App) setQueuePaused(paused bool) {
	a.mu.Lock()
	changed := a.paused != paused
	a.paused = paused
	a.mu.Unlock()
	if changed {
		a.broadcast()
	}
}
