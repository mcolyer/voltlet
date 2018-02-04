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
	state    chan string
}

var subscribes = make(chan outlet)

func echo(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()

	log.Println("Client connected")
	mqtt := make(chan string)
	state := make(chan string, 1)
	offline := make(chan bool)
	device := make(chan map[string]interface{})

	state <- "On"

	go func() {
		for {
			var message map[string]interface{}
			c.SetReadDeadline(time.Now().Add(10 * time.Second))
			err := c.ReadJSON(&message)
			if err != nil {
				log.Println("read error:", err)
				offline <- true
				break
			}
			device <- message
		}
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

outer:
	for {
		select {
		case <-offline:
			log.Println("offline")
			state <- "Off"
			break outer
		case <-ticker.C:
			log.Println("ping")
			err := c.WriteMessage(websocket.TextMessage, []byte("{\"uri\":\"/ka\"}"))
			if err != nil {
				log.Println("ping err:", err)
			}
		case message := <-device:
			log.Printf("recv: %s", message)
			if message["id"] != nil {
				subscribes <- outlet{"/voltson/" + string(message["id"].(string)), mqtt, state}
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

func main() {
	mqttPtr := flag.String("mqtt-broker", "localhost:1883", "The host and port of the MQTT broker")
	mqttUserPtr := flag.String("mqtt-user", "", "The MQTT broker user")
	mqttPasswordPtr := flag.String("mqtt-password", "", "The MQTT broker password")
	flag.Parse()

	log.Print("Connecting to mqtt broker")
	opts := MQTT.NewClientOptions()
	opts.SetClientID("esp8266-outlet")
	opts.AddBroker("tcp://" + *mqttPtr)
	opts.SetUsername(*mqttUserPtr)
	opts.SetPassword(*mqttPasswordPtr)
	opts.SetKeepAlive(2 * time.Second)
	opts.SetPingTimeout(1 * time.Second)

	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	go func() {
		for {
			outlet := <-subscribes
			log.Printf("Connecting to mqtt topic: %s", outlet.id)
			if token := c.Subscribe(outlet.id, 0, func(client MQTT.Client, msg MQTT.Message) {
				log.Printf("Received mqtt msg: %s", msg.Payload())
				outlet.commands <- string(msg.Payload())
			}); token.Wait() && token.Error() != nil {
				fmt.Println(token.Error())
			}

			go func() {
				for {
					state := <-outlet.state
					stateToken := c.Publish(outlet.id+"/state", 0, true, state)
					if stateToken.Wait() && stateToken.Error() != nil {
						log.Printf("Error publishing state for %s", outlet.id)
					}
				}
			}()

			log.Printf("Connected to mqtt topic: %s", outlet.id)
		}
	}()

	log.Print("Starting websocket")
	http.HandleFunc("/gnws", echo)
	log.Fatal(http.ListenAndServe("0.0.0.0:17273", nil))
}
