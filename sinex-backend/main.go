import (
	"fmt"
	"log"
	"net/http"
	"os"      // <-- Adicione isso!
	"time"

	"github.com/gorilla/websocket"
)

// ==========================================
// 1. CONSTANTES E TIMEOUTS
// ==========================================
const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512000
)

// ==========================================
// 2. ESTRUTURAS DE DADOS
// ==========================================
type Message struct {
	Type      string `json:"type"` // "chat", "digitando", "status_check", "status_reply", "status_update"
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

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var chatHub = &Hub{
	Clients:    make(map[string]*Client),
	Register:   make(chan *Client),
	Unregister: make(chan *Client),
	Broadcast:  make(chan Message),
}

// ==========================================
// 3. SISTEMA DE PRESENÇA
// ==========================================
// Avisa todo mundo quem entrou ou saiu
func (h *Hub) broadcastPresence(username string, status string) {
	msg := Message{
		Type:    "status_update",
		From:    username,
		Content: status,
	}
	for _, client := range h.Clients {
		if client.Username != username {
			select {
			case client.Send <- msg:
			default:
			}
		}
	}
}

// ==========================================
// 4. O CORAÇÃO DO SERVIDOR
// ==========================================
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			if oldClient, ok := h.Clients[client.Username]; ok {
				close(oldClient.Send)
				delete(h.Clients, client.Username)
			}
			h.Clients[client.Username] = client
			fmt.Println("🟢 Online:", client.Username, "| Total:", len(h.Clients))
			h.broadcastPresence(client.Username, "online") // Avisa que entrou

		case client := <-h.Unregister:
			if currentClient, ok := h.Clients[client.Username]; ok && currentClient == client {
				delete(h.Clients, client.Username)
				close(client.Send)
				fmt.Println("🔴 Offline:", client.Username, "| Total:", len(h.Clients))
				h.broadcastPresence(client.Username, "offline") // Avisa que saiu
			}

		case message := <-h.Broadcast:
			// SISTEMA NOVO: Alguém perguntou se um contato está online
			if message.Type == "status_check" {
				status := "offline"
				if _, ok := h.Clients[message.To]; ok {
					status = "online"
				}
				if sender, ok := h.Clients[message.From]; ok {
					sender.Send <- Message{
						Type:    "status_reply",
						From:    message.To,
						To:      message.From,
						Content: status,
					}
				}
				continue // Não repassa essa mensagem, o Go já respondeu!
			}

			// Roteamento normal de Chat e Digitando
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

// ==========================================
// 5. MOTOR DE ESCRITA E LEITURA
// ==========================================
func (c *Client) readPump() {
	defer func() {
		chatHub.Unregister <- c
		c.Conn.Close()
	}()
	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		var msg Message
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			break
		}
		chatHub.Broadcast <- msg
	}
}

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

// ==========================================
// 6. ROTA E MAIN
// ==========================================
func serveWs(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("user")
	if username == "" {
		http.Error(w, "Usuário não informado", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Erro no Upgrade WebSocket:", err)
		return
	}

	client := &Client{
		Username: username,
		Conn:     conn,
		Send:     make(chan Message, 256),
	}

	chatHub.Register <- client

	go client.writePump()
	go client.readPump()
}
func main() {
	fmt.Println("================================================")
	fmt.Println("🚀 MOTOR WEBSOCKET DO SINEX CHAT ATIVADO!")
	fmt.Println("================================================")

	go chatHub.Run()
	http.HandleFunc("/ws", serveWs)
	
	// 👇 Muda aqui! Pega a porta que o Render vai fornecer
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Porta padrão para rodar no seu PC
	}
	
	log.Fatal(http.ListenAndServe(":"+port, nil))
}