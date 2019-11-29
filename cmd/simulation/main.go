package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/decentraland/webrtc-broker/pkg/authentication"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/decentraland/webrtc-broker/pkg/simulation"
	"github.com/golang/protobuf/proto"
	pion "github.com/pion/webrtc/v2"
)

func main() {
	addr := flag.String("coordinatorURL", "ws://localhost:9090", "Coordinator URL")
	nBots := flag.Int("n", 1, "number of bots")
	trackStats := flag.Bool("trackStats", false, "")
	flag.Parse()

	log.Println("running random simulation")

	auth := authentication.NoopAuthenticator{}

	for i := 0; i < *nBots; i++ {
		go func() {
			config := simulation.Config{
				Auth:           &auth,
				CoordinatorURL: *addr,
				ICEServers: []pion.ICEServer{
					{
						URLs: []string{"stun:stun.l.google.com:19302"},
					},
				},
			}

			if *trackStats {
				trackCh := make(chan []byte, 256)
				config.OnMessageReceived = func(reliable bool, msgType protocol.MessageType, raw []byte) {
					if !reliable && msgType == protocol.MessageType_TOPIC_FW {
						trackCh <- raw
					}
				}

				go func() {
					peers := make(map[uint64]*simulation.Stats)
					topicFWMessage := protocol.TopicFWMessage{}

					onMessage := func(rawMsg []byte) {
						if err := proto.Unmarshal(rawMsg, &topicFWMessage); err != nil {
							log.Println("error unmarshalling data message")
							return
						}

						alias := topicFWMessage.FromAlias
						stats := peers[alias]

						if stats == nil {
							stats = &simulation.Stats{}
							peers[alias] = stats
						}

						stats.Seen(time.Now())
					}

					reportTicker := time.NewTicker(30 * time.Second)
					defer reportTicker.Stop()

					for {
						select {
						case rawMsg := <-trackCh:
							onMessage(rawMsg)

							n := len(trackCh)
							for i := 0; i < n; i++ {
								rawMsg = <-trackCh
								onMessage(rawMsg)
							}
						case <-reportTicker.C:
							log.Println("Avg duration between position messages")

							for alias, stats := range peers {
								fmt.Printf("%d: %f ms\n", alias, stats.Avg())

								if time.Since(stats.LastSeen).Seconds() > 1 {
									delete(peers, alias)
								}
							}
						}
					}
				}()
			}

			client := simulation.Start(&config)

			msg := protocol.TopicMessage{
				Type:  protocol.MessageType_TOPIC,
				Topic: "test",
				Body:  make([]byte, 32),
			}

			bytes, err := proto.Marshal(&msg)
			if err != nil {
				log.Fatal("encode failed", err)
			}

			err = client.SendTopicSubscriptionMessage(map[string]bool{"test": true})
			if err != nil {
				log.Fatal("send topic subscription message", err)
			}

			writeTicker := time.NewTicker(100 * time.Millisecond)
			defer writeTicker.Stop()

			for range writeTicker.C {
				client.SendUnreliable <- bytes
			}
		}()
	}

	select {}
}
