package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 65536 // 64KB (Recomendação OWASP contra ataques DoS)
)

// Estrutura padrão de troca de mensagens do Sinex Chat
type Message struct {
	Type      string `json:"type"`
	From      string `json:"from"`
	To        string `json:"to"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

type Client struct {
	Username string
	Conn     *websocket.Conn
	Send     chan Message
}

type Hub struct {
	Clients    map[string]*Client
	Register   chan *Client
	Unregister chan *Client
	Broadcast  chan Message
}

// Configuração do WebSocket (Upgrader) - BLINDADO
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Proteção contra Cross-Site WebSocket Hijacking (CSWSH)
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		
		// ALLOWLIST DE ORIGENS CONFIÁVEIS
		allowedOrigins := []string{
			"https://chat-parameuamor.firebaseapp.com",
			"https://chat-parameuamor.web.app",
			"http://localhost:8080",
			"http://localhost:3000",
			"http://localhost:5500",
			"http://127.0.0.1:5500",
			"http://127.0.0.1:3000",
		}

		for _, allowed := range allowedOrigins {
			if origin == allowed {
				return true
			}
		}
		log.Printf("[BLOQUEIO DE SEGURANÇA] Origem não autorizada bloqueada: %s", origin)
		return false
	},
}

// Instância global do Hub de conexões
var chatHub = &Hub{
	Clients:    make(map[string]*Client),
	Register:   make(chan *Client),
	Unregister: make(chan *Client),
	Broadcast:  make(chan Message),
}

// Emite eventos online/offline para os usuários conectados
func (h *Hub) broadcastPresence(username string, status string) {
	msg := Message{Type: "status_update", From: username, Content: status}
	for _, client := range h.Clients {
		if client.Username != username {
			select {
			case client.Send <- msg:
			default:
				// Fila cheia, ignora
			}
		}
	}
}

// Gerenciador principal do Hub
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			if oldClient, ok := h.Clients[client.Username]; ok {
				close(oldClient.Send)
				delete(h.Clients, client.Username)
			}
			h.Clients[client.Username] = client
			log.Printf("[+] Usuário conectado: %s", client.Username)
			h.broadcastPresence(client.Username, "online")

		case client := <-h.Unregister:
			if currentClient, ok := h.Clients[client.Username]; ok && currentClient == client {
				delete(h.Clients, client.Username)
				close(client.Send)
				log.Printf("[-] Usuário desconectado: %s", client.Username)
				h.broadcastPresence(client.Username, "offline")
			}

		case message := <-h.Broadcast:
			// Lógica de Status em Tempo Real
			if message.Type == "status_check" {
				status := "offline"
				if _, ok := h.Clients[message.To]; ok {
					status = "online"
				}
				if sender, ok := h.Clients[message.From]; ok {
					sender.Send <- Message{Type: "status_reply", From: message.To, To: message.From, Content: status}
				}
				continue
			}

			// Roteamento de mensagens diretas e chamadas
			if targetClient, ok := h.Clients[message.To]; ok {
				select {
				case targetClient.Send <- message:
				default:
					close(targetClient.Send)
					delete(h.Clients, targetClient.Username)
					h.broadcastPresence(targetClient.Username, "offline")
				}
			}
		}
	}
}

// Ciclo de leitura (Recebe dados do Frontend)
func (c *Client) readPump() {
	defer func() {
		chatHub.Unregister <- c
		c.Conn.Close()
	}()
	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error { c.Conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	for {
		var msg Message
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[!] Erro de leitura WS (%s): %v", c.Username, err)
			}
			break
		}
		chatHub.Broadcast <- msg
	}
}

// Ciclo de escrita (Envia dados para o Frontend)
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.Conn.WriteJSON(msg)
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Endpoint de conexão do WebSocket
func serveWs(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("user")
	token := r.URL.Query().Get("token")

	// Prevenção inicial de acesso sem token (transição suave para a Etapa 4)
	if username == "" || token == "" {
		http.Error(w, "Acesso Negado. Credenciais ausentes.", http.StatusUnauthorized)
		log.Printf("[BLOQUEIO] Tentativa de acesso não autorizada: user=%s", username)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ERRO] Falha no Upgrade do WebSocket: %v", err)
		return
	}

	client := &Client{Username: username, Conn: conn, Send: make(chan Message, 256)}
	chatHub.Register <- client

	go client.writePump()
	go client.readPump()
}

func main() {
	go chatHub.Run()

	http.HandleFunc("/ws", serveWs)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Sinex Chat Backend Seguro Ativo!"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Servidor WebSocket do Sinex Chat iniciado na porta %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal("Erro fatal no servidor: ", err)
	}
}
