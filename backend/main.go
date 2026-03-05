package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type LetterResult struct {
	Letter string `json:"letter"`
	Status string `json:"status"` // "correct", "present", "absent"
}

type Guess struct {
	Player  string         `json:"player"`
	Word    string         `json:"word,omitempty"`
	Results []LetterResult `json:"results"`
	Time    int64          `json:"time"`
}

type PlayerState struct {
	Nickname string  `json:"nickname"`
	Guesses  []Guess `json:"guesses"`
	Solved   bool    `json:"solved"`
}

type Session struct {
	ID      string                 `json:"id"`
	Word    string                 `json:"-"`
	Players map[string]*PlayerState `json:"players"`
	Guesses []Guess                `json:"guesses"` // all guesses in order
	Created int64                  `json:"created"`
	mu      sync.RWMutex
	clients map[*websocket.Conn]string // conn -> nickname
}

var (
	sessions   = make(map[string]*Session)
	sessionsMu sync.RWMutex
	upgrader   = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	validWords map[string]bool
)

func init() {
	validWords = make(map[string]bool, len(wordList))
	for _, w := range wordList {
		validWords[strings.ToLower(w)] = true
	}
}

func generateID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func pickWord() string {
	return strings.ToLower(wordList[rand.Intn(len(wordList))])
}

func checkGuess(guess, answer string) []LetterResult {
	guess = strings.ToLower(guess)
	answer = strings.ToLower(answer)
	results := make([]LetterResult, 5)
	answerRunes := []rune(answer)
	guessRunes := []rune(guess)
	used := make([]bool, 5)

	// First pass: correct positions
	for i := 0; i < 5; i++ {
		if guessRunes[i] == answerRunes[i] {
			results[i] = LetterResult{Letter: string(guessRunes[i]), Status: "correct"}
			used[i] = true
		}
	}

	// Second pass: present but wrong position
	for i := 0; i < 5; i++ {
		if results[i].Status == "correct" {
			continue
		}
		found := false
		for j := 0; j < 5; j++ {
			if !used[j] && guessRunes[i] == answerRunes[j] {
				found = true
				used[j] = true
				break
			}
		}
		if found {
			results[i] = LetterResult{Letter: string(guessRunes[i]), Status: "present"}
		} else {
			results[i] = LetterResult{Letter: string(guessRunes[i]), Status: "absent"}
		}
	}
	return results
}

type WSMessage struct {
	Type     string `json:"type"`
	Nickname string `json:"nickname,omitempty"`
	Word     string `json:"word,omitempty"`
	Session  string `json:"session,omitempty"`
}

