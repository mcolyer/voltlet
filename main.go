package main

import (
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

type message struct {
	topic    string
	contents string
}

var messages = make(chan message)

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

	go func() {
		for {
			var m map[string]interface{}
			c.SetReadDeadline(time.Now().Add(10 * time.Second))
			err := c.ReadJSON(&m)
			if err != nil {
				log.Println("read error:", err)
				offline <- true
				break
			}
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
			log.Println("offline")
			messages <- message{o.AvailableTopic(), "offline"}
			break outer
		case <-ticker.C:
			log.Println("ping")
			err := c.WriteMessage(websocket.TextMessage, []byte("{\"uri\":\"/ka\"}"))
			if err != nil {
				log.Println("ping err:", err)
			}
		case m := <-device:
			log.Printf("recv: %s", m)
			if m["id"] != nil {
				o.id = string(m["id"].(string))
				subscribes <- o
				messages <- message{o.AvailableTopic(), "online"}
			}
		case command := <-mqtt:
			log.Printf("command: %s", command)
			err = c.WriteMessage(websocket.TextMessage, []byte(command))
			if err != nil {
				log.Println("write error:", err)
				break outer
			}
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func connectMqtt(mqttPtr *string, mqttUserPtr *string, mqttPasswordPtr *string, subscribes chan outlet, messages chan message) {
	log.Print("Connecting to mqtt broker")

	opts := MQTT.NewClientOptions()
	opts.SetClientID("voltlet")
	opts.AddBroker("tcp://" + *mqttPtr)
	opts.SetUsername(*mqttUserPtr)
	opts.SetPassword(*mqttPasswordPtr)
	opts.SetKeepAlive(2 * time.Second)
	opts.SetPingTimeout(1 * time.Second)

	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	for {
		select {
		case o := <-subscribes:
			log.Printf("Subscribing to mqtt topic: %s", o.CommandTopic())
			if token := c.Subscribe(o.CommandTopic(), 0, func(client MQTT.Client, msg MQTT.Message) {
				log.Printf("Received mqtt msg: %s", msg.Payload())
				o.commands <- string(msg.Payload())
			}); token.Wait() && token.Error() != nil {
				fmt.Println(token.Error())
			}
			log.Printf("Subscribed to mqtt topic: %s", o.CommandTopic())
		case m := <-messages:
			log.Print(m.contents)
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
	mqttPtr := flag.String("mqtt-broker", "localhost:1883", "The host and port of the MQTT broker")
	mqttUserPtr := flag.String("mqtt-user", "", "The MQTT broker user")
	mqttPasswordPtr := flag.String("mqtt-password", "", "The MQTT broker password")
	flag.Parse()

	go connectMqtt(mqttPtr, mqttUserPtr, mqttPasswordPtr, subscribes, messages)
	startWebsocket()
}
