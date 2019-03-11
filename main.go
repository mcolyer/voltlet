package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type outlet struct {
	id       string
	commands chan string
}

func (o outlet) CommandTopic() string {
	return "/voltson/" + o.id
}

func (o outlet) AvailableTopic() string {
	return o.CommandTopic() + "/available"
}

func (o outlet) StateTopic() string {
	return o.CommandTopic() + "/state"
}

var subscribes = make(chan outlet)
var unsubscribes = make(chan outlet)

type message struct {
	topic    string
	contents string
}

type RelayMessage struct {
	Uri    string `json:"uri"`
	Action string `json:"action"`
}

type LoginReplyMessage struct {
	Uri   string `json:"uri"`
	Error int    `json:"error"`
	Wd    int    `json:"wd"`
	Year  int    `json:"year"`
	Month int    `json:"month"`
	Day   int    `json:"day"`
	Ms    int    `json:"ms"`
	Hh    int    `json:"hh"`
	Hl    int    `json:"hl"`
	Lh    int    `json:"lh"`
	Ll    int    `json:"ll"`
}

var messages = make(chan message)

type logWriter struct {
}

func (writer logWriter) Write(bytes []byte) (int, error) {
	return fmt.Print(time.Now().UTC().Format("15:04:05.999Z") + " " + string(bytes))
}

func websocketRequest(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()

	log.Println("Client connected")
	mqtt := make(chan string)
	offline := make(chan bool)
	device := make(chan map[string]interface{})
	pendingCommand := false

	go func() {
		for {
			var m map[string]interface{}
			c.SetReadDeadline(time.Now().Add(20 * time.Second))
			err := c.ReadJSON(&m)
			if err != nil {
				log.Println("read error:", err)
				offline <- true
				break
			}
			pendingCommand = false
			device <- m
		}
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	o := outlet{"", mqtt}

outer:
	for {
		select {
		case <-offline:
			log.Printf("[%s] offline", o.id)
			messages <- message{o.AvailableTopic(), "offline"}
			unsubscribes <- o
			break outer
		case <-ticker.C:
			log.Printf("[%s] ping", o.id)
			if !pendingCommand {
				err := c.WriteMessage(websocket.TextMessage, []byte("{\"uri\":\"/ka\"}"))
				if err != nil {
					log.Println("ping err:", err)
				}
			}
		case m := <-device:
			log.Printf("[%s] recv: %s", o.id, m)
			if m["id"] != nil {
				o.id = string(m["id"].(string))
				subscribes <- o
				messages <- message{o.AvailableTopic(), "online"}
				now := time.Now()
				msg, _ := json.Marshal(LoginReplyMessage{
					Uri:   "/loginReply",
					Error: 0,
					Wd:    3, // No idea what this is.
					Year:  now.Year(),
					Month: int(now.Month()),
					Day:   now.Day(),
					Ms:    (now.Nanosecond() / 1000000),
					Hh:    0, // No idea what these mean either
					Hl:    0,
					Lh:    0,
					Ll:    0,
				})
				log.Printf("[%s] send: %s", o.id, msg)
				err = c.WriteMessage(websocket.TextMessage, msg)

				if m["relay"] == "open" {
					messages <- message{o.StateTopic(), "true"}
				} else {
					messages <- message{o.StateTopic(), "false"}
				}
			}
			if m["uri"] == "/ka" && m["rssi"] != nil {
				now := time.Now()
				msg, _ := json.Marshal(LoginReplyMessage{
					Uri:   "/kr",
					Error: 0,
					Wd:    3, // No idea what this is.
					Year:  now.Year(),
					Month: int(now.Month()),
					Day:   now.Day(),
					Ms:    (now.Nanosecond() / 1000000),
				})
				log.Printf("[%s] send: %s", o.id, msg)
				err = c.WriteMessage(websocket.TextMessage, msg)
			}

			if m["uri"] == "/runtimeInfo" {
				if m["relay"] == "open" {
					messages <- message{o.StateTopic(), "true"}
				} else {
					messages <- message{o.StateTopic(), "false"}
				}
			}
			if m["uri"] == "/state" {
				if m["relay"] == "open" {
					messages <- message{o.StateTopic(), "true"}
				} else {
					messages <- message{o.StateTopic(), "false"}
				}
			}
		case command := <-mqtt:
			log.Printf("[%s] command: %s", o.id, command)
			var err error
			var msg []byte
			if command == "true" {
				msg, _ = json.Marshal(RelayMessage{Uri: "/relay", Action: "open"})
			} else {
				msg, _ = json.Marshal(RelayMessage{Uri: "/relay", Action: "break"})
			}
			pendingCommand = true
			log.Printf("[%s] send: %s", o.id, msg)
			err = c.WriteMessage(websocket.TextMessage, msg)

			if err != nil {
				log.Println("write error:", err)
				break outer
			}
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func connectMqtt(mqttPtr *string, mqttUserPtr *string, mqttPasswordPtr *string, unsubscribes chan outlet, subscribes chan outlet, messages chan message) {
	opts := MQTT.NewClientOptions()
	opts.SetClientID("voltlet")
	opts.AddBroker("tcp://" + *mqttPtr)
	opts.SetUsername(*mqttUserPtr)
	opts.SetPassword(*mqttPasswordPtr)
	opts.SetKeepAlive(2 * time.Second)
	opts.SetPingTimeout(1 * time.Second)
	opts.SetCleanSession(true)
	opts.SetOnConnectHandler(func(client MQTT.Client) {
		log.Print("Connected to mqtt broker")
	})
	opts.SetConnectionLostHandler(func(client MQTT.Client, reason error) {
		log.Printf("Connection lost to mqtt broker: %s", reason)
	})

	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	for {
		select {
		case o := <-subscribes:
			log.Printf("Subscribing to mqtt topic: %s", o.CommandTopic())
			if token := c.Subscribe(o.CommandTopic(), 0, func(client MQTT.Client, msg MQTT.Message) {
				o.commands <- string(msg.Payload())
			}); token.Wait() && token.Error() != nil {
				fmt.Println(token.Error())
			}
			log.Printf("Subscribed to mqtt topic: %s", o.CommandTopic())
		case o := <-unsubscribes:
			log.Printf("Unsubscribing to mqtt topic: %s", o.CommandTopic())
			if token := c.Unsubscribe(o.CommandTopic()); token.Wait() && token.Error() != nil {
				fmt.Println(token.Error())
			}
			log.Printf("Unsubscribed to mqtt topic: %s", o.CommandTopic())
		case m := <-messages:
			token := c.Publish(m.topic, 0, true, m.contents)
			if token.Wait() && token.Error() != nil {
				log.Printf("Error publishing %s on %s", m.contents, m.topic)
			}
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func startWebsocket() {
	log.Print("Starting websocket")
	http.HandleFunc("/gnws", websocketRequest)
	log.Fatal(http.ListenAndServe("0.0.0.0:17273", nil))
}

func main() {
	log.SetFlags(0)
	log.SetOutput(new(logWriter))

	mqttPtr := flag.String("mqtt-broker", "localhost:1883", "The host and port of the MQTT broker")
	mqttUserPtr := flag.String("mqtt-user", "", "The MQTT broker user")
	mqttPasswordPtr := flag.String("mqtt-password", "", "The MQTT broker password")
	flag.Parse()

	go connectMqtt(mqttPtr, mqttUserPtr, mqttPasswordPtr, unsubscribes, subscribes, messages)
	startWebsocket()
}