type WSResponse struct {
	Type    string      `json:"type"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func broadcastToSession(sess *Session, msg WSResponse, exclude *websocket.Conn) {
	data, _ := json.Marshal(msg)
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	for conn := range sess.clients {
		if conn != exclude {
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}
	defer conn.Close()

	var currentSession *Session
	var nickname string

	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "create":
			nickname = msg.Nickname
			if nickname == "" {
				nickname = "anon"
			}
			id := generateID()
			sess := &Session{
				ID:      id,
				Word:    pickWord(),
				Players: make(map[string]*PlayerState),
				Guesses: []Guess{},
				Created: time.Now().Unix(),
				clients: make(map[*websocket.Conn]string),
			}
			sess.Players[nickname] = &PlayerState{Nickname: nickname, Guesses: []Guess{}}
			sess.clients[conn] = nickname

			sessionsMu.Lock()
			sessions[id] = sess
			sessionsMu.Unlock()

			currentSession = sess
			resp, _ := json.Marshal(WSResponse{Type: "created", Data: map[string]interface{}{
				"sessionId": id, "nickname": nickname, "wordLength": 5,
			}})
			conn.WriteMessage(websocket.TextMessage, resp)
			log.Printf("Session %s created by %s (word: %s)", id, nickname, sess.Word)

		case "join":
			nickname = msg.Nickname
			if nickname == "" {
				nickname = "anon"
			}
			sessionsMu.RLock()
			sess, ok := sessions[msg.Session]
			sessionsMu.RUnlock()
			if !ok {
				resp, _ := json.Marshal(WSResponse{Type: "error", Error: "Session not found"})
				conn.WriteMessage(websocket.TextMessage, resp)
				continue
			}

			sess.mu.Lock()
			if _, exists := sess.Players[nickname]; !exists {
				sess.Players[nickname] = &PlayerState{Nickname: nickname, Guesses: []Guess{}}
			}
			sess.clients[conn] = nickname
			sess.mu.Unlock()
			currentSession = sess

			// Send current state
			sess.mu.RLock()
			resp, _ := json.Marshal(WSResponse{Type: "joined", Data: map[string]interface{}{
				"sessionId": sess.ID, "nickname": nickname, "guesses": sess.Guesses,
				"players": sess.Players, "wordLength": 5,
			}})
			sess.mu.RUnlock()
			conn.WriteMessage(websocket.TextMessage, resp)

			// Notify others
			broadcastToSession(sess, WSResponse{Type: "player_joined", Data: map[string]string{"nickname": nickname}}, conn)

		case "guess":
			if currentSession == nil {
				continue
			}
			word := strings.ToLower(strings.TrimSpace(msg.Word))
			if len(word) != 5 {
				resp, _ := json.Marshal(WSResponse{Type: "error", Error: "Word must be 5 letters"})
				conn.WriteMessage(websocket.TextMessage, resp)
				continue
			}
			if !validWords[word] {
				resp, _ := json.Marshal(WSResponse{Type: "error", Error: "Not a valid word"})
				conn.WriteMessage(websocket.TextMessage, resp)
				continue
			}

			currentSession.mu.Lock()
			player := currentSession.Players[nickname]
			if player == nil || player.Solved || len(player.Guesses) >= 6 {
				currentSession.mu.Unlock()
				continue
			}

			results := checkGuess(word, currentSession.Word)
			guess := Guess{Player: nickname, Word: word, Results: results, Time: time.Now().Unix()}
			player.Guesses = append(player.Guesses, guess)
			currentSession.Guesses = append(currentSession.Guesses, guess)

			solved := word == currentSession.Word
			if solved {
				player.Solved = true
			}
			currentSession.mu.Unlock()

			// Send result to guesser
			resp, _ := json.Marshal(WSResponse{Type: "guess_result", Data: map[string]interface{}{
				"guess": guess, "solved": solved, "answer": func() string {
					if solved || len(player.Guesses) >= 6 { return currentSession.Word }
					return ""
				}(),
			}})
			conn.WriteMessage(websocket.TextMessage, resp)

			// Broadcast to others
			broadcastToSession(currentSession, WSResponse{Type: "player_guess", Data: guess}, conn)

		case "new_word":
			if currentSession == nil {
				continue
			}
			currentSession.mu.Lock()
			currentSession.Word = pickWord()
			currentSession.Guesses = []Guess{}
			for _, p := range currentSession.Players {
				p.Guesses = []Guess{}
				p.Solved = false
			}
			currentSession.mu.Unlock()

			broadcastToSession(currentSession, WSResponse{Type: "new_round", Data: map[string]interface{}{
				"message": "New word selected!", "wordLength": 5,
			}}, nil)
			log.Printf("Session %s new word: %s", currentSession.ID, currentSession.Word)
		}
	}

	// Cleanup on disconnect
	if currentSession != nil {
		currentSession.mu.Lock()
		delete(currentSession.clients, conn)
		currentSession.mu.Unlock()
		broadcastToSession(currentSession, WSResponse{Type: "player_left", Data: map[string]string{"nickname": nickname}}, nil)
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())

	http.HandleFunc("/ws", handleWS)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	port := ":3848"
	log.Printf("Wordle backend starting on %s", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
