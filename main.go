package main

import (
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/streadway/amqp"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	upgrader  = websocket.Upgrader{}
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.Mutex
	log       = logrus.New()
)

func init() {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.WithFields(logrus.Fields{
			"event":  "config_load",
			"status": "failed",
			"error":  err.Error(),
		}).Fatal("Failed to read config file")
	}

	log.SetFormatter(&logrus.JSONFormatter{})
	logFile := &lumberjack.Logger{
		Filename:   viper.GetString("log.file_path"),
		MaxSize:    viper.GetInt("log.max_size"),
		MaxBackups: viper.GetInt("log.max_backups"),
		MaxAge:     viper.GetInt("log.max_age"),
		Compress:   viper.GetBool("log.compress"),
	}
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)
}

func main() {
	log.WithFields(logrus.Fields{
		"event":  "service_start",
		"status": "initializing",
	}).Info("Service started")

	rabbitMQURL := viper.GetString("rabbitmq.url")
	queueName := viper.GetString("rabbitmq.queue")

	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		log.WithFields(logrus.Fields{
			"event":  "rabbitmq_connection",
			"status": "failed",
			"error":  err.Error(),
		}).Fatal("Failed to connect to RabbitMQ")
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.WithFields(logrus.Fields{
			"event":  "channel_creation",
			"status": "failed",
			"error":  err.Error(),
		}).Fatal("Failed to create RabbitMQ channel")
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		log.WithFields(logrus.Fields{
			"event":  "queue_declare",
			"status": "failed",
			"queue":  queueName,
			"error":  err.Error(),
		}).Fatal("Failed to declare queue")
	}

	msgs, err := ch.Consume(queueName, "", true, false, false, false, nil)
	if err != nil {
		log.WithFields(logrus.Fields{
			"event":  "queue_subscribe",
			"status": "failed",
			"queue":  queueName,
			"error":  err.Error(),
		}).Fatal("Failed to subscribe to queue")
	}

	go startWebSocketServer()

	for msg := range msgs {
		log.WithFields(logrus.Fields{
			"event":   "message_received",
			"status":  "success",
			"queue":   queueName,
			"message": string(msg.Body),
		}).Info("Received message from RabbitMQ")
		broadcastMessage(msg.Body)
	}
}

func startWebSocketServer() {
	port := viper.GetString("server.port")
	http.HandleFunc("/ws", handleWebSocket)
	log.WithFields(logrus.Fields{
		"event":  "websocket_server",
		"status": "started",
		"port":   port,
	}).Info("WebSocket server started")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithFields(logrus.Fields{
			"event":  "websocket_upgrade",
			"status": "failed",
			"error":  err.Error(),
		}).Error("Failed to upgrade connection")
		return
	}
	defer conn.Close()

	clientsMu.Lock()
	clients[conn] = true
	clientsMu.Unlock()

	log.WithFields(logrus.Fields{
		"event":  "websocket_connection",
		"status": "connected",
		"client": r.RemoteAddr,
	}).Info("New WebSocket client connected")

	for {
		if _, _, err := conn.NextReader(); err != nil {
			break
		}
	}

	clientsMu.Lock()
	delete(clients, conn)
	clientsMu.Unlock()

	log.WithFields(logrus.Fields{
		"event":  "websocket_disconnection",
		"status": "disconnected",
		"client": r.RemoteAddr,
	}).Info("WebSocket client disconnected")
}

func broadcastMessage(message []byte) {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	for client := range clients {
		err := client.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			log.WithFields(logrus.Fields{
				"event":  "message_broadcast",
				"status": "failed",
				"client": client.RemoteAddr().String(),
				"error":  err.Error(),
			}).Error("Failed to send message to client")
			client.Close()
			delete(clients, client)
		} else {
			log.WithFields(logrus.Fields{
				"event":   "message_broadcast",
				"status":  "success",
				"client":  client.RemoteAddr().String(),
				"message": string(message),
			}).Info("Message sent to WebSocket client")
		}
	}
}
